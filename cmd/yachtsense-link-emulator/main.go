// YachtSense Link Emulator publishes the network identity that a Raymarine
// Axiom expects from a YachtSense Link router. The program is intentionally
// small and dependency-free so it can run as a static ARMv7 binary on RutOS.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// Standard mDNS multicast endpoint defined by RFC 6762.
	mdnsAddress = "224.0.0.251"
	mdnsPort    = 5353

	// YachtSense Link is discovered as an HTTP DNS-SD service.
	serviceType = "_http._tcp.local"
	browseType  = "_services._dns-sd._udp.local"
	productID   = "E70640"
	modelName   = "Raymarine YachtSense Link"

	// A short TTL keeps stale advertisements from lingering after shutdown.
	defaultTTL = 120
)

// packageVersion is replaced by the build script through a Go linker flag.
var packageVersion = "development"

// configuration contains all settings passed by the RutOS procd service.
type configuration struct {
	Interface   string
	IP          net.IP
	Serial      string
	Version     string
	HostLabel   string
	Instance    string
	HealthPort  int
	MDNSEnabled bool
	WebEnabled  bool
	TTL         uint32
}

// mdnsServer owns the multicast socket and the pre-built announcement packets.
type mdnsServer struct {
	cfg        configuration
	iface      *net.Interface
	conn       *net.UDPConn
	group      *net.UDPAddr
	packet     []byte
	goodbye    []byte
	instance   string
	hostname   string
	closeOnce  sync.Once
	periodicWG sync.WaitGroup
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

// cleanDNSLabel removes control characters and dots from a single DNS label.
func cleanDNSLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}

	// Dots would split the value into multiple labels, which the UI does not
	// expose and which is unnecessary for this local service.
	value = strings.ReplaceAll(value, ".", "-")
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)

	// DNS labels are limited to 63 octets. The configured values are ASCII in
	// normal use, so a byte truncation is appropriate here.
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
		Interface:   strings.TrimSpace(os.Getenv("YSL_IFACE")),
		IP:          net.ParseIP(strings.TrimSpace(os.Getenv("YSL_IP"))).To4(),
		Serial:      cleanSerial(os.Getenv("YSL_SERIAL")),
		Version:     strings.TrimSpace(os.Getenv("YSL_VERSION")),
		HostLabel:   cleanDNSLabel(os.Getenv("YSL_HOSTNAME"), "yachtsense-main"),
		Instance:    cleanDNSLabel(os.Getenv("YSL_INSTANCE"), "yachtsense-main Settings"),
		MDNSEnabled: environmentBoolean("YSL_MDNS_ENABLED", true),
		WebEnabled:  environmentBoolean("YSL_WEB_ENABLED", true),
		TTL:         defaultTTL,
	}

	if cfg.Interface == "" {
		cfg.Interface = "br-lan"
	}
	if cfg.IP == nil {
		return configuration{}, errors.New("YSL_IP is not a valid IPv4 address")
	}
	if cfg.Version == "" {
		cfg.Version = "V142.242.530"
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

	// A zero-length label terminates a fully qualified DNS name.
	return append(encoded, 0), nil
}

// appendUint16 appends an unsigned 16-bit value in network byte order.
func appendUint16(destination []byte, value uint16) []byte {
	var buffer [2]byte
	binary.BigEndian.PutUint16(buffer[:], value)
	return append(destination, buffer[:]...)
}

// appendUint32 appends an unsigned 32-bit value in network byte order.
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

