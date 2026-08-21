// YachtSense Link Emulator reproduces the local-network behavior used by
// Raymarine Axiom/LightHouse and the Raymarine mobile app to discover a
// YachtSense Link and MFD remote-view services.
//
// The binary is intentionally dependency-free so it can be cross-compiled as a
// small static ARMv7 executable for Teltonika RUTX routers running RutOS 7.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// Standard IPv4 mDNS multicast endpoint defined by RFC 6762.
	mdnsAddress = "224.0.0.251"
	mdnsPort    = 5353

	// YachtSense Link is advertised as an HTTP DNS-SD service.
	yachtSenseServiceType = "_http._tcp.local"
	browseType            = "_services._dns-sd._udp.local"
	productID             = "E70640"
	modelName             = "Raymarine YachtSense Link"

	// Raymarine Android 2.3.16 browses these service types while looking for
	// MFDs and onboard devices. The browseType is also included below.
	rtspServiceType  = "_rtsp._tcp.local"
	rymServiceType   = "_rym_rrc._tcp.local"
	rayDBServiceType = "_raydb._tcp.local"

	// A short TTL keeps stale advertisements from lingering after shutdown.
	defaultTTL = 120

	// Runtime state is text so the RutOS Lua API can read it without an extra
	// JSON dependency on the router.
	defaultRuntimeStateFile = "/var/run/yachtsense-link-emulator.runtime"
)

// packageVersion is replaced by the build script through a Go linker flag.
var packageVersion = "development"

// Debug logging is selected from UCI and copied into this process-global flag
// after configuration has been parsed.
var debugLogging bool

// raymarineServiceTypes is the allow-list used by the built-in reflector. The
// reflector never forwards arbitrary mDNS traffic between configured networks.
var raymarineServiceTypes = map[string]struct{}{
	yachtSenseServiceType: {},
	rtspServiceType:       {},
	rymServiceType:        {},
	rayDBServiceType:      {},
	browseType:            {},
}

// mfdServiceTypes are strong indicators of an Axiom/MFD rather than a generic
// HTTP/onboard device. They are used for friendly status and log messages.
var mfdServiceTypes = map[string]struct{}{
	rtspServiceType:  {},
	rymServiceType:   {},
	rayDBServiceType: {},
}

// configuration contains settings exported by the RutOS procd init script.
type configuration struct {
	AxiomInterface    string
	AxiomIP           net.IP
	RemoteInterfaces  []string
	Serial            string
	Version           string
	HostLabel         string
	Instance          string
	HealthPort        int
	MDNSEnabled       bool
	WebEnabled        bool
	TTL               uint32
	RelayMode         string
	ExistingReflector bool
	ReflectorReason   string
	LogLevel          string
	StateFile         string
}

// dnsQuestion and dnsRecord contain only the DNS fields the reflector needs.
// Keeping a small parser here avoids pulling a general DNS library into RutOS.
type dnsQuestion struct {
	Name  string
	Type  uint16
	Class uint16
}

type dnsRecord struct {
	Name    string
	Type    uint16
	Class   uint16
	TTL     uint32
	Target  string
	Port    uint16
	Address string
}

type dnsMessage struct {
	Response  bool
	Questions []dnsQuestion
	Records   []dnsRecord
}

// runtimeState writes concise last-seen information for the VuCI status page.
type runtimeState struct {
	mu     sync.Mutex
	path   string
	values map[string]string
}

// nameCache remembers DNS-SD instance and host names learned from Raymarine
// responses. Android NSD may query those names after the initial service browse.
type nameCache struct {
	mu      sync.Mutex
	expires map[string]time.Time
}

// serviceObservation is assembled from PTR/SRV/A records across packets.
type serviceObservation struct {
	Service  string
	Instance string
	Host     string
	IP       string
	Port     uint16
}

// discoveryTracker maintains enough cross-packet state to produce useful logs
// such as "Axiom/MFD detected" without logging every multicast packet.
type discoveryTracker struct {
	mu             sync.Mutex
	observations   map[string]*serviceObservation
	hostAddresses  map[string]string
	loggedDevices  map[string]string
	loggedServices map[string]string
	state          *runtimeState
}

// mdnsEndpoint owns one socket bound to one Linux interface. Using one socket
// per interface lets the reflector know where each packet entered without
// requiring ancillary IP_PKTINFO parsing.
type mdnsEndpoint struct {
	name         string
	iface        *net.Interface
	ip           net.IP
	conn         *net.UDPConn
	group        *net.UDPAddr
	announcement []byte
	goodbye      []byte
	publish      bool
}

// mdnsEngine coordinates direct YachtSense advertisements, selective
// reflection and discovery logging across the configured interfaces.
type mdnsEngine struct {
	cfg          configuration
	state        *runtimeState
	cache        *nameCache
	tracker      *discoveryTracker
	axiom        *mdnsEndpoint
	remotes      []*mdnsEndpoint
	endpoints    map[string]*mdnsEndpoint
	relayActive  bool
	relayReason  string
	ownNames     map[string]struct{}
	queryLogMu   sync.Mutex
	lastQueryLog map[string]time.Time
	wg           sync.WaitGroup
	closeOnce    sync.Once
}

// infof and debugf keep normal logs readable while retaining packet-level
// diagnostics when the user switches the WebUI log level to Debug.
func infof(format string, args ...interface{}) {
	log.Printf("INFO "+format, args...)
}

func debugf(format string, args ...interface{}) {
	if debugLogging {
		log.Printf("DEBUG "+format, args...)
	}
}

// environmentBoolean accepts the common boolean values used by UCI/procd.
func environmentBoolean(name string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return fallback
	}

	switch raw {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return fallback
	}
}

