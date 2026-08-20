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

// TestBuildAnnouncement checks the product markers used by Axiom discovery.
func TestBuildAnnouncement(t *testing.T) {
	cfg := configuration{
		IP:        net.IPv4(198, 18, 0, 1).To4(),
		Serial:    "AF002A4",
		Version:   "V142.242.530",
		HostLabel: "yachtsense",
		Instance:  "yachtsense-main Settings",
	}
	iface := &net.Interface{HardwareAddr: net.HardwareAddr{0x02, 0x11, 0x22, 0x33, 0x44, 0x55}}

	packet, err := buildAnnouncement(cfg, iface, defaultTTL)
	if err != nil {
		t.Fatalf("buildAnnouncement returned an error: %v", err)
	}
	if len(packet) < 12 {
		t.Fatalf("packet is too short: %d", len(packet))
	}
	if answers := binary.BigEndian.Uint16(packet[6:8]); answers != 5 {
		t.Fatalf("expected five answers, got %d", answers)
	}

	for _, marker := range []string{
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

// TestContainsInterestingQuestion verifies that matching mDNS queries trigger a response.
func TestContainsInterestingQuestion(t *testing.T) {
	name, err := encodeDNSName(serviceType)
	if err != nil {
		t.Fatal(err)
	}

	query := make([]byte, 12)
	binary.BigEndian.PutUint16(query[4:6], 1)
	query = append(query, name...)
	query = appendUint16(query, 12)     // PTR query.
	query = appendUint16(query, 0x0001) // Internet class.

	interesting := map[string]struct{}{serviceType: {}}
	if !containsInterestingQuestion(query, interesting) {
		t.Fatal("expected the service query to be recognized")
	}
}

// TestCleanSerial documents the serial filtering applied before advertising.
func TestCleanSerial(t *testing.T) {
	if got := cleanSerial(" RUTX 14/@boat "); got != "RUTX14boat" {
		t.Fatalf("unexpected cleaned serial %q", got)
	}
	if got := cleanSerial(""); got != "AF002A4" {
		t.Fatalf("unexpected default serial %q", got)
	}
}