// buildAnnouncement creates the unsolicited mDNS response used for discovery.
func buildAnnouncement(cfg configuration, iface *net.Interface, ttl uint32) ([]byte, error) {
	instanceName := cfg.Instance + "." + serviceType
	hostName := cfg.HostLabel + ".local"

	encodedInstance, err := encodeDNSName(instanceName)
	if err != nil {
		return nil, err
	}
	encodedHost, err := encodeDNSName(hostName)
	if err != nil {
		return nil, err
	}
	encodedService, err := encodeDNSName(serviceType)
	if err != nil {
		return nil, err
	}

	// Use the selected interface MAC in the identity advertised to the Axiom.
	macAddress := "02:00:00:00:00:01"
	if iface != nil && len(iface.HardwareAddr) > 0 {
		macAddress = strings.ToLower(iface.HardwareAddr.String())
	}

	// The product ID is the critical marker observed in the Axiom firmware.
	txtData, err := buildTXTData([]string{
		"id=" + productID + " " + cfg.Serial,
		"model=" + modelName,
		"version=" + cfg.Version,
		"mac=" + macAddress,
	})
	if err != nil {
		return nil, err
	}

	// The DNS-SD SRV record points to port 80, matching a real YachtSense Link.
	// RutOS already normally owns TCP port 80; the separate health responder is
	// exposed on port 7777 and is not the SRV endpoint.
	srvData := make([]byte, 0, len(encodedHost)+6)
	srvData = appendUint16(srvData, 0)
	srvData = appendUint16(srvData, 0)
	srvData = appendUint16(srvData, 80)
	srvData = append(srvData, encodedHost...)

	// Create an authoritative response with five answer records.
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[2:4], 0x8400)
	binary.BigEndian.PutUint16(packet[6:8], 5)

	packet, err = appendResourceRecord(packet, serviceType, 12, 0x0001, ttl, encodedInstance)
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
	packet, err = appendResourceRecord(packet, hostName, 1, 0x8001, ttl, []byte(cfg.IP.To4()))
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
			return strings.ToLower(strings.Join(labels, ".")), next, nil
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
			return strings.ToLower(strings.Join(labels, ".")), next, nil
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

// containsInterestingQuestion limits replies to names belonging to this service.
func containsInterestingQuestion(packet []byte, names map[string]struct{}) bool {
	if len(packet) < 12 || binary.BigEndian.Uint16(packet[2:4])&0x8000 != 0 {
		return false
	}

	questionCount := int(binary.BigEndian.Uint16(packet[4:6]))
	offset := 12
	for question := 0; question < questionCount; question++ {
		name, next, err := decodeDNSName(packet, offset, 0)
		if err != nil {
			return false
		}
		offset = next
		if offset+4 > len(packet) {
			return false
		}
		offset += 4
		if _, found := names[name]; found {
			return true
		}
	}
	return false
}

// openMDNSSocket creates a reusable UDP socket bound to the standard mDNS port.
func openMDNSSocket(sourceIP net.IP) (*net.UDPConn, error) {
	listenConfig := net.ListenConfig{
		Control: func(_ string, _ string, raw syscall.RawConn) error {
			var controlError error
			if err := raw.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
					controlError = err
					return
				}
				_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 0x0f, 1)
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
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1)
	}); err != nil || socketError != nil {
		_ = connection.Close()
		if err != nil {
			return nil, err
		}
		return nil, socketError
	}

	_ = connection.SetReadBuffer(64 * 1024)
	return connection, nil
}

// newMDNSServer validates the selected interface and prepares both packets.
func newMDNSServer(cfg configuration) (*mdnsServer, error) {
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", cfg.Interface, err)
	}

	addressPresent := false
	addresses, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("read addresses on %s: %w", cfg.Interface, err)
	}
	for _, address := range addresses {
		ip, _, parseError := net.ParseCIDR(address.String())
		if parseError == nil && ip.To4() != nil && ip.Equal(cfg.IP) {
			addressPresent = true
			break
		}
	}
	if !addressPresent {
		return nil, fmt.Errorf("configured IP %s is not present on %s", cfg.IP.String(), cfg.Interface)
	}

	connection, err := openMDNSSocket(cfg.IP)
	if err != nil {
		return nil, err
	}

	announcement, err := buildAnnouncement(cfg, iface, cfg.TTL)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	goodbye, err := buildAnnouncement(cfg, iface, 0)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}

	return &mdnsServer{
		cfg:      cfg,
		iface:    iface,
		conn:     connection,
		group:    &net.UDPAddr{IP: net.ParseIP(mdnsAddress), Port: mdnsPort},
		packet:   announcement,
		goodbye:  goodbye,
		instance: strings.ToLower(cfg.Instance + "." + serviceType),
		hostname: strings.ToLower(cfg.HostLabel + ".local"),
	}, nil
}