// environmentList parses a comma-separated interface list while preserving
// order and removing empty or duplicate entries.
func environmentList(name string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, 4)
	for _, item := range strings.Split(os.Getenv(name), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

// cleanDNSLabel removes control characters and dots from a single DNS label.
func cleanDNSLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}

	// Dots would split the value into multiple labels. Spaces are valid in a
	// DNS-SD service instance and are intentionally retained.
	value = strings.ReplaceAll(value, ".", "-")
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(value) > 63 {
		value = value[:63]
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// cleanSerial keeps the identifier compatible with the validation in VuCI.
func cleanSerial(value string) string {
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return -1
		}
	}, strings.TrimSpace(value))
	if value == "" {
		value = "AF002A4"
	}
	if len(value) > 32 {
		value = value[:32]
	}
	return value
}

// loadConfiguration validates the environment exported by the init script.
func loadConfiguration() (configuration, error) {
	cfg := configuration{
		AxiomInterface:    strings.TrimSpace(os.Getenv("YSL_AXIOM_IFACE")),
		AxiomIP:           net.ParseIP(strings.TrimSpace(os.Getenv("YSL_IP"))).To4(),
		RemoteInterfaces:  environmentList("YSL_REMOTE_IFACES"),
		Serial:            cleanSerial(os.Getenv("YSL_SERIAL")),
		Version:           strings.TrimSpace(os.Getenv("YSL_VERSION")),
		HostLabel:         cleanDNSLabel(os.Getenv("YSL_HOSTNAME"), "yachtsense-main"),
		Instance:          cleanDNSLabel(os.Getenv("YSL_INSTANCE"), "yachtsense-main Settings"),
		MDNSEnabled:       environmentBoolean("YSL_MDNS_ENABLED", true),
		WebEnabled:        environmentBoolean("YSL_WEB_ENABLED", true),
		TTL:               defaultTTL,
		RelayMode:         strings.ToLower(strings.TrimSpace(os.Getenv("YSL_RELAY_MODE"))),
		ExistingReflector: environmentBoolean("YSL_EXISTING_REFLECTOR", false),
		ReflectorReason:   strings.TrimSpace(os.Getenv("YSL_REFLECTOR_REASON")),
		LogLevel:          strings.ToLower(strings.TrimSpace(os.Getenv("YSL_LOG_LEVEL"))),
		StateFile:         strings.TrimSpace(os.Getenv("YSL_STATE_FILE")),
	}

	if cfg.AxiomInterface == "" {
		cfg.AxiomInterface = "br-lan"
	}
	if cfg.AxiomIP == nil {
		return configuration{}, errors.New("YSL_IP is not a valid IPv4 address")
	}
	if len(cfg.RemoteInterfaces) == 0 {
		cfg.RemoteInterfaces = []string{"br-lan"}
	}
	if cfg.Version == "" {
		cfg.Version = "V142.242.530"
	}
	if cfg.RelayMode == "" {
		cfg.RelayMode = "auto"
	}
	if cfg.RelayMode != "auto" && cfg.RelayMode != "force" && cfg.RelayMode != "disabled" {
		return configuration{}, fmt.Errorf("invalid YSL_RELAY_MODE %q", cfg.RelayMode)
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.LogLevel != "info" && cfg.LogLevel != "debug" {
		return configuration{}, fmt.Errorf("invalid YSL_LOG_LEVEL %q", cfg.LogLevel)
	}
	if cfg.StateFile == "" {
		cfg.StateFile = defaultRuntimeStateFile
	}

	// Parse the optional HTTP health port.
	if raw := strings.TrimSpace(os.Getenv("YSL_HEALTH_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 1 || port > 65535 {
			return configuration{}, fmt.Errorf("invalid YSL_HEALTH_PORT %q", raw)
		}
		cfg.HealthPort = port
	} else {
		cfg.HealthPort = 7777
	}

	// Parse an optional mDNS TTL used mainly for protocol testing.
	if raw := strings.TrimSpace(os.Getenv("YSL_TTL")); raw != "" {
		ttl, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || ttl == 0 {
			return configuration{}, fmt.Errorf("invalid YSL_TTL %q", raw)
		}
		cfg.TTL = uint32(ttl)
	}

	return cfg, nil
}

// newRuntimeState creates a key/value state writer and initializes stable keys.
func newRuntimeState(path string) *runtimeState {
	state := &runtimeState{path: path, values: make(map[string]string)}
	state.update(map[string]string{
		"relay_active":        "0",
		"relay_reason":        "not started",
		"last_axiom_name":     "",
		"last_axiom_ip":       "",
		"last_axiom_service":  "",
		"last_axiom_port":     "",
		"last_remote_ip":      "",
		"last_remote_iface":   "",
		"last_remote_service": "",
		"last_activity":       "",
	})
	return state
}

// sanitizeStateValue guarantees one state entry stays on one text line.
func sanitizeStateValue(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}

// update stores several fields and atomically replaces the runtime state file.
func (state *runtimeState) update(values map[string]string) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for key, value := range values {
		state.values[key] = sanitizeStateValue(value)
	}
	state.writeLocked()
}

func (state *runtimeState) writeLocked() {
	keys := make([]string, 0, len(state.values))
	for key := range state.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&builder, "%s=%s\n", key, state.values[key])
	}

	temporary := state.path + ".tmp"
	if err := os.WriteFile(temporary, []byte(builder.String()), 0644); err != nil {
		debugf("runtime state write failed: %v", err)
		return
	}
	if err := os.Rename(temporary, state.path); err != nil {
		_ = os.Remove(temporary)
		debugf("runtime state rename failed: %v", err)
	}
}

