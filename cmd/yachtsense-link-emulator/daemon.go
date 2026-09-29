package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Seen struct {
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Interface string    `json:"interface,omitempty"`
	Service   string    `json:"service"`
	Port      uint16    `json:"port,omitempty"`
	LastSeen  time.Time `json:"last_seen"`
	Expires   time.Time `json:"expires"`
}
type Runtime struct {
	PID           int       `json:"pid"`
	ProcStart     string    `json:"proc_start"`
	Started       time.Time `json:"started"`
	Updated       time.Time `json:"updated"`
	Version       string    `json:"version"`
	ConfigHash    string    `json:"config_hash"`
	Raynet        string    `json:"raynet"`
	CIDR          string    `json:"cidr"`
	Apps          []string  `json:"apps"`
	MDNS          bool      `json:"mdns_active"`
	HTTP          bool      `json:"http_active"`
	Relay         bool      `json:"relay_active"`
	Engine        string    `json:"engine"`
	Reason        string    `json:"reason"`
	Error         string    `json:"error,omitempty"`
	Axiom         Seen      `json:"axiom"`
	App           Seen      `json:"app"`
	LastHealth    time.Time `json:"last_health"`
	QueryCount    uint64    `json:"queries"`
	ResponseCount uint64    `json:"responses"`
}
type stateStore struct {
	mu   sync.Mutex
	path string
	v    Runtime
}

func (s *stateStore) update(f func(*Runtime)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.v)
	s.v.Updated = time.Now().UTC()
}
func (s *stateStore) snapshot() Runtime { s.mu.Lock(); defer s.mu.Unlock(); return s.v }
func (s *stateStore) flush() {
	v := s.snapshot()
	d, e := json.Marshal(v)
	if e == nil {
		_ = atomicWrite(s.path, d, 0640)
	}
}
func processStart(pid int) string {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return ""
	}
	at := strings.LastIndexByte(string(b), ')')
	if at < 0 {
		return ""
	}
	f := strings.Fields(string(b[at+1:]))
	if len(f) < 20 {
		return ""
	}
	return f[19]
}
func processMatches(v Runtime, exe string) bool {
	if v.PID < 2 || v.ProcStart == "" || v.ProcStart != processStart(v.PID) {
		return false
	}
	p, e := os.Readlink(fmt.Sprintf("/proc/%d/exe", v.PID))
	if e != nil {
		return false
	}
	return strings.TrimSuffix(p, " (deleted)") == exe || filepath.Base(strings.TrimSuffix(p, " (deleted)")) == serviceName
}
func readRuntime(p Paths) Runtime {
	var v Runtime
	d, e := os.ReadFile(filepath.Join(p.StateDir, "runtime.json"))
	if e == nil && len(d) < 65536 {
		_ = json.Unmarshal(d, &v)
	}
	return v
}
func lockFile(path string) (*os.File, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another operation is still running")
	}
	return f, nil
}

type endpoint struct {
	name                  string
	iface                 *net.Interface
	ip                    net.IP
	conn                  *net.UDPConn
	publish               bool
	announcement, goodbye []byte
	lastReply             time.Time
}

func interfaceHasIP(i *net.Interface, ip net.IP) bool {
	a, e := i.Addrs()
	if e != nil {
		return false
	}
	for _, s := range a {
		v, _, e := net.ParseCIDR(s.String())
		if e == nil && v.Equal(ip) {
			return true
		}
	}
	return false
}
func firstInterfaceIPv4(i *net.Interface) (net.IP, error) {
	a, e := i.Addrs()
	if e != nil {
		return nil, e
	}
	for _, s := range a {
		v, _, e := net.ParseCIDR(s.String())
		if e == nil && v.To4() != nil && !v.IsLoopback() {
			return v.To4(), nil
		}
	}
	return nil, errors.New("interface has no IPv4 address")
}
func openMDNSSocket(name string, ip net.IP) (*net.UDPConn, error) {
	if ip.To4() == nil {
		return nil, errors.New("invalid socket address")
	}
	ip = ip.To4()
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var se error
		e := raw.Control(func(fd uintptr) {
			if se = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); se != nil {
				return
			}
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 15, 1)
			se = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, name)
		})
		if e != nil {
			return e
		}
		return se
	}}
	pc, e := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
	if e != nil {
		return nil, e
	}
	c, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return nil, errors.New("not UDP")
	}
	raw, e := c.SyscallConn()
	if e != nil {
		c.Close()
		return nil, e
	}
	var se error
	e = raw.Control(func(fd uintptr) {
		m := syscall.IPMreq{Multiaddr: [4]byte{224, 0, 0, 251}, Interface: [4]byte{ip[0], ip[1], ip[2], ip[3]}}
		if se = syscall.SetsockoptIPMreq(int(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, &m); se != nil {
			return
		}
		if se = syscall.SetsockoptInet4Addr(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_IF, m.Interface); se != nil {
			return
		}
		if se = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255); se != nil {
			return
		}
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 0)
	})
	if e != nil || se != nil {
		c.Close()
		if e != nil {
			return nil, e
		}
		return nil, se
	}
	_ = c.SetReadBuffer(128 * 1024)
	return c, nil
}
func (e *endpoint) send(p []byte, to *net.UDPAddr) error {
	if len(p) == 0 {
		return nil
	}
	if to == nil {
		to = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	}
	_ = e.conn.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := e.conn.WriteToUDP(p, to)
	return err
}

