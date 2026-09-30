package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func rawJSON(v string) rawSource { return rawSource{data: []byte(v)} }
func TestDashboardIsReadOnly(t *testing.T) {
	c := defaultSettings()
	s := &stateStore{v: Runtime{PID: 2, Updated: time.Now()}}
	cache := &dashboardCache{}
	cache.replace(RouterView{Profile: "onWiFi", Observed: time.Now()})
	h := dashboardHandler(c, Resolved{CIDR: "198.18.0.1/21"}, s, cache)
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://router/api/status", strings.NewReader(`{"enabled":false}`))
		h.ServeHTTP(w, r)
		if w.Code != 405 {
			t.Fatal(method, w.Code)
		}
	}
	for _, path := range []string{"/actions/start", "/etc/config/network", "/api/config", "/api/status/../config"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://router"+path, nil))
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://router/api/status", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "dashboard_allowed_cidrs") || strings.Contains(w.Body.String(), "config_dir") {
		t.Fatal(w.Body.String())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("public CORS not allowed")
	}
	for _, path := range []string{"/", "/status.js", "/status.css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("HEAD", "http://router"+path, nil))
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal("HEAD", path, w.Code)
		}
	}
}
func TestDashboardSourceAccess(t *testing.T) {
	p := []netip.Prefix{netip.MustParsePrefix("198.18.0.0/21"), netip.MustParsePrefix("192.168.4.0/24")}
	if !dashboardAllowed("192.168.4.117:5123", false, p) || dashboardAllowed("203.0.113.12:3333", false, p) || !dashboardAllowed("203.0.113.12:3333", true, p) {
		t.Fatal("source filter")
	}
	c := defaultSettings()
	c.DashboardAllowAll = false
	c.DashboardCIDRs = ""
	h := dashboardHandler(c, Resolved{CIDR: "198.18.0.1/21"}, &stateStore{}, &dashboardCache{})
	r := httptest.NewRequest("GET", "http://router/api/status", nil)
	r.RemoteAddr = "203.0.113.12:1234"
	r.Header.Set("X-Forwarded-For", "198.18.3.234")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("forwarded header bypass")
	}
}
func TestMwanActualPolicyNotFirstPolicy(t *testing.T) {
	raw := map[string]rawSource{"failover": {data: []byte("Interface status:\n interface wan1 is online\n interface mob1s1a1 is online\nCurrent ipv4 policies:\nbalance_default:\n wan1 (50%)\n mob1s1a1 (50%)\nmwan_default:\n wan1 (100%)\nCurrent ipv6 policies:\n")}, "interfaces": rawJSON(`{"interface":[{"interface":"wan1","l3_device":"wlan0-4","up":true,"ipv4-address":[{"address":"10.0.0.2"}]},{"interface":"mob1s1a1","l3_device":"qmimux0","up":true}]}`), "signal": {data: []byte("RSSI: -77\nRSRP: -91")}, "sim": {data: []byte("inserted")}}
	uci := []byte("config interface 'wan1'\n option enabled '1'\nconfig rule 'default_rule'\n option dest_ip '0.0.0.0/0'\n option use_policy 'mwan_default'\n")
	v := makeInternet(raw, map[string]map[string]any{"wlan0-4": {"ssid": "Harbour", "signal": float64(-61)}}, uci, nil)
	if len(v.Links) != 2 || v.Links[0].Name != "wan1" || v.Links[0].State != "active" || v.Links[0].RSSI == nil || *v.Links[0].RSSI != -61 || v.Links[1].State != "standby" {
		t.Fatalf("%+v", v)
	}
	raw["sim"] = rawSource{data: []byte("not inserted")}
	v = makeInternet(raw, nil, uci, nil)
	if len(v.Links) != 1 {
		t.Fatal("absent SIM was not hidden")
	}
	v = makeInternet(raw, nil, uci, []byte("config interface 'wan1'\n option disabled '1'\n"))
	if len(v.Links) != 0 {
		t.Fatal("disabled interface not hidden")
	}
}
func TestGPSFixGeofenceAndStale(t *testing.T) {
	now := time.Now()
	fix := map[string]any{"latitude": 52.2, "longitude": 5.08, "fix_status": "3D", "utc_timestamp": now.Unix()}
	b, _ := json.Marshal(fix)
	p := decodeSource(rawSource{data: b})
	fences := []any{map[string]any{"name": "Harbour", "enabled": "1", "latitude": 52.2, "longitude": 5.08, "radius": float64(25)}}
	v := gpsView(p, nil, fences, now)
	if v.State != "fix" || v.Fences[0].State != "inside" {
		t.Fatalf("%+v", v)
	}
	v = gpsView(p, nil, fences, now.Add(time.Minute))
	if v.State != "stale" || v.Latitude != nil || v.Fences[0].State != "unknown" {
		t.Fatal("stale fix remained active")
	}
}
func TestTypedTelemetryAndCounters(t *testing.T) {
	if decodeSource(rawJSON(`{"http_code":403,"http_body":{"success":false}}`)) != nil || decodeSource(rawSource{err: errors.New("failed")}) != nil {
		t.Fatal("invalid source accepted")
	}
	if rmsState(map[string]any{"connection_status": float64(0)}) != "connected" || rmsState(map[string]any{"connection_status": float64(1)}) != "disconnected" {
		t.Fatal("RMS state")
	}
	peers := swanVPN([]byte("Kerklaan: #1, ESTABLISHED, IKEv2\n established 45s ago\n Kerklaan_c: #1, reqid 1, INSTALLED, TUNNEL\n in cec1, 1200 bytes, 10 packets\n out f2f8, 3400 bytes, 20 packets\n"))
	if len(peers) != 1 || peers[0].State != "connected" || *peers[0].RX != 1200 || *peers[0].TX != 3400 || *peers[0].Uptime != 45 {
		t.Fatal(peers)
	}
	if !usablePublicIPv4("8.8.8.8") || usablePublicIPv4("100.64.0.2") || usablePublicIPv4("198.18.0.1") || usablePublicIPv4("unavailable") {
		t.Fatal("public IP validation")
	}
}
func TestAllKnownDeviceTypesAndNoStaleOnline(t *testing.T) {
	now := time.Now()
	neighbors := decodeSource(rawJSON(`[{"dst":"192.168.40.2","lladdr":"00:11:22:33:44:55","dev":"br-lan","state":["STALE"]},{"dst":"192.168.40.3","lladdr":"00:11:22:33:44:66","dev":"br-lan","state":["REACHABLE"]}]`))
	associated := map[string]DeviceView{"00:11:22:33:44:66": {MAC: "00:11:22:33:44:66", Connection: "Wi-Fi", State: "associated", SSID: "Boat"}, "00:11:22:33:44:77": {MAC: "00:11:22:33:44:77", Connection: "Wi-Fi", State: "associated"}}
	devices := devicesView(neighbors, associated, nil, map[string]bool{"br-lan": true}, "eth0.3", Seen{}, now)
	devices = mergeKnownDevices(devices, []byte("config host\n option name 'Static device'\n option mac '00:11:22:33:44:88'\n option ip '192.168.40.4'\n"), nil, nil, nil)
	if len(devices) != 4 {
		t.Fatal(devices)
	}
	found := false
	for _, d := range devices {
		if d.IP == "192.168.40.2" && d.State != "recently_seen" {
			t.Fatal("STALE became online")
		}
		if d.Name == "Static device" && d.State == "known" {
			found = true
		}
	}
	if !found {
		t.Fatal("static LAN device missing")
	}
}
func TestPublicLogFiltering(t *testing.T) {
	data := []byte("daemon yachtsense-link-emulator: INFO ready package=1.2.0\nother-service password=secret\nyachtsense-link-emulator: random token=secret\nyachtsense-link-emulator: Axiom/MFD detected address=198.18.3.234\n")
	v := publicLogs(data)
	if len(v) != 2 || strings.Contains(strings.Join(v, ""), "secret") {
		t.Fatal(v)
	}
}