// newNameCache creates a cache for instance/host names learned from responses.
func newNameCache() *nameCache {
	return &nameCache{expires: make(map[string]time.Time)}
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

func (cache *nameCache) add(name string) {
	name = normalizeDNSName(name)
	if name == "" {
		return
	}
	cache.mu.Lock()
	cache.expires[name] = time.Now().Add(15 * time.Minute)
	cache.mu.Unlock()
}

func (cache *nameCache) has(name string) bool {
	name = normalizeDNSName(name)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	expires, found := cache.expires[name]
	if !found {
		return false
	}
	if time.Now().After(expires) {
		delete(cache.expires, name)
		return false
	}
	return true
}

// encodeDNSName converts a dotted DNS name to the DNS wire representation.
func encodeDNSName(name string) ([]byte, error) {
	labels := strings.Split(strings.TrimSuffix(name, "."), ".")
	encoded := make([]byte, 0, len(name)+2)
	for _, label := range labels {
		if label == "" || len(label) > 63 {
			return nil, fmt.Errorf("invalid DNS label %q", label)
		}
		encoded = append(encoded, byte(len(label)))
		encoded = append(encoded, []byte(label)...)
	}
	return append(encoded, 0), nil
}

func appendUint16(destination []byte, value uint16) []byte {
	var buffer [2]byte
	binary.BigEndian.PutUint16(buffer[:], value)
	return append(destination, buffer[:]...)
}

func appendUint32(destination []byte, value uint32) []byte {
	var buffer [4]byte
	binary.BigEndian.PutUint32(buffer[:], value)
	return append(destination, buffer[:]...)
}

// appendResourceRecord adds one DNS resource record to an existing packet.
func appendResourceRecord(destination []byte, name string, recordType, class uint16, ttl uint32, data []byte) ([]byte, error) {
	encodedName, err := encodeDNSName(name)
	if err != nil {
		return nil, err
	}
	destination = append(destination, encodedName...)
	destination = appendUint16(destination, recordType)
	destination = appendUint16(destination, class)
	destination = appendUint32(destination, ttl)
	destination = appendUint16(destination, uint16(len(data)))
	destination = append(destination, data...)
	return destination, nil
}

// buildTXTData serializes DNS-SD TXT values as length-prefixed strings.
func buildTXTData(values []string) ([]byte, error) {
	data := make([]byte, 0, 128)
	for _, value := range values {
		if len(value) > 255 {
			return nil, errors.New("TXT record exceeds 255 bytes")
		}
		data = append(data, byte(len(value)))
		data = append(data, []byte(value)...)
	}
	return data, nil
}

// buildAnnouncement creates the YachtSense DNS-SD response for one interface.
func buildAnnouncement(cfg configuration, iface *net.Interface, advertisedIP net.IP, ttl uint32) ([]byte, error) {
	instanceName := cfg.Instance + "." + yachtSenseServiceType
	hostName := cfg.HostLabel + ".local"

	encodedInstance, err := encodeDNSName(instanceName)
	if err != nil {
		return nil, err
	}
	encodedHost, err := encodeDNSName(hostName)
	if err != nil {
		return nil, err
	}
	encodedService, err := encodeDNSName(yachtSenseServiceType)
	if err != nil {
		return nil, err
	}
	if advertisedIP == nil || advertisedIP.To4() == nil {
		return nil, errors.New("announcement requires an IPv4 address")
	}

	// Use the selected interface MAC in the identity advertised to Raymarine.
	macAddress := "02:00:00:00:00:01"
	if iface != nil && len(iface.HardwareAddr) > 0 {
		macAddress = strings.ToLower(iface.HardwareAddr.String())
	}

	txtData, err := buildTXTData([]string{
		"id=" + productID + " " + cfg.Serial,
		"model=" + modelName,
		"version=" + cfg.Version,
		"mac=" + macAddress,
	})
	if err != nil {
		return nil, err
	}

	// The real Link advertises HTTP port 80 in SRV metadata. The Axiom's
	// separate internet-source liveness probe is answered on port 7777.
	srvData := make([]byte, 0, len(encodedHost)+6)
	srvData = appendUint16(srvData, 0)
	srvData = appendUint16(srvData, 0)
	srvData = appendUint16(srvData, 80)
	srvData = append(srvData, encodedHost...)

	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[2:4], 0x8400)
	binary.BigEndian.PutUint16(packet[6:8], 5)
	packet, err = appendResourceRecord(packet, yachtSenseServiceType, 12, 0x0001, ttl, encodedInstance)
	if err != nil {
		return nil, err
	}
	packet, err = appendResourceRecord(packet, instanceName, 33, 0x8001, ttl, srvData)
	if err != nil {
		return nil, err
	}
	packet, err = appendResourceRecord(packet, instanceName, 16, 0x8001, ttl, txtData)
	if err != nil {
		return nil, err
	}
	packet, err = appendResourceRecord(packet, hostName, 1, 0x8001, ttl, []byte(advertisedIP.To4()))
	if err != nil {
		return nil, err
	}
	packet, err = appendResourceRecord(packet, browseType, 12, 0x0001, ttl, encodedService)
	if err != nil {
		return nil, err
	}
	return packet, nil
}

// decodeDNSName reads a possibly compressed DNS name from a received packet.
func decodeDNSName(packet []byte, start, depth int) (string, int, error) {
	if depth > 12 {
		return "", start, errors.New("DNS compression loop")
	}
	labels := make([]string, 0, 4)
	offset := start
	next := start
	jumped := false

	for offset < len(packet) {
		length := int(packet[offset])
		if length == 0 {
			if !jumped {
				next = offset + 1
			}
			return normalizeDNSName(strings.Join(labels, ".")), next, nil
		}
		if length&0xc0 == 0xc0 {
			if offset+1 >= len(packet) {
				return "", next, errors.New("truncated DNS pointer")
			}
			pointer := ((length & 0x3f) << 8) | int(packet[offset+1])
			if !jumped {
				next = offset + 2
			}
			nested, _, err := decodeDNSName(packet, pointer, depth+1)
			if err != nil {
				return "", next, err
			}
			if nested != "" {
				labels = append(labels, nested)
			}
			return normalizeDNSName(strings.Join(labels, ".")), next, nil
		}
		if length > 63 || offset+1+length > len(packet) {
			return "", next, errors.New("invalid DNS label")
		}
		labels = append(labels, string(packet[offset+1:offset+1+length]))
		offset += 1 + length
		if !jumped {
			next = offset
		}
	}
	return "", next, errors.New("unterminated DNS name")
}