// announce transmits a prepared mDNS response to the multicast group.
func (server *mdnsServer) announce(packet []byte) {
	if _, err := server.conn.WriteToUDP(packet, server.group); err != nil && !errors.Is(err, net.ErrClosed) {
		log.Printf("mDNS send failed: %v", err)
	}
}

// run publishes periodic announcements and answers relevant mDNS questions.
func (server *mdnsServer) run(ctx context.Context) {
	log.Printf(
		"mDNS advertising %q as id=%s %s on %s/%s",
		server.cfg.Instance,
		productID,
		server.cfg.Serial,
		server.cfg.Interface,
		server.cfg.IP,
	)

	server.announce(server.packet)
	initialAnnouncement := time.NewTimer(time.Second)
	periodicAnnouncement := time.NewTicker(time.Minute)
	server.periodicWG.Add(1)
	go func() {
		defer server.periodicWG.Done()
		defer periodicAnnouncement.Stop()
		defer initialAnnouncement.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-initialAnnouncement.C:
				server.announce(server.packet)
			case <-periodicAnnouncement.C:
				server.announce(server.packet)
			}
		}
	}()

	interestingNames := map[string]struct{}{
		strings.ToLower(serviceType): {},
		server.instance:              {},
		server.hostname:              {},
		strings.ToLower(browseType):  {},
	}

	buffer := make([]byte, 64*1024)
	for {
		_ = server.conn.SetReadDeadline(time.Now().Add(750 * time.Millisecond))
		count, _, err := server.conn.ReadFromUDP(buffer)
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
			log.Printf("mDNS receive failed: %v", err)
			continue
		}

		if containsInterestingQuestion(buffer[:count], interestingNames) {
			delay := time.Duration(20+time.Now().UnixNano()%80) * time.Millisecond
			time.Sleep(delay)
			server.announce(server.packet)
		}
	}
}

// close sends a zero-TTL goodbye packet and releases the multicast socket.
func (server *mdnsServer) close() {
	server.closeOnce.Do(func() {
		server.announce(server.goodbye)
		time.Sleep(80 * time.Millisecond)
		_ = server.conn.Close()
		server.periodicWG.Wait()
		log.Printf("mDNS responder stopped")
	})
}

// startHealthServer starts the optional HTTP 200 responder used for probing.
func startHealthServer(ctx context.Context, cfg configuration) (*http.Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("OK\n"))
	})

	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.IP.String(), strconv.Itoa(cfg.HealthPort)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       15 * time.Second,
	}

	listener, err := net.Listen("tcp4", server.Addr)
	if err != nil {
		return nil, err
	}
	log.Printf("HTTP health service listening on http://%s/", server.Addr)

	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP health service failed: %v", err)
		}
	}()

	return server, nil
}

// main coordinates configuration, component startup and graceful shutdown.
func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)
	log.SetPrefix("yachtsense-link-emulator: ")
	log.Printf("starting package version %s", packageVersion)

	cfg, err := loadConfiguration()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	if !cfg.MDNSEnabled && !cfg.WebEnabled {
		log.Printf("mDNS and HTTP health service are both disabled; exiting")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	var multicast *mdnsServer
	if cfg.MDNSEnabled {
		multicast, err = newMDNSServer(cfg)
		if err != nil {
			log.Fatalf("mDNS startup failed: %v", err)
		}
		go multicast.run(ctx)
	}

	var healthServer *http.Server
	if cfg.WebEnabled {
		healthServer, err = startHealthServer(ctx, cfg)
		if err != nil {
			if multicast != nil {
				multicast.close()
			}
			log.Fatalf("HTTP health service startup failed: %v", err)
		}
	}

	<-ctx.Done()
	log.Printf("shutdown requested")

	if multicast != nil {
		multicast.close()
	}
	if healthServer != nil {
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = healthServer.Shutdown(shutdownContext)
		shutdownCancel()
	}

	log.Printf("stopped")
}