type observation struct {
	Service, Instance, Host, IP string
	Port                        uint16
	Seen, Expires               time.Time
}
type hostAddress struct {
	IP      string
	Expires time.Time
}
type engine struct {
	c        configuration
	state    *stateStore
	eps      []*endpoint
	axiom    *endpoint
	cache    *nameCache
	relay    bool
	wg       sync.WaitGroup
	mu       sync.Mutex
	obs      map[string]*observation
	hosts    map[string]hostAddress
	localIPs map[string]bool
	ctx      context.Context
	cancel   context.CancelFunc
}

func buildEngine(ctx context.Context, c configuration, s *stateStore) (*engine, error) {
	ec, cancel := context.WithCancel(ctx)
	g := &engine{c: c, state: s, cache: newNameCache(), obs: map[string]*observation{}, hosts: map[string]hostAddress{}, localIPs: map[string]bool{}, ctx: ec, cancel: cancel}
	distinct := false
	for _, n := range c.RemoteInterfaces {
		if n != c.AxiomInterface {
			distinct = true
		}
	}
	var reason string
	g.relay, reason = determineRelayMode(c, distinct)
	names := append([]string{c.AxiomInterface}, c.RemoteInterfaces...)
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		i, e := net.InterfaceByName(n)
		if e != nil {
			g.close()
			return nil, e
		}
		ip := c.AxiomIP
		if n != c.AxiomInterface {
			ip, e = firstInterfaceIPv4(i)
			if e != nil {
				g.close()
				return nil, e
			}
		}
		if !interfaceHasIP(i, ip) {
			g.close()
			return nil, fmt.Errorf("%s is absent on %s", ip, n)
		}
		conn, e := openMDNSSocket(n, ip)
		if e != nil {
			g.close()
			return nil, fmt.Errorf("mDNS socket %s: %w", n, e)
		}
		ep := &endpoint{name: n, iface: i, ip: ip, conn: conn}
		g.eps = append(g.eps, ep)
		g.localIPs[ip.String()] = true
		if n == c.AxiomInterface {
			g.axiom = ep
		}
		// External reflection receives only one origin advertisement, avoiding
		// contradictory A records from two direct publishers of the same hostname.
		ep.publish = c.MDNSEnabled && (n == c.AxiomInterface || !c.ExistingReflector)
		if ep.publish {
			ep.announcement, e = buildAnnouncement(c, i, ip, c.TTL)
			if e == nil {
				ep.goodbye, e = buildAnnouncement(c, i, ip, 0)
			}
			if e != nil {
				g.close()
				return nil, e
			}
		}
	}
	s.update(func(v *Runtime) { v.MDNS = c.MDNSEnabled; v.Relay = g.relay; v.Reason = reason })
	return g, nil
}
func ttlTime(ttl uint32) time.Duration {
	if ttl > 900 {
		ttl = 900
	}
	return time.Duration(ttl) * time.Second
}
func (g *engine) observe(m *dnsMessage) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now().UTC()
	touched := map[string]bool{}
	current := g.state.snapshot().Axiom
	for _, rec := range m.Records {
		if rec.TTL == 0 && (rec.Target == current.Name+"."+current.Service || rec.Name == current.Name+"."+current.Service || rec.Address == current.IP) {
			g.state.update(func(v *Runtime) { v.Axiom.Expires = now })
		}
	}
	for k, o := range g.obs {
		if now.After(o.Expires) {
			delete(g.obs, k)
		}
	}
	for k, o := range g.hosts {
		if now.After(o.Expires) {
			delete(g.hosts, k)
		}
	}
	for _, r := range m.Records {
		if r.Class&0x7fff != 1 {
			continue
		}
		if r.Type == 12 && mfdServiceTypes[r.Name] {
			if r.TTL == 0 {
				delete(g.obs, r.Target)
				continue
			}
			o := g.obs[r.Target]
			if o == nil {
				o = &observation{Service: r.Name, Instance: r.Target}
				g.obs[r.Target] = o
			}
			o.Seen = now
			o.Expires = now.Add(ttlTime(r.TTL))
			touched[r.Target] = true
		}
		if r.Type == 33 && mfdServiceTypes[serviceForName(r.Name)] {
			if r.TTL == 0 {
				delete(g.obs, r.Name)
				continue
			}
			o := g.obs[r.Name]
			if o == nil {
				o = &observation{Service: serviceForName(r.Name), Instance: r.Name}
				g.obs[r.Name] = o
			}
			o.Host = r.Target
			o.Port = r.Port
			o.Seen = now
			o.Expires = now.Add(ttlTime(r.TTL))
			touched[r.Name] = true
		}
	}
	for _, r := range m.Records {
		if r.Type != 1 || r.Address == "" || !g.cache.has(r.Name) {
			continue
		}
		if r.TTL == 0 {
			delete(g.hosts, r.Name)
			continue
		}
		g.hosts[r.Name] = hostAddress{r.Address, now.Add(ttlTime(r.TTL))}
		for k, o := range g.obs {
			if o.Host == r.Name {
				touched[k] = true
			}
		}
	}
	// Bound all learned state; multicast traffic must not grow memory indefinitely.
	if len(g.obs) > 256 {
		for k := range g.obs {
			delete(g.obs, k)
			if len(g.obs) <= 256 {
				break
			}
		}
	}
	if len(g.hosts) > 512 {
		for k := range g.hosts {
			delete(g.hosts, k)
			if len(g.hosts) <= 512 {
				break
			}
		}
	}
	for k := range touched {
		o := g.obs[k]
		if o == nil {
			continue
		}
		host, ok := g.hosts[o.Host]
		if !ok || now.After(host.Expires) {
			continue
		}
		expiry := o.Expires
		if host.Expires.Before(expiry) {
			expiry = host.Expires
		}
		prev := g.state.snapshot().Axiom
		seen := Seen{Name: strings.TrimSuffix(o.Instance, "."+o.Service), IP: host.IP, Service: o.Service, Port: o.Port, Interface: g.c.AxiomInterface, LastSeen: now, Expires: expiry}
		g.state.update(func(v *Runtime) { v.Axiom = seen })
		if prev.IP != seen.IP || prev.Name != seen.Name {
			log.Printf("INFO Axiom/MFD detected name=%q address=%s service=%s port=%d", seen.Name, seen.IP, seen.Service, seen.Port)
		}
	}
}
func (g *engine) recordApp(ep *endpoint, src net.IP, m *dnsMessage) {
	// Generic HTTP/Bonjour browsing does not prove the Raymarine app is running.
	for _, q := range m.Questions {
		if mfdServiceTypes[q.Name] {
			now := time.Now().UTC()
			g.state.update(func(v *Runtime) {
				v.App = Seen{IP: src.String(), Interface: ep.name, Service: q.Name, LastSeen: now, Expires: now.Add(2 * time.Minute)}
			})
			return
		}
	}
}
func unicastReply(announcement []byte, q *dnsMessage, legacy bool) ([]byte, error) {
	m, e := parseDNSMessage(announcement)
	if e != nil {
		return nil, e
	}
	p := make([]byte, 12)
	binary.BigEndian.PutUint16(p[2:4], 0x8400)
	if legacy {
		binary.BigEndian.PutUint16(p[0:2], q.ID)
		binary.BigEndian.PutUint16(p[4:6], uint16(len(q.Questions)))
		for _, q := range q.Questions {
			p = append(p, q.Wire...)
			p = appendUint16(p, q.Type)
			p = appendUint16(p, q.Class&0x7fff)
		}
	}
	binary.BigEndian.PutUint16(p[6:8], uint16(len(m.Records)))
	for _, r := range m.Records {
		d := r.Data
		if r.Type == 12 {
			d = r.TargetWire
		}
		if r.Type == 33 {
			d = append(append([]byte{}, d[:6]...), r.TargetWire...)
		}
		ttl := r.TTL
		class := r.Class
		if legacy {
			class &= 0x7fff
			if ttl > 10 {
				ttl = 10
			}
		}
		p, e = appendRR(p, r.Wire, r.Type, class, ttl, d)
		if e != nil {
			return nil, e
		}
	}
	return p, nil
}
func (g *engine) fail(err error) {
	g.state.update(func(v *Runtime) { v.MDNS = false; v.Relay = false; v.Error = err.Error() })
	g.cancel()
}
func (g *engine) debug(format string, args ...any) {
	if g.c.LogLevel == "debug" {
		log.Printf("DEBUG "+format, args...)
	}
}
func (g *engine) readLoop(ep *endpoint) {
	defer g.wg.Done()
	buf := make([]byte, 9000)
	window := time.Now()
	count := 0
	seenPackets := make(map[[32]byte]time.Time)
	for {
		_ = ep.conn.SetReadDeadline(time.Now().Add(time.Second))
		n, src, e := ep.conn.ReadFromUDP(buf)
		if e != nil {
			if g.ctx.Err() != nil {
				return
			}
			if errors.Is(e, net.ErrClosed) {
				g.fail(fmt.Errorf("mDNS socket closed unexpectedly on %s", ep.name))
				return
			}
			if ne, ok := e.(net.Error); ok && ne.Timeout() {
				continue
			}
			g.fail(fmt.Errorf("mDNS receive failed on %s: %w", ep.name, e))
			return
		}
		if g.localIPs[src.IP.String()] {
			continue
		}
		if time.Since(window) > time.Second {
			window = time.Now()
			count = 0
		}
		count++
		if count > 100 {
			continue
		}
		m, e := parseDNSMessage(buf[:n])
		if e != nil || (m.Response && src.Port != 5353) {
			continue
		}
		digest := sha256.Sum256(buf[:n])
		now := time.Now()
		if prior, ok := seenPackets[digest]; ok && now.Sub(prior) < 900*time.Millisecond {
			continue
		}
		if len(seenPackets) >= 256 {
			for k, t := range seenPackets {
				if now.Sub(t) >= time.Second {
					delete(seenPackets, k)
				}
			}
			if len(seenPackets) >= 256 {
				for k := range seenPackets {
					delete(seenPackets, k)
					break
				}
			}
		}
		seenPackets[digest] = now
		if !m.Response && ep.publish {
			own := false
			uni := src.Port != 5353
			for _, q := range m.Questions {
				if q.Class&0x7fff == 1 && (q.Name == yachtSenseServiceType || q.Name == browseType || q.Name == normalizeDNSName(g.c.HostLabel+".local") || q.Name == normalizeDNSName(g.c.Instance+"."+yachtSenseServiceType)) {
					own = true
					if q.Class&0x8000 != 0 {
						uni = true
					}
				}
			}
			if own && time.Since(ep.lastReply) > 100*time.Millisecond {
				ep.lastReply = time.Now()
				if uni {
					reply, err := unicastReply(ep.announcement, m, src.Port != 5353)
					if err == nil {
						_ = ep.send(reply, src)
					}
				} else {
					_ = ep.send(ep.announcement, nil)
				}
			}
		}
		if ep == g.axiom && m.Response {
			if responseRelevant(m, g.cache) {
				g.observe(m)
				if g.relay {
					p, e := filterDNS(m, g.cache, normalizeDNSName(g.c.HostLabel+".local"), normalizeDNSName(g.c.Instance+"."+yachtSenseServiceType))
					if e == nil && len(p) > 0 {
						for _, r := range g.eps {
							if r != ep {
								if err := r.send(p, nil); err != nil {
									g.fail(fmt.Errorf("relay send on %s: %w", r.name, err))
									return
								}
								g.debug("response %s -> %s (%d bytes)", ep.name, r.name, len(p))
							}
						}
						g.state.update(func(v *Runtime) { v.ResponseCount++ })
					}
				}
			}
		} else if !m.Response {
			if ep != g.axiom || len(g.eps) == 1 {
				g.recordApp(ep, src.IP, m)
			}
			if ep != g.axiom && g.relay {
				p, e := filterDNS(m, g.cache, "", "")
				if e == nil && len(p) > 0 {
					if err := g.axiom.send(p, nil); err != nil {
						g.fail(fmt.Errorf("query relay send: %w", err))
						return
					}
					g.debug("query %s -> %s (%d bytes)", ep.name, g.axiom.name, len(p))
					g.state.update(func(v *Runtime) { v.QueryCount++ })
				}
			}
		}
	}
}
func (g *engine) start() {
	for _, ep := range g.eps {
		g.wg.Add(1)
		go g.readLoop(ep)
	}
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		interval := time.Duration(g.c.TTL) * time.Second / 2
		if interval < time.Second {
			interval = time.Second
		}
		if interval > time.Minute {
			interval = time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		initial := time.NewTimer(time.Second)
		defer initial.Stop()
		browse := time.NewTicker(30 * time.Second)
		defer browse.Stop()
		announce := func() {
			for _, ep := range g.eps {
				if ep.publish {
					if e := ep.send(ep.announcement, nil); e != nil {
						g.fail(fmt.Errorf("mDNS announcement send on %s: %w", ep.name, e))
						return
					}
				}
			}
		}
		query := func() {
			p := make([]byte, 12)
			binary.BigEndian.PutUint16(p[4:6], 3)
			for _, s := range []string{rtspServiceType, rymServiceType, rayDBServiceType} {
				w, _ := encodeDNSName(s)
				p = append(p, w...)
				p = appendUint16(p, 12)
				p = appendUint16(p, 1)
			}
			_ = g.axiom.send(p, nil)
		}
		announce()
		query()
		for {
			select {
			case <-g.ctx.Done():
				return
			case <-initial.C:
				announce()
			case <-ticker.C:
				announce()
			case <-browse.C:
				query()
			}
		}
	}()
}
func (g *engine) close() {
	if g == nil {
		return
	}
	g.cancel()
	for _, e := range g.eps {
		if e.publish {
			_ = e.send(e.goodbye, nil)
		}
		_ = e.conn.Close()
	}
	g.wg.Wait()
}
func healthServer(c configuration, s *stateStore) (*http.Server, error) {
	listener, e := net.Listen("tcp4", net.JoinHostPort(c.AxiomIP.String(), strconv.Itoa(c.HealthPort)))
	if e != nil {
		return nil, e
	}
	h := &http.Server{ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		s.update(func(v *Runtime) { v.LastHealth = time.Now().UTC() })
		w.WriteHeader(200)
		_, _ = w.Write([]byte("OK\n"))
	})}
	h.Addr = listener.Addr().String()
	s.update(func(v *Runtime) { v.HTTP = true })
	go func() {
		if e := h.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
			s.update(func(v *Runtime) { v.HTTP = false; v.Error = "HTTP listener failed: " + e.Error() })
		}
	}()
	return h, nil
}