// parseDNSMessage decodes questions and the record fields required by DNS-SD.
func parseDNSMessage(packet []byte) (*dnsMessage, error) {
	if len(packet) < 12 {
		return nil, errors.New("DNS packet is shorter than header")
	}
	message := &dnsMessage{Response: binary.BigEndian.Uint16(packet[2:4])&0x8000 != 0}
	offset := 12
	questionCount := int(binary.BigEndian.Uint16(packet[4:6]))
	for i := 0; i < questionCount; i++ {
		name, next, err := decodeDNSName(packet, offset, 0)
		if err != nil {
			return nil, err
		}
		offset = next
		if offset+4 > len(packet) {
			return nil, errors.New("truncated DNS question")
		}
		message.Questions = append(message.Questions, dnsQuestion{
			Name:  name,
			Type:  binary.BigEndian.Uint16(packet[offset : offset+2]),
			Class: binary.BigEndian.Uint16(packet[offset+2 : offset+4]),
		})
		offset += 4
	}

	recordCount := int(binary.BigEndian.Uint16(packet[6:8])) +
		int(binary.BigEndian.Uint16(packet[8:10])) +
		int(binary.BigEndian.Uint16(packet[10:12]))
	for i := 0; i < recordCount; i++ {
		name, next, err := decodeDNSName(packet, offset, 0)
		if err != nil {
			return nil, err
		}
		offset = next
		if offset+10 > len(packet) {
			return nil, errors.New("truncated DNS resource record")
		}
		recordType := binary.BigEndian.Uint16(packet[offset : offset+2])
		class := binary.BigEndian.Uint16(packet[offset+2 : offset+4])
		ttl := binary.BigEndian.Uint32(packet[offset+4 : offset+8])
		length := int(binary.BigEndian.Uint16(packet[offset+8 : offset+10]))
		offset += 10
		if offset+length > len(packet) {
			return nil, errors.New("truncated DNS RDATA")
		}
		rdataOffset := offset
		record := dnsRecord{Name: name, Type: recordType, Class: class, TTL: ttl}
		switch recordType {
		case 12: // PTR target.
			target, _, decodeErr := decodeDNSName(packet, rdataOffset, 0)
			if decodeErr == nil {
				record.Target = target
			}
		case 33: // SRV priority, weight, port and target.
			if length >= 6 {
				record.Port = binary.BigEndian.Uint16(packet[rdataOffset+4 : rdataOffset+6])
				target, _, decodeErr := decodeDNSName(packet, rdataOffset+6, 0)
				if decodeErr == nil {
					record.Target = target
				}
			}
		case 1: // IPv4 A record.
			if length == net.IPv4len {
				record.Address = net.IP(packet[rdataOffset : rdataOffset+length]).String()
			}
		case 28: // IPv6 AAAA record; retained for completeness in relayed replies.
			if length == net.IPv6len {
				record.Address = net.IP(packet[rdataOffset : rdataOffset+length]).String()
			}
		}
		message.Records = append(message.Records, record)
		offset += length
	}
	return message, nil
}

// serviceForName returns a Raymarine service type for either the service browse
// name itself or a DNS-SD instance below that service type.
func serviceForName(name string) string {
	name = normalizeDNSName(name)
	if _, found := raymarineServiceTypes[name]; found {
		return name
	}
	for service := range raymarineServiceTypes {
		if service == browseType {
			continue
		}
		if strings.HasSuffix(name, "."+service) {
			return service
		}
	}
	return ""
}

