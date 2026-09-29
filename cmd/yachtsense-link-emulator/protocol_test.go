package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig() configuration {
	return configuration{AxiomIP: net.IPv4(198, 18, 0, 1), Serial: "AF002A4", Version: "V142.242.530", HostLabel: "yachtsense-main", Instance: "yachtsense-main Settings", TTL: 120}
}
func buildQuery(t *testing.T, name string, typ uint16) []byte {
	t.Helper()
	w, e := encodeDNSName(name)
	if e != nil {
		t.Fatal(e)
	}
	p := make([]byte, 12)
	binary.BigEndian.PutUint16(p[4:6], 1)
	p = append(p, w...)
	p = appendUint16(p, typ)
	return appendUint16(p, 1)
}
func response(t *testing.T, ttl uint32, extra bool) []byte {
	t.Helper()
	p := make([]byte, 12)
	binary.BigEndian.PutUint16(p[2:4], 0x8400)
	n := uint16(3)
	if extra {
		n = 6
	}
	binary.BigEndian.PutUint16(p[6:8], n)
	iw, _ := encodeDNSName("Axiom 7." + rtspServiceType)
	hw, _ := encodeDNSName("axiom-123.local")
	p, _ = appendResourceRecord(p, rtspServiceType, 12, 1, ttl, iw)
	p, _ = appendResourceRecord(p, "Axiom 7."+rtspServiceType, 33, 0x8001, ttl, append([]byte{0, 0, 0, 0, 33, 106}, hw...))
	p, _ = appendResourceRecord(p, "axiom-123.local", 1, 0x8001, ttl, []byte{198, 18, 0, 23})
	if extra {
		iw, _ := encodeDNSName("Secret printer._ipp._tcp.local")
		hw, _ := encodeDNSName("printer.local")
		p, _ = appendResourceRecord(p, "_ipp._tcp.local", 12, 1, ttl, iw)
		p, _ = appendResourceRecord(p, "Secret printer._ipp._tcp.local", 33, 0x8001, ttl, append([]byte{0, 0, 0, 0, 2, 119}, hw...))
		p, _ = appendResourceRecord(p, "printer.local", 1, 0x8001, ttl, []byte{10, 0, 0, 44})
	}
	return p
}
func parse(t *testing.T, p []byte) *dnsMessage {
	t.Helper()
	m, e := parseDNSMessage(p)
	if e != nil {
		t.Fatal(e)
	}
	return m
}