type managedAddress struct{ Interface, CIDR string }

func prepareAddress(p Paths, c Settings, r Resolved) (func(), error) {
	path := filepath.Join(p.StateDir, "managed-ip.json")
	var owned managedAddress
	data, e := os.ReadFile(path)
	if e == nil {
		_ = json.Unmarshal(data, &owned)
	}
	if !c.ManageIP {
		// Relinquishing ownership must never remove a user-managed address.
		_ = os.Remove(path)
		return func() {}, nil
	}
	if owned.Interface != "" && (owned.Interface != r.Raynet || owned.CIDR != r.CIDR) {
		if _, e = runCommand(3*time.Second, nil, p.IP, "addr", "del", owned.CIDR, "dev", owned.Interface); e != nil {
			return nil, fmt.Errorf("cannot remove previous package-owned address: %w", e)
		}
		_ = os.Remove(path)
		owned = managedAddress{}
	}
	i, e := net.InterfaceByName(r.Raynet)
	if e != nil {
		return nil, e
	}
	if !interfaceHasIP(i, net.ParseIP(c.IP)) {
		if _, e = runCommand(3*time.Second, nil, p.IP, "addr", "add", r.CIDR, "dev", r.Raynet); e != nil {
			return nil, fmt.Errorf("cannot add explicitly managed address: %w", e)
		}
		owned = managedAddress{r.Raynet, r.CIDR}
		b, _ := json.Marshal(owned)
		if e = atomicWrite(path, b, 0600); e != nil {
			_, _ = runCommand(3*time.Second, nil, p.IP, "addr", "del", r.CIDR, "dev", r.Raynet)
			return nil, e
		}
	}
	return func() {
		if c.RemoveIP && owned.Interface == r.Raynet && owned.CIDR == r.CIDR {
			// A successful switch to observe-only relinquishes this marker before
			// stopping the old daemon. Never act on captured ownership alone.
			var current managedAddress
			data, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(data, &current) != nil || current != owned {
				return
			}
			_, e := runCommand(3*time.Second, nil, p.IP, "addr", "del", owned.CIDR, "dev", owned.Interface)
			if e == nil {
				_ = os.Remove(path)
			}
		}
	}, nil
}
func runDaemon(ctx context.Context, p Paths) error {
	if e := os.MkdirAll(p.StateDir, 0750); e != nil {
		return e
	}
	lock, e := lockFile(filepath.Join(p.StateDir, "daemon.lock"))
	if e != nil {
		return e
	}
	defer lock.Close()
	c, e := readSettings(p)
	if e != nil {
		return e
	}
	if !c.Enabled {
		return nil
	}
	ifs, e := interfaceSnapshot()
	if e != nil {
		return e
	}
	r, e := resolve(c, ifs)
	if e != nil {
		return e
	}
	a := inspectAvahi(p, r)
	mode, relay, reason, e := chooseEngine(c, r, a)
	if e != nil {
		return e
	}
	cleanup, e := prepareAddress(p, c, r)
	if e != nil {
		return e
	}
	defer cleanup()
	state := &stateStore{path: filepath.Join(p.StateDir, "runtime.json"), v: Runtime{PID: os.Getpid(), ProcStart: processStart(os.Getpid()), Started: time.Now().UTC(), Version: packageVersion, ConfigHash: fingerprint(c), Raynet: r.Raynet, CIDR: r.CIDR, Apps: r.Apps, Engine: mode, Reason: reason}}
	state.flush()
	defer func() {
		state.update(func(v *Runtime) { v.MDNS = false; v.HTTP = false; v.Relay = false; v.Reason = "Service stopped" })
		state.flush()
	}()
	cfg := configuration{AxiomInterface: r.Raynet, AxiomIP: net.ParseIP(c.IP), RemoteInterfaces: r.Apps, Serial: c.Serial, Version: c.Version, HostLabel: c.Hostname, Instance: c.Instance, HealthPort: c.HealthPort, MDNSEnabled: c.MDNS, WebEnabled: c.HTTP, TTL: uint32(c.TTL), RelayMode: "disabled", ExistingReflector: mode == "avahi", LogLevel: c.LogLevel}
	if relay {
		cfg.RelayMode = "force"
	}
	if mode == "avahi" {
		cfg.RelayMode = "auto"
	}
	dc, cancel := context.WithCancel(ctx)
	defer cancel()
	g, e := buildEngine(dc, cfg, state)
	if e != nil {
		return e
	}
	defer g.close()
	g.start()
	state.update(func(v *Runtime) { v.Engine = mode; v.Reason = reason })
	var h *http.Server
	if c.HTTP {
		h, e = healthServer(cfg, state)
		if e != nil {
			cancel()
			return e
		}
		defer func() {
			sctx, sc := context.WithTimeout(context.Background(), 2*time.Second)
			defer sc()
			_ = h.Shutdown(sctx)
		}()
	}
	state.flush()
	log.Printf("INFO ready package=%s raynet=%s apps=%s discovery=%s", packageVersion, r.Raynet, strings.Join(r.Apps, ","), mode)
	flush := time.NewTicker(time.Second)
	defer flush.Stop()
	check := time.NewTicker(10 * time.Second)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			return nil
		case <-g.ctx.Done():
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("discovery engine stopped: %s", state.snapshot().Error)
		case <-flush.C:
			// A quiet network is still a live daemon. Heartbeats are independent
			// of packet timestamps, which are used only for peer freshness.
			state.update(func(v *Runtime) {})
			state.flush()
		case <-check.C:
			// Re-resolve after link/address or external-reflector changes, including boot
			// delays. procd restarts the process; no interface is changed in observe mode.
			current, e := interfaceSnapshot()
			if e != nil {
				continue
			}
			rr, e := resolve(c, current)
			if e != nil {
				cancel()
				return e
			}
			aa := inspectAvahi(p, rr)
			mm, _, _, e := chooseEngine(c, rr, aa)
			if e != nil {
				cancel()
				return e
			}
			if rr.Raynet != r.Raynet || rr.CIDR != r.CIDR || strings.Join(rr.Apps, ",") != strings.Join(r.Apps, ",") || mm != mode {
				cancel()
				return errors.New("network/discovery selection changed; restarting")
			}
		}
	}
}