// relevantQueryNames returns service/learned names that are safe to forward
// from a Raymarine app network toward the Axiom/RayNet interface.
func relevantQueryNames(message *dnsMessage, cache *nameCache) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(message.Questions))
	for _, question := range message.Questions {
		name := normalizeDNSName(question.Name)
		_, exactServiceBrowse := raymarineServiceTypes[name]
		if !exactServiceBrowse && !cache.has(name) {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

// isRelevantHTTPInstance mirrors the service-name filtering used by the mobile
// app for onboard HTTP devices. It prevents a generic _http._tcp browse from
// turning the reflector into a bridge for unrelated web/Bonjour devices.
func isRelevantHTTPInstance(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "yachtsense-main") ||
		strings.Contains(name, "imx8mmevk") ||
		strings.Contains(name, "rds webserver") ||
		(strings.Contains(name, "raymarinerds") && strings.Contains(name, "cloudconnector"))
}

// responseRelevant learns instance/host names and decides whether an Axiom-side
// response belongs to Raymarine discovery and may be reflected to app networks.
func responseRelevant(message *dnsMessage, cache *nameCache) bool {
	relevant := false

	// First learn only safe PTR targets. DNS-SD enumeration packets may contain
	// many unrelated service types, so only our explicit service allow-list is
	// learned from _services._dns-sd._udp. Generic HTTP PTR targets are accepted
	// only when their service instance matches known Raymarine onboard names.
	for _, record := range message.Records {
		if record.Type != 12 || record.Target == "" {
			continue
		}
		owner := normalizeDNSName(record.Name)
		target := normalizeDNSName(record.Target)
		switch owner {
		case browseType:
			if _, allowed := raymarineServiceTypes[target]; allowed && target != browseType {
				relevant = true
				cache.add(target)
			}
		case yachtSenseServiceType:
			if isRelevantHTTPInstance(target) {
				relevant = true
				cache.add(target)
			}
		case rtspServiceType, rymServiceType, rayDBServiceType:
			relevant = true
			cache.add(target)
		}
	}

	// Then learn SRV hostnames and accept records belonging to already learned
	// names. Core MFD service instances may also be recognized directly by their
	// suffix in case an unsolicited announcement omits the preceding PTR.
	for _, record := range message.Records {
		name := normalizeDNSName(record.Name)
		ownerRelevant := cache.has(name)
		service := serviceForName(name)
		if service == rtspServiceType || service == rymServiceType || service == rayDBServiceType {
			ownerRelevant = true
		}
		if service == yachtSenseServiceType && isRelevantHTTPInstance(name) {
			ownerRelevant = true
		}
		if ownerRelevant {
			relevant = true
			cache.add(name)
			if record.Target != "" {
				cache.add(record.Target)
			}
			continue
		}
		if record.Target != "" && cache.has(record.Target) {
			relevant = true
		}
	}
	return relevant
}

// containsOwnQuestion decides whether this interface should answer a query for
// the emulator's YachtSense Link advertisement.
func containsOwnQuestion(message *dnsMessage, ownNames map[string]struct{}) bool {
	if message.Response {
		return false
	}
	for _, question := range message.Questions {
		if _, found := ownNames[normalizeDNSName(question.Name)]; found {
			return true
		}
	}
	return false
}

// firstInterfaceIPv4 chooses the first non-loopback IPv4 address assigned to an
// interface. The Axiom interface uses the explicitly configured RayNet address
// instead, while remote interfaces normally use their regular LAN/VLAN address.
func firstInterfaceIPv4(iface *net.Interface) (net.IP, error) {
	addresses, err := iface.Addrs()
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && ip.To4() != nil && !ip.IsLoopback() {
			return ip.To4(), nil
		}
	}
	return nil, errors.New("no IPv4 address is assigned")
}

func interfaceHasIP(iface *net.Interface, wanted net.IP) bool {
	addresses, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && ip.To4() != nil && ip.Equal(wanted) {
			return true
		}
	}
	return false
}

// openMDNSSocket creates a reusable mDNS socket bound to one Linux interface.
// SO_BINDTODEVICE is important: it prevents the same multicast packet from
// appearing on several per-interface sockets and makes relay direction explicit.
func openMDNSSocket(interfaceName string, sourceIP net.IP) (*net.UDPConn, error) {
	listenConfig := net.ListenConfig{
		Control: func(_ string, _ string, raw syscall.RawConn) error {
			var controlError error
			if err := raw.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					controlError = err
					return
				}
				// SO_REUSEPORT is 15 on Linux and permits coexistence with Avahi/
				// other well-behaved mDNS responders that also use socket reuse.
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 0x0f, 1)
				if err := syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, interfaceName); err != nil {
					controlError = err
				}
			}); err != nil {
				return err
			}
			return controlError
		},
	}

	packetConnection, err := listenConfig.ListenPacket(context.Background(), "udp4", fmt.Sprintf("0.0.0.0:%d", mdnsPort))
	if err != nil {
		return nil, err
	}
	connection, ok := packetConnection.(*net.UDPConn)
	if !ok {
		_ = packetConnection.Close()
		return nil, errors.New("mDNS socket is not UDP")
	}

	rawConnection, err := connection.SyscallConn()
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	var socketError error
	if err := rawConnection.Control(func(fd uintptr) {
		group := net.ParseIP(mdnsAddress).To4()
		ip := sourceIP.To4()
		membership := syscall.IPMreq{
			Multiaddr: [4]byte{group[0], group[1], group[2], group[3]},
			Interface: [4]byte{ip[0], ip[1], ip[2], ip[3]},
		}
		if err := syscall.SetsockoptIPMreq(int(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, &membership); err != nil {
			socketError = err
			return
		}
		if err := syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, [4]byte{ip[0], ip[1], ip[2], ip[3]}); err != nil {
			socketError = err
			return
		}
		if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255); err != nil {
			socketError = err
			return
		}
		// Reflected packets must leave the interface but need not be looped
		// back into this process, which also prevents simple relay loops.
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 0)
	}); err != nil || socketError != nil {
		_ = connection.Close()
		if err != nil {
			return nil, err
		}
		return nil, socketError
	}
	_ = connection.SetReadBuffer(128 * 1024)
	return connection, nil
}

// newEndpoint validates an interface/address pair and opens its mDNS socket.
func newEndpoint(name string, explicitIP net.IP) (*mdnsEndpoint, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", name, err)
	}
	ip := explicitIP
	if ip == nil {
		ip, err = firstInterfaceIPv4(iface)
		if err != nil {
			return nil, fmt.Errorf("interface %s: %w", name, err)
		}
	}
	if !interfaceHasIP(iface, ip) {
		return nil, fmt.Errorf("configured IP %s is not present on %s", ip.String(), name)
	}
	connection, err := openMDNSSocket(name, ip)
	if err != nil {
		return nil, fmt.Errorf("open mDNS socket on %s: %w", name, err)
	}
	return &mdnsEndpoint{
		name:  name,
		iface: iface,
		ip:    ip.To4(),
		conn:  connection,
		group: &net.UDPAddr{IP: net.ParseIP(mdnsAddress), Port: mdnsPort},
	}, nil
}

