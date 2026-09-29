// YachtSense protocol implementation adapted from Roel Broersma's v1.0.13.
// Names, identity TXT records and the SRV port preserve the existing emulator.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	mdnsAddress           = "224.0.0.251"
	mdnsPort              = 5353
	yachtSenseServiceType = "_http._tcp.local"
	browseType            = "_services._dns-sd._udp.local"
	rtspServiceType       = "_rtsp._tcp.local"
	rymServiceType        = "_rym_rrc._tcp.local"
	rayDBServiceType      = "_raydb._tcp.local"
	productID             = "E70640"
	modelName             = "Raymarine YachtSense Link"
	defaultTTL            = 120
)

var raymarineServiceTypes = map[string]bool{yachtSenseServiceType: true, browseType: true, rtspServiceType: true, rymServiceType: true, rayDBServiceType: true}
var mfdServiceTypes = map[string]bool{rtspServiceType: true, rymServiceType: true, rayDBServiceType: true}

type dnsQuestion struct {
	Name        string
	Type, Class uint16
	Wire        []byte
}
type dnsRecord struct {
	Name                   string
	Type, Class            uint16
	TTL                    uint32
	Target                 string
	Port                   uint16
	Address                string
	Data, Wire, TargetWire []byte
}
type dnsMessage struct {
	ID, Flags uint16
	Response  bool
	Questions []dnsQuestion
	Records   []dnsRecord
}
type configuration struct {
	AxiomInterface                       string
	AxiomIP                              net.IP
	RemoteInterfaces                     []string
	Serial, Version, HostLabel, Instance string
	HealthPort                           int
	MDNSEnabled, WebEnabled              bool
	TTL                                  uint32
	RelayMode                            string
	ExistingReflector                    bool
	ReflectorReason, LogLevel, StateFile string
}

func normalizeDNSName(name string) string { return strings.ToLower(strings.TrimSuffix(name, ".")) }
func cleanSerial(v string) string {
	v = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, strings.TrimSpace(v))
	if v == "" {
		v = "AF002A4"
	}
	if len(v) > 32 {
		v = v[:32]
	}
	return v
}
func encodeDNSName(name string) ([]byte, error) {
	if name == "" || name == "." {
		return []byte{0}, nil
	}
	var out []byte
	for _, label := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, errors.New("invalid DNS label")
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	out = append(out, 0)
	if len(out) > 255 {
		return nil, errors.New("DNS name too long")
	}
	return out, nil
}

