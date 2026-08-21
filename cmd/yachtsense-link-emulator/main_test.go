package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
)

// TestEncodeDNSName verifies the uncompressed DNS wire representation.
func TestEncodeDNSName(t *testing.T) {
	encoded, err := encodeDNSName("_http._tcp.local")
	if err != nil {
		t.Fatalf("encodeDNSName returned an error: %v", err)
	}
	expected := []byte{
		5, '_', 'h', 't', 't', 'p',
		4, '_', 't', 'c', 'p',
		5, 'l', 'o', 'c', 'a', 'l',
		0,
	}
	if !bytes.Equal(encoded, expected) {
		t.Fatalf("unexpected encoding: %v", encoded)
	}
}

// TestBuildAnnouncement checks the exact discovery markers used by Axiom and
// the current Raymarine mobile app.
func TestBuildAnnouncement(t *testing.T) {
	cfg := configuration{
		Serial:    "AF002A4",
		Version:   "V142.242.530",
		HostLabel: "yachtsense-main",
		Instance:  "yachtsense-main Settings",
	}
	iface := &net.Interface{HardwareAddr: net.HardwareAddr{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}}
	advertisedIP := net.IPv4(198, 18, 0, 1).To4()

	packet, err := buildAnnouncement(cfg, iface, advertisedIP, defaultTTL)
	if err != nil {
		t.Fatalf("buildAnnouncement returned an error: %v", err)
	}
	if answers := binary.BigEndian.Uint16(packet[6:8]); answers != 5 {
		t.Fatalf("expected five answers, got %d", answers)
	}
	for _, marker := range []string{
		"yachtsense-main Settings",
		"yachtsense-main",
		"id=E70640 AF002A4",
		"model=Raymarine YachtSense Link",
		"version=V142.242.530",
		"mac=02:11:22:33:44:55",
	} {
		if !bytes.Contains(packet, []byte(marker)) {
			t.Errorf("packet does not contain %q", marker)
		}
	}
}

// buildQuery creates a minimal one-question mDNS query for filter tests.
func buildQuery(t *testing.T, name string, recordType uint16) []byte {
	t.Helper()
	encoded, err := encodeDNSName(name)
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[4:6], 1)
	packet = append(packet, encoded...)
	packet = appendUint16(packet, recordType)
	packet = appendUint16(packet, 0x0001)
	return packet
}

// TestRelevantQueryNames proves the relay accepts Raymarine DNS-SD browses but
// not unrelated Bonjour services.
func TestRelevantQueryNames(t *testing.T) {
	cache := newNameCache()
	message, err := parseDNSMessage(buildQuery(t, rtspServiceType, 12))
	if err != nil {
		t.Fatal(err)
	}
	names := relevantQueryNames(message, cache)
	if len(names) != 1 || names[0] != rtspServiceType {
		t.Fatalf("unexpected Raymarine query result: %#v", names)
	}

	unrelated, err := parseDNSMessage(buildQuery(t, "_ipp._tcp.local", 12))
	if err != nil {
		t.Fatal(err)
	}
	if names := relevantQueryNames(unrelated, cache); len(names) != 0 {
		t.Fatalf("unrelated Bonjour service should not be reflected: %#v", names)
	}
}

// TestResponseLearning verifies that a relevant PTR response teaches the relay
// both the service instance and the SRV hostname used by follow-up NSD queries.
func TestResponseLearning(t *testing.T) {
	instance := "Axiom 7." + rtspServiceType
	host := "axiom-123.local"
	encodedInstance, _ := encodeDNSName(instance)
	encodedHost, _ := encodeDNSName(host)

	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[2:4], 0x8400)
	binary.BigEndian.PutUint16(packet[6:8], 3)
	var err error
	packet, err = appendResourceRecord(packet, rtspServiceType, 12, 0x0001, 120, encodedInstance)
	if err != nil {
		t.Fatal(err)
	}
	srvData := make([]byte, 0, len(encodedHost)+6)
	srvData = appendUint16(srvData, 0)
	srvData = appendUint16(srvData, 0)
	srvData = appendUint16(srvData, 8554)
	srvData = append(srvData, encodedHost...)
	packet, err = appendResourceRecord(packet, instance, 33, 0x8001, 120, srvData)
	if err != nil {
		t.Fatal(err)
	}
	packet, err = appendResourceRecord(packet, host, 1, 0x8001, 120, net.IPv4(198, 18, 0, 23).To4())
	if err != nil {
		t.Fatal(err)
	}

	message, err := parseDNSMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	cache := newNameCache()
	if !responseRelevant(message, cache) {
		t.Fatal("expected RTSP response to be relevant")
	}
	if !cache.has(instance) || !cache.has(host) {
		t.Fatalf("expected instance and host to be learned: instance=%v host=%v", cache.has(instance), cache.has(host))
	}

	followUp, err := parseDNSMessage(buildQuery(t, host, 1))
	if err != nil {
		t.Fatal(err)
	}
	if names := relevantQueryNames(followUp, cache); len(names) != 1 || names[0] != host {
		t.Fatalf("learned host query was not accepted: %#v", names)
	}
}