// determineRelayMode centralizes safety rules around existing reflectors.
func determineRelayMode(cfg configuration, hasDistinctRemote bool) (bool, string) {
	if !hasDistinctRemote {
		return false, "Axiom and app interfaces share the same layer-2 interface"
	}
	switch cfg.RelayMode {
	case "disabled":
		return false, "discovery relay disabled by configuration"
	case "force":
		if cfg.ExistingReflector {
			return true, "forced built-in relay; existing reflector was detected"
		}
		return true, "forced built-in relay"
	default: // auto
		if cfg.ExistingReflector {
			reason := cfg.ReflectorReason
			if reason == "" {
				reason = "existing mDNS reflector detected"
			}
			return false, reason
		}
		return true, "automatic built-in Raymarine discovery relay"
	}
}

// newMDNSEngine opens every unique configured interface and prepares direct
// YachtSense advertisements. When an existing reflector is used in Auto mode,
// the remote advertisement is deliberately omitted to avoid duplicate reflected
// instances; the Axiom-side advertisement will be reflected by the existing one.
func newMDNSEngine(cfg configuration, state *runtimeState) (*mdnsEngine, error) {
	engine := &mdnsEngine{
		cfg:          cfg,
		state:        state,
		cache:        newNameCache(),
		endpoints:    make(map[string]*mdnsEndpoint),
		ownNames:     make(map[string]struct{}),
		lastQueryLog: make(map[string]time.Time),
	}
	engine.tracker = &discoveryTracker{
		observations:   make(map[string]*serviceObservation),
		hostAddresses:  make(map[string]string),
		loggedDevices:  make(map[string]string),
		loggedServices: make(map[string]string),
		state:          state,
	}

	axiom, err := newEndpoint(cfg.AxiomInterface, cfg.AxiomIP)
	if err != nil {
		return nil, err
	}
	engine.axiom = axiom
	engine.endpoints[axiom.name] = axiom

	hasDistinctRemote := false
	for _, name := range cfg.RemoteInterfaces {
		if name == cfg.AxiomInterface {
			engine.remotes = append(engine.remotes, axiom)
			continue
		}
		hasDistinctRemote = true
		endpoint, found := engine.endpoints[name]
		if !found {
			endpoint, err = newEndpoint(name, nil)
			if err != nil {
				engine.close()
				return nil, err
			}
			engine.endpoints[name] = endpoint
		}
		engine.remotes = append(engine.remotes, endpoint)
	}

	engine.relayActive, engine.relayReason = determineRelayMode(cfg, hasDistinctRemote)
	state.update(map[string]string{
		"relay_active": boolString(engine.relayActive),
		"relay_reason": engine.relayReason,
	})

	// All interface-local publishers share the same instance/hostname names.
	engine.ownNames[normalizeDNSName(yachtSenseServiceType)] = struct{}{}
	engine.ownNames[normalizeDNSName(cfg.Instance+"."+yachtSenseServiceType)] = struct{}{}
	engine.ownNames[normalizeDNSName(cfg.HostLabel+".local")] = struct{}{}
	engine.ownNames[normalizeDNSName(browseType)] = struct{}{}

	if cfg.MDNSEnabled {
		publishRemoteDirectly := !(cfg.RelayMode == "auto" && cfg.ExistingReflector)
		for _, endpoint := range engine.endpoints {
			publish := endpoint == engine.axiom || publishRemoteDirectly
			if !publish {
				continue
			}
			announcement, buildErr := buildAnnouncement(cfg, endpoint.iface, endpoint.ip, cfg.TTL)
			if buildErr != nil {
				engine.close()
				return nil, buildErr
			}
			goodbye, buildErr := buildAnnouncement(cfg, endpoint.iface, endpoint.ip, 0)
			if buildErr != nil {
				engine.close()
				return nil, buildErr
			}
			endpoint.announcement = announcement
			endpoint.goodbye = goodbye
			endpoint.publish = true
		}
	}
	return engine, nil
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

// send writes an mDNS packet on one endpoint with source interface selection
// already enforced by SO_BINDTODEVICE and IP_MULTICAST_IF.
func (endpoint *mdnsEndpoint) send(packet []byte) {
	if endpoint == nil || endpoint.conn == nil || len(packet) == 0 {
		return
	}
	if _, err := endpoint.conn.WriteToUDP(packet, endpoint.group); err != nil && !errors.Is(err, net.ErrClosed) {
		infof("mDNS send failed interface=%s error=%v", endpoint.name, err)
	}
}

// periodicallyAdvertise sends initial and refresh announcements on every
// interface that should present the YachtSense identity directly.
func (engine *mdnsEngine) periodicallyAdvertise(ctx context.Context) {
	if !engine.cfg.MDNSEnabled {
		return
	}
	announceAll := func() {
		for _, endpoint := range engine.endpoints {
			if endpoint.publish {
				endpoint.send(endpoint.announcement)
			}
		}
	}
	announceAll()
	initial := time.NewTimer(time.Second)
	periodic := time.NewTicker(time.Minute)
	defer initial.Stop()
	defer periodic.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-initial.C:
			announceAll()
		case <-periodic.C:
			announceAll()
		}
	}
}

// shouldLogQuery rate-limits normal Info logs while Debug mode can still show
// every forwarded query.
func (engine *mdnsEngine) shouldLogQuery(key string) bool {
	engine.queryLogMu.Lock()
	defer engine.queryLogMu.Unlock()
	now := time.Now()
	if previous, found := engine.lastQueryLog[key]; found && now.Sub(previous) < 30*time.Second {
		return false
	}
	engine.lastQueryLog[key] = now
	return true
}