// Original 1.0.13 protocol regression scenarios, plus stricter packet checks.
func TestEncodeDNSName(t *testing.T) {
	w, e := encodeDNSName("_http._tcp.local")
	want := []byte{5, '_', 'h', 't', 't', 'p', 4, '_', 't', 'c', 'p', 5, 'l', 'o', 'c', 'a', 'l', 0}
	if e != nil || !bytes.Equal(w, want) {
		t.Fatalf("%v %v", w, e)
	}
}
func TestBuildAnnouncement(t *testing.T) {
	c := testConfig()
	i := &net.Interface{HardwareAddr: net.HardwareAddr{2, 17, 34, 51, 68, 85}}
	p, e := buildAnnouncement(c, i, c.AxiomIP, defaultTTL)
	if e != nil {
		t.Fatal(e)
	}
	if binary.BigEndian.Uint16(p[6:8]) != 5 {
		t.Fatal("five answers expected")
	}
	for _, marker := range []string{"yachtsense-main Settings", "id=E70640 AF002A4", "model=Raymarine YachtSense Link", "version=V142.242.530", "mac=02:11:22:33:44:55"} {
		if !bytes.Contains(p, []byte(marker)) {
			t.Error(marker)
		}
	}
	m := parse(t, p)
	for _, r := range m.Records {
		if r.Type == 33 && r.Port != 80 {
			t.Fatal("SRV identity port changed")
		}
	}
}
func TestRelevantQueryNames(t *testing.T) {
	c := newNameCache()
	m := parse(t, buildQuery(t, rtspServiceType, 12))
	if n := relevantQueryNames(m, c); len(n) != 1 || n[0] != rtspServiceType {
		t.Fatal(n)
	}
	m = parse(t, buildQuery(t, "_ipp._tcp.local", 12))
	if len(relevantQueryNames(m, c)) != 0 {
		t.Fatal("printer query leaked")
	}
}
func TestResponseLearning(t *testing.T) {
	m := parse(t, response(t, 120, false))
	c := newNameCache()
	if !responseRelevant(m, c) || !c.has("Axiom 7."+rtspServiceType) || !c.has("axiom-123.local") {
		t.Fatal("learning failed")
	}
	if len(relevantQueryNames(parse(t, buildQuery(t, "axiom-123.local", 1)), c)) != 1 {
		t.Fatal("host follow-up rejected")
	}
}
func TestDetermineRelayMode(t *testing.T) {
	c := configuration{RelayMode: "auto"}
	if active, _ := determineRelayMode(c, false); active {
		t.Fatal("same-interface relay")
	}
	c.ExistingReflector = true
	if active, _ := determineRelayMode(c, true); active {
		t.Fatal("automatic double reflector")
	}
	c.RelayMode = "force"
	if active, _ := determineRelayMode(c, true); !active {
		t.Fatal("low-level explicit relay mode ignored")
	}
}
func TestCleanSerial(t *testing.T) {
	if cleanSerial(" RUTX 14/@boat ") != "RUTX14boat" || cleanSerial("") != "AF002A4" {
		t.Fatal("serial changed")
	}
}
func TestHTTPResponseFiltering(t *testing.T) {
	for _, x := range []struct {
		name string
		want bool
	}{{"Printer Web UI", false}, {"yachtsense-main Settings", true}} {
		t.Run(x.name, func(t *testing.T) {
			w, _ := encodeDNSName(x.name + "." + yachtSenseServiceType)
			p := make([]byte, 12)
			binary.BigEndian.PutUint16(p[2:4], 0x8400)
			binary.BigEndian.PutUint16(p[6:8], 1)
			p, _ = appendResourceRecord(p, yachtSenseServiceType, 12, 1, 120, w)
			if responseRelevant(parse(t, p), newNameCache()) != x.want {
				t.Fatal("wrong HTTP filter")
			}
		})
	}
}
func TestServiceEnumerationFiltering(t *testing.T) {
	w, _ := encodeDNSName("_ipp._tcp.local")
	p := make([]byte, 12)
	binary.BigEndian.PutUint16(p[2:4], 0x8400)
	binary.BigEndian.PutUint16(p[6:8], 1)
	p, _ = appendResourceRecord(p, browseType, 12, 1, 120, w)
	c := newNameCache()
	if responseRelevant(parse(t, p), c) || c.has("_ipp._tcp.local") {
		t.Fatal("unrelated enumeration learned")
	}
}