// TestDetermineRelayMode documents the loop-prevention rules around an existing
// Avahi reflector and the no-op case when both roles use one interface.
func TestDetermineRelayMode(t *testing.T) {
	cfg := configuration{RelayMode: "auto"}
	active, _ := determineRelayMode(cfg, false)
	if active {
		t.Fatal("relay must stay off when both roles use one interface")
	}

	cfg.ExistingReflector = true
	active, _ = determineRelayMode(cfg, true)
	if active {
		t.Fatal("automatic relay must stay off when an existing reflector is detected")
	}

	cfg.RelayMode = "force"
	active, _ = determineRelayMode(cfg, true)
	if !active {
		t.Fatal("force mode must enable the built-in relay")
	}
}

// TestCleanSerial documents the serial filtering and public default serial.
func TestCleanSerial(t *testing.T) {
	if got := cleanSerial(" RUTX 14/@boat "); got != "RUTX14boat" {
		t.Fatalf("unexpected cleaned serial %q", got)
	}
	if got := cleanSerial(""); got != "AF002A4" {
		t.Fatalf("unexpected default serial %q", got)
	}
}

// TestHTTPResponseFiltering ensures a generic _http._tcp service does not make
// the Raymarine relay forward unrelated Bonjour web devices.
func TestHTTPResponseFiltering(t *testing.T) {
	genericInstance := "Printer Web UI." + yachtSenseServiceType
	encodedGeneric, _ := encodeDNSName(genericInstance)
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[2:4], 0x8400)
	binary.BigEndian.PutUint16(packet[6:8], 1)
	var err error
	packet, err = appendResourceRecord(packet, yachtSenseServiceType, 12, 0x0001, 120, encodedGeneric)
	if err != nil {
		t.Fatal(err)
	}
	message, err := parseDNSMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	if responseRelevant(message, newNameCache()) {
		t.Fatal("unrelated generic HTTP service should not be reflected")
	}

	yachtSenseInstance := "yachtsense-main Settings." + yachtSenseServiceType
	encodedYachtSense, _ := encodeDNSName(yachtSenseInstance)
	packet = make([]byte, 12)
	binary.BigEndian.PutUint16(packet[2:4], 0x8400)
	binary.BigEndian.PutUint16(packet[6:8], 1)
	packet, err = appendResourceRecord(packet, yachtSenseServiceType, 12, 0x0001, 120, encodedYachtSense)
	if err != nil {
		t.Fatal(err)
	}
	message, err = parseDNSMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !responseRelevant(message, newNameCache()) {
		t.Fatal("YachtSense HTTP service should be reflected")
	}
}

// TestServiceEnumerationFiltering ensures a DNS-SD enumeration response cannot
// teach the relay an unrelated service type such as _ipp._tcp.
func TestServiceEnumerationFiltering(t *testing.T) {
	encodedIPP, _ := encodeDNSName("_ipp._tcp.local")
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[2:4], 0x8400)
	binary.BigEndian.PutUint16(packet[6:8], 1)
	var err error
	packet, err = appendResourceRecord(packet, browseType, 12, 0x0001, 120, encodedIPP)
	if err != nil {
		t.Fatal(err)
	}
	message, err := parseDNSMessage(packet)
	if err != nil {
		t.Fatal(err)
	}
	cache := newNameCache()
	if responseRelevant(message, cache) {
		t.Fatal("enumeration containing only unrelated service types should not be reflected")
	}
	if cache.has("_ipp._tcp.local") {
		t.Fatal("unrelated enumeration target must not enter the learned-name cache")
	}
}