// recordRemoteQuery updates WebUI state and emits a concise client-discovery log.
func (engine *mdnsEngine) recordRemoteQuery(endpoint *mdnsEndpoint, source net.IP, names []string) {
	for _, name := range names {
		service, exactBrowse := func() (string, bool) {
			if _, exact := raymarineServiceTypes[name]; exact {
				return name, true
			}
			return serviceForName(name), false
		}()
		if !exactBrowse {
			// Follow-up host/instance lookups are useful in Debug logs but too
			// noisy for the normal event stream.
			debugf("Raymarine app/client resolution query source=%s interface=%s name=%s", source, endpoint.name, name)
			continue
		}
		engine.state.update(map[string]string{
			"last_remote_ip":      source.String(),
			"last_remote_iface":   endpoint.name,
			"last_remote_service": service,
			"last_activity":       time.Now().Format(time.RFC3339),
		})
		key := source.String() + "|" + endpoint.name + "|" + service
		if engine.shouldLogQuery(key) {
			infof("Raymarine app/client query detected source=%s interface=%s service=%s", source, endpoint.name, service)
		}
		debugf("remote query source=%s interface=%s name=%s", source, endpoint.name, name)
	}
}

// observe ingests Axiom-side response records, learns follow-up DNS names and
// emits friendly device/service detection events.
func (tracker *discoveryTracker) observe(message *dnsMessage, cache *nameCache) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	// PTR records identify the DNS-SD service instance.
	for _, record := range message.Records {
		if record.Type != 12 || record.Target == "" {
			continue
		}
		service := serviceForName(record.Name)
		if service == "" || service == browseType {
			continue
		}
		cache.add(record.Target)
		observation := tracker.observations[record.Target]
		if observation == nil {
			observation = &serviceObservation{Service: service, Instance: record.Target}
			tracker.observations[record.Target] = observation
		} else if observation.Service == "" {
			observation.Service = service
		}
	}

	// SRV records provide the direct host and port used after discovery.
	for _, record := range message.Records {
		if record.Type != 33 || record.Target == "" {
			continue
		}
		service := serviceForName(record.Name)
		observation := tracker.observations[record.Name]
		if observation == nil && service != "" && service != browseType {
			observation = &serviceObservation{Service: service, Instance: record.Name}
			tracker.observations[record.Name] = observation
		}
		if observation != nil {
			observation.Host = record.Target
			observation.Port = record.Port
			cache.add(record.Name)
			cache.add(record.Target)
		}
	}

	// A/AAAA records resolve previously learned SRV hostnames.
	for _, record := range message.Records {
		if (record.Type == 1 || record.Type == 28) && record.Address != "" {
			tracker.hostAddresses[record.Name] = record.Address
		}
	}

	for _, observation := range tracker.observations {
		if observation.Host != "" {
			observation.IP = tracker.hostAddresses[observation.Host]
		}
		if _, isMFD := mfdServiceTypes[observation.Service]; !isMFD {
			continue
		}
		tracker.emitObservation(observation)
	}
}

// displayInstance strips the service suffix from a DNS-SD instance for logs.
func displayInstance(instance, service string) string {
	name := instance
	suffix := "." + service
	if strings.HasSuffix(name, suffix) {
		name = strings.TrimSuffix(name, suffix)
	}
	if name == "" {
		return instance
	}
	return name
}

func (tracker *discoveryTracker) emitObservation(observation *serviceObservation) {
	name := displayInstance(observation.Instance, observation.Service)
	port := strconv.Itoa(int(observation.Port))
	signature := observation.IP + "|" + port
	serviceKey := observation.Service + "|" + observation.Instance
	if tracker.loggedServices[serviceKey] == signature {
		return
	}
	tracker.loggedServices[serviceKey] = signature

	// Wait for an address before announcing a complete MFD device in Info logs.
	if observation.IP == "" {
		debugf("Axiom/MFD service discovered name=%s service=%s port=%d awaiting-address", name, observation.Service, observation.Port)
		return
	}

	deviceKey := observation.Instance + "|" + observation.IP
	if tracker.loggedDevices[deviceKey] == "" {
		tracker.loggedDevices[deviceKey] = observation.Service
		infof("Axiom/MFD detected name=%q address=%s service=%s port=%d", name, observation.IP, observation.Service, observation.Port)
	} else {
		switch observation.Service {
		case rymServiceType:
			infof("Axiom remote-control service detected name=%q address=%s port=%d", name, observation.IP, observation.Port)
		case rayDBServiceType:
			infof("Axiom RayDB service detected name=%q address=%s port=%d", name, observation.IP, observation.Port)
		default:
			debugf("Axiom service updated name=%q address=%s service=%s port=%d", name, observation.IP, observation.Service, observation.Port)
		}
	}
	tracker.state.update(map[string]string{
		"last_axiom_name":    name,
		"last_axiom_ip":      observation.IP,
		"last_axiom_service": observation.Service,
		"last_axiom_port":    port,
		"last_activity":      time.Now().Format(time.RFC3339),
	})
}

