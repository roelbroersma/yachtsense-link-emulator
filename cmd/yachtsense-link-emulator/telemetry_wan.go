package main

// Read-only router telemetry. Missing observations are never invented.
import (
	"encoding/json"
	"math"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func defaultPolicy(data []byte) (string, bool) {
	configured := false
	for _, s := range uciSections(data) {
		if s.typ == "interface" && uv(s, "enabled") != "0" {
			configured = true
		}
		if s.typ != "rule" {
			continue
		}
		if uv(s, "enabled") == "0" || uv(s, "family") == "ipv6" {
			continue
		}
		dst, src, proto := uv(s, "dest_ip"), uv(s, "src_ip"), uv(s, "proto")
		if (dst == "" || dst == "0.0.0.0/0") && (src == "" || src == "0.0.0.0/0") && (proto == "" || proto == "all") && uv(s, "dest_port") == "" && uv(s, "src_port") == "" {
			if p := uv(s, "use_policy"); p != "" {
				return p, configured
			}
		}
	}
	return "", configured
}

var policyMemberRE = regexp.MustCompile(`^\s*([A-Za-z0-9_.:-]+)\s+\(([0-9.]+)%\)`)

func parseMwan(data []byte) (map[string]map[string]float64, map[string]string) {
	policies := map[string]map[string]float64{}
	states := map[string]string{}
	ipv4 := false
	name := ""
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		f := strings.Fields(t)
		if len(f) > 3 && f[0] == "interface" && f[2] == "is" {
			states[f[1]] = strings.Trim(f[3], ",:")
		}
		if strings.HasPrefix(strings.ToLower(t), "current ipv4 policies:") {
			ipv4 = true
			continue
		}
		if strings.HasPrefix(strings.ToLower(t), "current ipv6 policies:") {
			ipv4 = false
			name = ""
			continue
		}
		if !ipv4 {
			continue
		}
		if strings.HasSuffix(t, ":") && !strings.Contains(t, " ") {
			name = strings.TrimSuffix(t, ":")
			policies[name] = map[string]float64{}
			continue
		}
		if m := policyMemberRE.FindStringSubmatch(line); m != nil && name != "" {
			n, _ := strconv.ParseFloat(m[2], 64)
			if n > 0 {
				policies[name][m[1]] = n
			}
		}
	}
	return policies, states
}
func signalValues(data []byte) map[string]*float64 {
	out := map[string]*float64{}
	re := regexp.MustCompile(`(?mi)\b(RSSI|RSRP|SINR|RSRQ)\s*:\s*(-?[0-9.]+)`)
	for _, m := range re.FindAllStringSubmatch(string(data), -1) {
		n, _ := strconv.ParseFloat(m[2], 64)
		out[strings.ToLower(m[1])] = &n
	}
	if len(out) == 0 {
		if n, ok := number(strings.TrimSpace(string(data))); ok && n < 0 && n >= -150 {
			out["rssi"] = &n
		}
	}
	return out
}
func validRSSI(n *float64) *float64 {
	if n == nil || *n >= 0 || *n < -160 {
		return nil
	}
	return n
}
func radioDetails(name string) map[string]any {
	if !safeInterface(name) {
		return nil
	}
	req, _ := json.Marshal(map[string]string{"device": name})
	b, e := runCommand(time.Second, nil, "ubus", "call", "iwinfo", "info", string(req))
	return obj(decodeSource(rawSource{b, e}))
}
func radioClients(name string) ([]map[string]any, bool) {
	if !safeInterface(name) {
		return nil, false
	}
	req, _ := json.Marshal(map[string]string{"device": name})
	b, e := runCommand(time.Second, nil, "ubus", "call", "iwinfo", "assoclist", string(req))
	v := obj(decodeSource(rawSource{b, e}))
	if v == nil {
		return nil, false
	}
	_, ok := v["results"]
	return rows(v["results"]), ok
}
func collectWireless(v any) ([]WifiView, map[string]map[string]any, map[string]DeviceView) {
	aps := []WifiView{}
	stas := map[string]map[string]any{}
	clients := map[string]DeviceView{}
	keys := []string{}
	for k := range obj(v) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, radio := range keys {
		r := obj(obj(v)[radio])
		up, _ := truth(r["up"])
		for _, iv := range list(r["interfaces"]) {
			i := obj(iv)
			cfg := obj(i["config"])
			name := first(i, "ifname", "name")
			if !safeInterface(name) {
				continue
			}
			info := radioDetails(name)
			ssid := first(info, "ssid")
			if ssid == "" {
				ssid = first(cfg, "ssid")
			}
			mode := strings.ToLower(first(cfg, "mode"))
			if mode == "" {
				mode = strings.ToLower(first(info, "mode"))
			}
			if mode == "sta" || mode == "client" {
				if info == nil {
					info = map[string]any{}
				}
				info["ssid"] = ssid
				stas[name] = info
				continue
			}
			if mode != "ap" && mode != "master" {
				continue
			}
			if disabled, _ := truth(cfg["disabled"]); disabled {
				continue
			}
			ap := WifiView{Name: name, SSID: ssid, Up: up, Channel: num(info, "channel")}
			if f := num(info, "frequency"); f != nil {
				if *f >= 5925 {
					ap.Band = "6 GHz"
				} else if *f >= 4900 {
					ap.Band = "5 GHz"
				} else {
					ap.Band = "2.4 GHz"
				}
			}
			if ds, ok := radioClients(name); ok {
				n := len(ds)
				ap.Clients = &n
				for _, d := range ds {
					mac := strings.ToLower(first(d, "mac"))
					if _, err := net.ParseMAC(mac); err == nil {
						clients[mac] = DeviceView{MAC: mac, Interface: name, Connection: "Wi-Fi", SSID: ssid, State: "associated"}
					}
				}
			}
			aps = append(aps, ap)
			if len(aps) >= 16 {
				return aps, stas, clients
			}
		}
	}
	return aps, stas, clients
}
func makeInternet(raw map[string]rawSource, stas map[string]map[string]any, uci []byte, network []byte) InternetView {
	out := InternetView{State: "unknown", Links: []UplinkView{}, Note: "Selection for new default-policy traffic; existing sessions and explicit rules may use another uplink."}
	policies, health := parseMwan(raw["failover"].data)
	policy, configured := defaultPolicy(uci)
	out.Policy = policy
	disabled := map[string]bool{}
	for _, section := range uciSections(network) {
		if section.typ == "interface" && (uv(section, "disabled") == "1" || uv(section, "enabled") == "0" || uv(section, "auto") == "0") {
			disabled[section.name] = true
		}
	}
	simState := strings.ToLower(strings.TrimSpace(string(raw["sim"].data)))
	noSIM := raw["sim"].err == nil && (simState == "not inserted" || simState == "not inserted." || simState == "absent")
	selected, known := policies[policy]
	out.Source = "mwan3 IPv4 policy"
	if !known {
		out.Source = "unavailable"
	}
	interfaces := list(obj(decodeSource(raw["interfaces"]))["interface"])
	signal := signalValues(raw["signal"].data)
	mobile := obj(decodeSource(raw["mobile"]))
	operator := strings.TrimSpace(string(raw["operator"].data))
	if raw["operator"].err != nil {
		operator = ""
	}
	if operator == "" {
		operator = first(mobile, "operator", "operator_name")
	}
	for _, iv := range interfaces {
		i := obj(iv)
		name := first(i, "interface")
		dev := first(i, "l3_device", "device")
		up, _ := truth(i["up"])
		_, member := health[name]
		share, active := selected[name]
		hasDefault := false
		for _, rv := range list(i["route"]) {
			r := obj(rv)
			if first(r, "target") == "0.0.0.0" {
				hasDefault = true
			}
		}
		// Skip duplicate netifd _4 aliases when the parent points to the same device.
		if strings.HasSuffix(name, "_4") {
			parent := strings.TrimSuffix(name, "_4")
			skip := false
			for _, pv := range interfaces {
				p := obj(pv)
				if first(p, "interface") == parent && first(p, "l3_device", "device") == dev {
					skip = true
				}
			}
			if skip {
				continue
			}
		}
		if !member && !active && !hasDefault {
			continue
		}
		if disabled[name] {
			continue
		}
		link := UplinkView{Name: name, Device: dev, Kind: "ethernet", Label: name, State: "unknown", Uptime: num(i, "uptime")}
		for _, addr := range list(i["ipv4-address"]) {
			if p := first(obj(addr), "address"); net.ParseIP(p) != nil {
				link.IP = p
				break
			}
		}
		// Netifd may put IPv4 addresses and the live device on a dynamic _4 child.
		for _, child := range interfaces {
			ch := obj(child)
			if first(ch, "interface") == name+"_4" {
				if childUp, known := truth(ch["up"]); known && childUp {
					up = true
				}
				if d := first(ch, "l3_device", "device"); d != "" {
					link.Device = d
					dev = d
				}
				if link.IP == "" {
					for _, addr := range list(ch["ipv4-address"]) {
						if p := first(obj(addr), "address"); net.ParseIP(p) != nil {
							link.IP = p
							break
						}
					}
				}
			}
		}
		if !up {
			link.State = "offline"
		} else if health[name] == "offline" {
			link.State = "offline"
		} else if known {
			link.State = "standby"
		}
		if active && up {
			link.State = "active"
			link.Share = &share
		}
		if info, ok := stas[dev]; ok {
			link.Kind = "wifi"
			link.Label = first(info, "ssid")
			link.RSSI = validRSSI(num(info, "signal"))
		} else if strings.HasPrefix(name, "mob") || strings.HasPrefix(dev, "qmimux") || first(i, "proto") == "qmi" {
			link.Kind = "mobile"
			link.Label = text(operator)
			if strings.EqualFold(link.Label, "n/a") {
				link.Label = ""
			}
			link.RSSI = validRSSI(signal["rssi"])
			link.RSRP = signal["rsrp"]
			link.SINR = signal["sinr"]
			link.Carrier = first(mobile, "conntype", "connection_type", "technology")
			slot := first(mobile, "sim_slot", "sim_position", "sim")
			if slot == "1" || slot == "2" {
				link.SIM = slot
			}
			if noSIM && !active {
				continue
			}
			if noSIM {
				present := false
				link.Present = &present
			} else if simState == "inserted" || up {
				present := true
				link.Present = &present
			}
			if link.SIM == "" {
				re := regexp.MustCompile(`^mob[0-9]+s([12])a[0-9]+(?:_4)?$`)
				if m := re.FindStringSubmatch(name); m != nil {
					link.SIM = m[1]
				}
			}
		}
		out.Links = append(out.Links, link)
	}
	if known {
		out.State = "offline"
		for _, l := range out.Links {
			if l.State == "active" {
				out.State = "active"
			}
		}
		if len(selected) > 1 {
			out.State = "multiple"
		}
	}
	if !configured && !known {
		// Without failover, use the actual best main-table default route.
		routes := list(decodeSource(raw["routes"]))
		best := math.Inf(1)
		dev := ""
		ambiguous := false
		for _, rv := range routes {
			r := obj(rv)
			m := 0.0
			if p := num(r, "metric"); p != nil {
				m = *p
			}
			d := first(r, "dev")
			if m < best {
				best = m
				dev = d
				ambiguous = false
			} else if m == best && d != dev {
				ambiguous = true
			}
		}
		if dev != "" && !ambiguous {
			out.Source = "main routing table (no configured failover)"
			for i := range out.Links {
				if out.Links[i].Device == dev && out.Links[i].State != "offline" {
					out.Links[i].State = "active"
					out.State = "active"
				}
			}
		}
	}
	sort.SliceStable(out.Links, func(i, j int) bool {
		if (out.Links[i].State == "active") != (out.Links[j].State == "active") {
			return out.Links[i].State == "active"
		}
		return out.Links[i].Name < out.Links[j].Name
	})
	return out
}