// Retain the uncompressed wire name as well as the display name. Re-encoding
// the display name would corrupt a DNS-SD label containing a literal dot.
func decodeName(packet []byte, start, depth int) (string, int, []byte, error) {
	if start < 0 || start >= len(packet) || depth > 12 {
		return "", start, nil, errors.New("invalid DNS name/pointer")
	}
	off := start
	next := start
	var wire []byte
	var labels []string
	for {
		if off >= len(packet) {
			return "", next, nil, errors.New("unterminated DNS name")
		}
		n := int(packet[off])
		off++
		if n == 0 {
			wire = append(wire, 0)
			next = off
			break
		}
		if n&0xc0 == 0xc0 {
			if off >= len(packet) {
				return "", next, nil, errors.New("truncated DNS pointer")
			}
			ptr := ((n & 0x3f) << 8) | int(packet[off])
			if ptr >= off-1 {
				return "", next, nil, errors.New("forward or cyclic DNS pointer")
			}
			name, _, tail, err := decodeName(packet, ptr, depth+1)
			if err != nil {
				return "", next, nil, err
			}
			wire = append(wire, tail...)
			if name != "" {
				labels = append(labels, name)
			}
			next = off + 1
			break
		}
		if n > 63 || off+n > len(packet) {
			return "", next, nil, errors.New("invalid DNS label")
		}
		wire = append(wire, byte(n))
		wire = append(wire, packet[off:off+n]...)
		labels = append(labels, string(packet[off:off+n]))
		off += n
		if len(wire) > 254 {
			return "", next, nil, errors.New("DNS name too long")
		}
	}
	if len(wire) > 255 {
		return "", next, nil, errors.New("DNS name too long")
	}
	return normalizeDNSName(strings.Join(labels, ".")), next, wire, nil
}
func decodeDNSName(p []byte, start, depth int) (string, int, error) {
	n, next, _, err := decodeName(p, start, depth)
	return n, next, err
}
func appendUint16(p []byte, v uint16) []byte { return append(p, byte(v>>8), byte(v)) }
func appendUint32(p []byte, v uint32) []byte {
	return append(p, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}
func appendResourceRecord(p []byte, name string, typ, class uint16, ttl uint32, data []byte) ([]byte, error) {
	wire, err := encodeDNSName(name)
	if err != nil {
		return nil, err
	}
	return appendRR(p, wire, typ, class, ttl, data)
}
func appendRR(p, wire []byte, typ, class uint16, ttl uint32, data []byte) ([]byte, error) {
	if len(data) > 65535 {
		return nil, errors.New("oversize RDATA")
	}
	p = append(p, wire...)
	p = appendUint16(p, typ)
	p = appendUint16(p, class)
	p = appendUint32(p, ttl)
	p = appendUint16(p, uint16(len(data)))
	return append(p, data...), nil
}
func buildTXTData(values []string) ([]byte, error) {
	var out []byte
	for _, v := range values {
		if len(v) > 255 {
			return nil, errors.New("oversize TXT item")
		}
		out = append(out, byte(len(v)))
		out = append(out, v...)
	}
	return out, nil
}
func buildAnnouncement(c configuration, iface *net.Interface, ip net.IP, ttl uint32) ([]byte, error) {
	if ip.To4() == nil {
		return nil, errors.New("IPv4 address required")
	}
	instance := c.Instance + "." + yachtSenseServiceType
	host := c.HostLabel + ".local"
	iw, e := encodeDNSName(instance)
	if e != nil {
		return nil, e
	}
	hw, e := encodeDNSName(host)
	if e != nil {
		return nil, e
	}
	sw, _ := encodeDNSName(yachtSenseServiceType)
	mac := "02:00:00:00:00:01"
	if iface != nil && len(iface.HardwareAddr) > 0 {
		mac = iface.HardwareAddr.String()
	}
	txt, e := buildTXTData([]string{"id=" + productID + " " + c.Serial, "model=" + modelName, "version=" + c.Version, "mac=" + strings.ToLower(mac)})
	if e != nil {
		return nil, e
	}
	// SRV 80 is identity metadata; the independent Axiom health listener is 7777.
	srv := append([]byte{0, 0, 0, 0, 0, 80}, hw...)
	p := make([]byte, 12)
	binary.BigEndian.PutUint16(p[2:4], 0x8400)
	binary.BigEndian.PutUint16(p[6:8], 5)
	for _, r := range []struct {
		n     string
		t, cl uint16
		d     []byte
	}{{yachtSenseServiceType, 12, 1, iw}, {instance, 33, 0x8001, srv}, {instance, 16, 0x8001, txt}, {host, 1, 0x8001, ip.To4()}, {browseType, 12, 1, sw}} {
		p, e = appendResourceRecord(p, r.n, r.t, r.cl, ttl, r.d)
		if e != nil {
			return nil, e
		}
	}
	return p, nil
}
func parseDNSMessage(p []byte) (*dnsMessage, error) {
	if len(p) < 12 {
		return nil, errors.New("short DNS header")
	}
	m := &dnsMessage{ID: binary.BigEndian.Uint16(p[:2]), Flags: binary.BigEndian.Uint16(p[2:4])}
	m.Response = m.Flags&0x8000 != 0
	if m.Flags&0x780f != 0 {
		return nil, errors.New("unsupported DNS opcode/rcode")
	}
	q := int(binary.BigEndian.Uint16(p[4:6]))
	nr := int(binary.BigEndian.Uint16(p[6:8])) + int(binary.BigEndian.Uint16(p[8:10])) + int(binary.BigEndian.Uint16(p[10:12]))
	if q+nr > 1024 {
		return nil, errors.New("too many DNS entries")
	}
	off := 12
	for i := 0; i < q; i++ {
		name, next, wire, err := decodeName(p, off, 0)
		if err != nil {
			return nil, err
		}
		off = next
		if off+4 > len(p) {
			return nil, errors.New("short question")
		}
		m.Questions = append(m.Questions, dnsQuestion{Name: name, Type: binary.BigEndian.Uint16(p[off:]), Class: binary.BigEndian.Uint16(p[off+2:]), Wire: wire})
		off += 4
	}
	for i := 0; i < nr; i++ {
		name, next, wire, err := decodeName(p, off, 0)
		if err != nil {
			return nil, err
		}
		off = next
		if off+10 > len(p) {
			return nil, errors.New("short RR")
		}
		r := dnsRecord{Name: name, Wire: wire, Type: binary.BigEndian.Uint16(p[off:]), Class: binary.BigEndian.Uint16(p[off+2:]), TTL: binary.BigEndian.Uint32(p[off+4:])}
		n := int(binary.BigEndian.Uint16(p[off+8:]))
		off += 10
		end := off + n
		if end > len(p) {
			return nil, errors.New("short RDATA")
		}
		r.Data = append([]byte(nil), p[off:end]...)
		switch r.Type {
		case 12, 33:
			at := off
			if r.Type == 33 {
				if n < 7 {
					return nil, errors.New("short SRV")
				}
				r.Port = binary.BigEndian.Uint16(p[off+4:])
				at += 6
			}
			r.Target, next, r.TargetWire, err = decodeName(p, at, 0)
			if err != nil || next != end {
				return nil, errors.New("invalid PTR/SRV RDATA")
			}
		case 1:
			if n != 4 {
				return nil, errors.New("invalid A record")
			}
			r.Address = net.IP(r.Data).String()
		case 28:
			if n != 16 {
				return nil, errors.New("invalid AAAA record")
			}
			r.Address = net.IP(r.Data).String()
		case 16:
			for j := 0; j < n; {
				l := int(r.Data[j])
				j++
				if j+l > n {
					return nil, errors.New("invalid TXT record")
				}
				j += l
			}
		}
		m.Records = append(m.Records, r)
		off = end
	}
	return m, nil
}
func serviceForName(name string) string {
	name = normalizeDNSName(name)
	if raymarineServiceTypes[name] {
		return name
	}
	for s := range raymarineServiceTypes {
		if s != browseType && strings.HasSuffix(name, "."+s) {
			return s
		}
	}
	return ""
}
func isRelevantHTTPInstance(n string) bool {
	n = strings.ToLower(n)
	return strings.Contains(n, "yachtsense-main") || strings.Contains(n, "imx8mmevk") || strings.Contains(n, "rds webserver") || (strings.Contains(n, "raymarinerds") && strings.Contains(n, "cloudconnector"))
}

type nameCache struct {
	mu      sync.Mutex
	expires map[string]time.Time
}

func newNameCache() *nameCache    { return &nameCache{expires: map[string]time.Time{}} }
func (c *nameCache) add(n string) { c.addTTL(n, 120) }
func (c *nameCache) addTTL(n string, ttl uint32) {
	n = normalizeDNSName(n)
	if n == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if ttl > 900 {
		ttl = 900
	}
	if ttl == 0 {
		delete(c.expires, n)
		return
	}
	if len(c.expires) >= 1024 {
		for k, v := range c.expires {
			if now.After(v) {
				delete(c.expires, k)
			}
		}
		if len(c.expires) >= 1024 {
			for k := range c.expires {
				delete(c.expires, k)
				break
			}
		}
	}
	c.expires[n] = now.Add(time.Duration(ttl) * time.Second)
}
func (c *nameCache) has(n string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	n = normalizeDNSName(n)
	e, ok := c.expires[n]
	if !ok {
		return false
	}
	if time.Now().After(e) {
		delete(c.expires, n)
		return false
	}
	return true
}
func relevantQueryNames(m *dnsMessage, c *nameCache) []string {
	var out []string
	seen := map[string]bool{}
	for _, q := range m.Questions {
		if q.Class&0x7fff != 1 {
			continue
		}
		n := normalizeDNSName(q.Name)
		if (raymarineServiceTypes[n] || c.has(n)) && !seen[n] {
			out = append(out, n)
			seen[n] = true
		}
	}
	return out
}
func allowedInstance(n string) bool {
	s := serviceForName(n)
	return mfdServiceTypes[s] || (s == yachtSenseServiceType && isRelevantHTTPInstance(n))
}
func responseRelevant(m *dnsMessage, c *nameCache) bool {
	// Learn only permitted instance and host names, never all names in a packet.
	relevant := false
	for _, r := range m.Records {
		if r.Class&0x7fff != 1 {
			continue
		}
		if r.Type == 12 && (mfdServiceTypes[r.Name] || r.Name == yachtSenseServiceType && isRelevantHTTPInstance(r.Target) || r.Name == browseType && raymarineServiceTypes[r.Target] && r.Target != browseType) {
			relevant = true
			c.addTTL(r.Target, r.TTL)
		}
	}
	for _, r := range m.Records {
		if r.Class&0x7fff != 1 {
			continue
		}
		if allowedInstance(r.Name) || c.has(r.Name) {
			relevant = true
			if r.Type == 33 && r.Target != "" {
				c.addTTL(r.Target, r.TTL)
			}
		}
	}
	return relevant
}

// A mixed Bonjour datagram must not leak unrelated printer/services records.
// The old packet-level allow decision forwarded the entire original packet.
func filterDNS(m *dnsMessage, c *nameCache, ownHost, ownInstance string) ([]byte, error) {
	p := make([]byte, 12)
	if m.Response {
		binary.BigEndian.PutUint16(p[2:4], 0x8400)
		responseRelevant(m, c)
	}
	count := 0
	if !m.Response {
		for _, q := range m.Questions {
			if q.Class&0x7fff != 1 || (!raymarineServiceTypes[q.Name] && !c.has(q.Name)) {
				continue
			}
			w := q.Wire
			if len(w) == 0 {
				w, _ = encodeDNSName(q.Name)
			}
			p = append(p, w...)
			p = appendUint16(p, q.Type)
			p = appendUint16(p, 1)
			count++
		}
		binary.BigEndian.PutUint16(p[4:6], uint16(count))
	} else {
		packetHosts := map[string]bool{}
		for _, r := range m.Records {
			if r.Type == 33 && allowedInstance(r.Name) {
				packetHosts[r.Target] = true
			}
		}
		for _, r := range m.Records {
			if r.Class&0x7fff != 1 || (ownHost != "" && r.Name == ownHost) || (ownInstance != "" && (r.Name == ownInstance || r.Target == ownInstance)) {
				continue
			}
			allow := false
			switch r.Type {
			case 12:
				allow = mfdServiceTypes[r.Name] || r.Name == yachtSenseServiceType && isRelevantHTTPInstance(r.Target) || r.Name == browseType && raymarineServiceTypes[r.Target] && r.Target != browseType
			case 33, 16:
				allow = allowedInstance(r.Name) || c.has(r.Name)
			case 1, 28:
				allow = c.has(r.Name) || packetHosts[r.Name]
			}
			if !allow {
				continue
			}
			d := r.Data
			if r.Type == 12 {
				d = r.TargetWire
			}
			if r.Type == 33 {
				d = append(append([]byte(nil), r.Data[:6]...), r.TargetWire...)
			}
			var e error
			p, e = appendRR(p, r.Wire, r.Type, r.Class, r.TTL, d)
			if e != nil {
				return nil, e
			}
			count++
		}
		binary.BigEndian.PutUint16(p[6:8], uint16(count))
	}
	if count == 0 {
		return nil, nil
	}
	if len(p) > 9000 {
		return nil, fmt.Errorf("filtered DNS response too large")
	}
	return p, nil
}
func determineRelayMode(c configuration, distinct bool) (bool, string) {
	if !distinct {
		return false, "Direct discovery on the same interface"
	}
	switch c.RelayMode {
	case "disabled":
		return false, "Built-in relay disabled"
	case "force":
		return true, "Built-in relay selected"
	default:
		if c.ExistingReflector {
			return false, "Existing Avahi reflector selected"
		}
		return true, "Automatic built-in Raymarine relay"
	}
}