func TestMixedResponseDoesNotLeakRecords(t *testing.T) {
	m := parse(t, response(t, 120, true))
	out, e := filterDNS(m, newNameCache(), "", "")
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(out, []byte("printer")) || len(parse(t, out).Records) != 3 {
		t.Fatal("mixed packet leaked unrelated records")
	}
}
func TestMixedQueryAndQU(t *testing.T) {
	p := buildQuery(t, rtspServiceType, 12)
	p[len(p)-2] = 0x80
	binary.BigEndian.PutUint16(p[4:6], 2)
	w, _ := encodeDNSName("_ipp._tcp.local")
	p = append(p, w...)
	p = appendUint16(p, 12)
	p = appendUint16(p, 1)
	m := parse(t, p)
	out, e := filterDNS(m, newNameCache(), "", "")
	if e != nil {
		t.Fatal(e)
	}
	q := parse(t, out).Questions
	if len(q) != 1 || q[0].Class != 1 {
		t.Fatal("filter/QU conversion failed")
	}
}
func TestOwnAnnouncementNotRelayed(t *testing.T) {
	c := testConfig()
	p, _ := buildAnnouncement(c, nil, c.AxiomIP, 120)
	out, e := filterDNS(parse(t, p), newNameCache(), normalizeDNSName(c.HostLabel+".local"), normalizeDNSName(c.Instance+"."+yachtSenseServiceType))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(out, []byte("yachtsense-main")) {
		t.Fatal("own hostname relayed across direct publishers")
	}
}
func TestLegacyUnicastResponse(t *testing.T) {
	c := testConfig()
	a, _ := buildAnnouncement(c, nil, c.AxiomIP, 120)
	q := parse(t, buildQuery(t, yachtSenseServiceType, 12))
	q.ID = 1234
	p, e := unicastReply(a, q, true)
	if e != nil {
		t.Fatal(e)
	}
	m := parse(t, p)
	if m.ID != 1234 || len(m.Questions) != 1 {
		t.Fatal("legacy ID/question missing")
	}
	for _, r := range m.Records {
		if r.Class&0x8000 != 0 || r.TTL > 10 {
			t.Fatal("legacy cache flush/TTL")
		}
	}
}
func TestMalformedDNS(t *testing.T) {
	cases := [][]byte{{}, {0}, make([]byte, 12), append(append(make([]byte, 12), 0xc0), 12)}
	cases[2][4] = 0xff
	cases[3][5] = 1
	for i, b := range cases {
		if _, e := parseDNSMessage(b); e == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
	for _, at := range []int{-1, 0, 1000} {
		if _, _, e := decodeDNSName(nil, at, 0); e == nil {
			t.Fatal("invalid name offset")
		}
	}
}
func TestCompressedNameWithLiteralDot(t *testing.T) {
	prefix := make([]byte, 12)
	wire := append([]byte{7}, []byte("A.b MFD")...)
	tail, _ := encodeDNSName(rtspServiceType)
	wire = append(wire, tail...)
	name, next, decoded, e := decodeName(append(prefix, wire...), 12, 0)
	if e != nil || next != 12+len(wire) || !bytes.Equal(decoded, wire) || !strings.Contains(name, "a.b") {
		t.Fatal(name, next, e)
	}
	if _, e = encodeDNSName(strings.Repeat("a.", 128)); e == nil {
		t.Fatal("oversize DNS name accepted")
	}
}
func TestGoodbyeExpiresObservation(t *testing.T) {
	s := &stateStore{path: filepath.Join(t.TempDir(), "state"), v: Runtime{}}
	g := &engine{state: s, cache: newNameCache(), obs: map[string]*observation{}, hosts: map[string]hostAddress{}}
	m := parse(t, response(t, 120, false))
	responseRelevant(m, g.cache)
	g.observe(m)
	if s.snapshot().Axiom.IP != "198.18.0.23" {
		t.Fatal("MFD not tracked")
	}
	bye := parse(t, response(t, 0, false))
	responseRelevant(bye, g.cache)
	g.observe(bye)
	if time.Now().Before(s.snapshot().Axiom.Expires) {
		t.Fatal("goodbye left green detection")
	}
}
func TestNameCacheBoundAndRace(t *testing.T) {
	c := newNameCache()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				n := strings.Repeat("a", i%64) + time.Now().String()
				c.add(n)
				c.has(n)
			}
		}()
	}
	wg.Wait()
	if len(c.expires) > 1024 {
		t.Fatal(len(c.expires))
	}
	c.addTTL("x.local", 0)
	if c.has("x.local") {
		t.Fatal("zero TTL retained")
	}
}
func TestHTTPHealthListener(t *testing.T) {
	s := &stateStore{v: Runtime{}}
	c := testConfig()
	c.AxiomIP = net.IPv4(127, 0, 0, 1)
	c.HealthPort = 0
	h, e := healthServer(c, s)
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	client := http.Client{Timeout: 2 * time.Second}
	res, e := client.Get("http://" + h.Addr + "/connection-monitor")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(b) != "OK\n" || s.snapshot().LastHealth.IsZero() {
		t.Fatal("health response incorrect")
	}
}
func TestEngineCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := &engine{ctx: ctx, cancel: cancel}
	g.close()
}
func FuzzDNSParser(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 12))
	p, _ := buildAnnouncement(testConfig(), nil, net.IPv4(198, 18, 0, 1), 120)
	f.Add(p)
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > 9000 {
			t.Skip()
		}
		m, e := parseDNSMessage(p)
		if e == nil {
			out, e := filterDNS(m, newNameCache(), "", "")
			if e == nil && len(out) > 0 {
				if _, e = parseDNSMessage(out); e != nil {
					t.Fatalf("filter generated invalid DNS: %v", e)
				}
			}
		}
	})
}