// runEndpoint handles publisher replies and one-way star-topology reflection:
// app-network queries go to RayNet, RayNet responses go to every app network.
func (engine *mdnsEngine) runEndpoint(ctx context.Context, endpoint *mdnsEndpoint) {
	defer engine.wg.Done()
	buffer := make([]byte, 64*1024)
	for {
		_ = endpoint.conn.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
		count, source, err := endpoint.conn.ReadFromUDP(buffer)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			infof("mDNS receive failed interface=%s error=%v", endpoint.name, err)
			continue
		}

		packet := append([]byte(nil), buffer[:count]...)
		message, parseErr := parseDNSMessage(packet)
		if parseErr != nil {
			debugf("ignored malformed mDNS packet interface=%s source=%s error=%v", endpoint.name, source.IP, parseErr)
			continue
		}

		// Reply directly to YachtSense discovery on every interface where the
		// emulator intentionally publishes the YachtSense service.
		if endpoint.publish && containsOwnQuestion(message, engine.ownNames) {
			time.Sleep(time.Duration(20+time.Now().UnixNano()%80) * time.Millisecond)
			endpoint.send(endpoint.announcement)
			debugf("answered YachtSense mDNS query source=%s interface=%s", source.IP, endpoint.name)
		}

		if endpoint == engine.axiom {
			if message.Response {
				if responseRelevant(message, engine.cache) {
					engine.tracker.observe(message, engine.cache)
					if engine.relayActive {
						for _, remote := range engine.remotes {
							if remote == engine.axiom {
								continue
							}
							remote.send(packet)
							debugf("relayed Raymarine response %s -> %s bytes=%d", endpoint.name, remote.name, len(packet))
						}
					}
				}
			} else if !engine.relayActive {
				// When app and Axiom already share this bridge, relevant queries
				// are still useful diagnostics even though no relay is needed.
				names := relevantQueryNames(message, engine.cache)
				if len(names) > 0 {
					engine.recordRemoteQuery(endpoint, source.IP, names)
				}
			}
			continue
		}

		// Only client queries travel from a configured app interface to RayNet.
		if !message.Response && engine.relayActive {
			names := relevantQueryNames(message, engine.cache)
			if len(names) == 0 {
				continue
			}
			engine.recordRemoteQuery(endpoint, source.IP, names)
			engine.axiom.send(packet)
			debugf("relayed Raymarine query %s -> %s bytes=%d", endpoint.name, engine.axiom.name, len(packet))
		}
	}
}

// run starts all mDNS endpoint readers and periodic direct advertisements.
func (engine *mdnsEngine) run(ctx context.Context) {
	infof("mDNS engine starting AxiomInterface=%s AxiomIP=%s remoteInterfaces=%s relay=%v reason=%q",
		engine.cfg.AxiomInterface,
		engine.cfg.AxiomIP,
		strings.Join(engine.cfg.RemoteInterfaces, ","),
		engine.relayActive,
		engine.relayReason,
	)
	for _, endpoint := range engine.endpoints {
		if endpoint.publish {
			infof("YachtSense advertisement active interface=%s address=%s service=%q id=%s %s",
				endpoint.name, endpoint.ip, engine.cfg.Instance, productID, engine.cfg.Serial)
		}
		engine.wg.Add(1)
		go engine.runEndpoint(ctx, endpoint)
	}
	go engine.periodicallyAdvertise(ctx)
}

// close sends zero-TTL goodbye packets and releases all multicast sockets.
func (engine *mdnsEngine) close() {
	if engine == nil {
		return
	}
	engine.closeOnce.Do(func() {
		for _, endpoint := range engine.endpoints {
			if endpoint.publish {
				endpoint.send(endpoint.goodbye)
			}
		}
		time.Sleep(80 * time.Millisecond)
		for _, endpoint := range engine.endpoints {
			_ = endpoint.conn.Close()
		}
		engine.wg.Wait()
		infof("mDNS engine stopped")
	})
}

// startHealthServer starts the optional HTTP 200 responder used by the Axiom
// internet-source connection monitor. It binds only to the RayNet-side address.
func startHealthServer(ctx context.Context, cfg configuration) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		debugf("HTTP health request source=%s path=%s", request.RemoteAddr, request.URL.Path)
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("OK\n"))
	})

	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.AxiomIP.String(), strconv.Itoa(cfg.HealthPort)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       15 * time.Second,
	}
	listener, err := net.Listen("tcp4", server.Addr)
	if err != nil {
		return nil, err
	}
	infof("HTTP health service listening url=http://%s/", server.Addr)

	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			infof("HTTP health service failed: %v", err)
		}
	}()
	return server, nil
}

// main coordinates configuration, selective mDNS behavior and graceful stop.
func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
	log.SetPrefix("yachtsense-link-emulator: ")

	cfg, err := loadConfiguration()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	debugLogging = cfg.LogLevel == "debug"
	infof("starting package version=%s logLevel=%s", packageVersion, cfg.LogLevel)
	state := newRuntimeState(cfg.StateFile)
	state.update(map[string]string{
		"relay_reason":  "initializing",
		"last_activity": time.Now().Format(time.RFC3339),
	})

	if !cfg.MDNSEnabled && !cfg.WebEnabled && cfg.RelayMode == "disabled" {
		infof("mDNS publisher, HTTP health and discovery relay are all disabled; exiting")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// Open mDNS sockets whenever we publish, relay, or passively observe an
	// existing reflector for status/logging. If only passive observation fails,
	// the HTTP health service can still run.
	var engine *mdnsEngine
	needMDNS := cfg.MDNSEnabled || cfg.RelayMode != "disabled"
	if needMDNS {
		engine, err = newMDNSEngine(cfg, state)
		if err != nil {
			if cfg.MDNSEnabled || cfg.RelayMode == "force" || !cfg.ExistingReflector {
				log.Fatalf("mDNS startup failed: %v", err)
			}
			infof("passive mDNS observation unavailable while existing reflector is active: %v", err)
			state.update(map[string]string{"relay_active": "0", "relay_reason": "existing reflector active; local observer could not bind UDP/5353"})
		} else {
			engine.run(ctx)
		}
	}

	var healthServer *http.Server
	if cfg.WebEnabled {
		healthServer, err = startHealthServer(ctx, cfg)
		if err != nil {
			if engine != nil {
				engine.close()
			}
			log.Fatalf("HTTP health service startup failed: %v", err)
		}
	}

	<-ctx.Done()
	infof("shutdown requested")
	if engine != nil {
		engine.close()
	}
	if healthServer != nil {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = healthServer.Shutdown(shutdownContext)
		shutdownCancel()
	}
	state.update(map[string]string{"relay_active": "0", "relay_reason": "service stopped"})
	infof("stopped")
}
