package main

// Read-only router telemetry. Missing observations are never invented.
import (
	"net"
	"sort"
	"strings"
	"time"
)

func devicesView(neigh any, associated map[string]DeviceView, leases []byte, local map[string]bool, raynet string, axiom Seen, now time.Time) []DeviceView {
	names := map[string]string{}
	leaseIPs := map[string]string{}
	for _, line := range strings.Split(string(leases), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[3] != "*" {
			mac := strings.ToLower(f[1])
			names[mac] = text(f[3])
			leaseIPs[mac] = f[2]
		}
	}
	out := []DeviceView{}
	seen := map[string]bool{}
	for _, nv := range list(neigh) {
		m := obj(nv)
		dev := first(m, "dev")
		if !local[dev] {
			continue
		}
		mac := strings.ToLower(first(m, "lladdr"))
		ip := first(m, "dst")
		if net.ParseIP(ip) == nil || mac == "" {
			continue
		}
		states := []string{}
		for _, s := range list(m["state"]) {
			states = append(states, text(s))
		}
		if s := first(m, "state"); s != "" {
			states = append(states, s)
		}
		st := strings.Join(states, " ")
		state := "recently_seen"
		if strings.Contains(st, "FAILED") || strings.Contains(st, "INCOMPLETE") {
			continue
		}
		if strings.Contains(st, "REACHABLE") {
			state = "reachable"
		}
		d := DeviceView{Name: names[mac], MAC: mac, IP: ip, Interface: dev, Connection: "LAN", State: state}
		if dev == raynet {
			d.Connection = "RayNet"
		}
		if ap, ok := associated[mac]; ok {
			d.Connection = ap.Connection
			d.Interface = ap.Interface
			d.State = ap.State
			d.SSID = ap.SSID
		}
		if axiom.IP == ip && now.Before(axiom.Expires) {
			d.Name = text(axiom.Name)
			d.State = "observed"
		}
		out = append(out, d)
		seen[mac] = true
		if len(out) >= 256 {
			break
		}
	}
	for mac, d := range associated {
		if seen[mac] || len(out) >= 256 {
			continue
		}
		d.Name = names[mac]
		d.IP = leaseIPs[mac]
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Connection != out[j].Connection {
			return out[i].Connection < out[j].Connection
		}
		return out[i].IP < out[j].IP
	})
	return out
}
func publicLogs(data []byte) []string {
	out := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		// Only the emulator's bounded event vocabulary is public, never arbitrary syslog.
		if !strings.Contains(line, serviceName) || !(strings.Contains(line, "INFO ready package=") || strings.Contains(line, "Axiom/MFD detected") || strings.Contains(line, "app/client detected") || strings.Contains(line, "INFO shutdown") || strings.Contains(line, "INFO stopped")) {
			continue
		}
		if strings.Contains(strings.ToLower(line), "password") || strings.Contains(strings.ToLower(line), "token=") {
			continue
		}
		if len(line) > 480 {
			line = line[:480]
		}
		out = append(out, line)
	}
	if len(out) > 30 {
		out = out[len(out)-30:]
	}
	return out
}

// Include configured static leases and known DHCP clients without claiming that
// a historical lease is currently online. Bridge-only MACs retain an empty IP.
func mergeKnownDevices(devices []DeviceView, dhcp, leases []byte, fdb any, local map[string]bool) []DeviceView {
	byMAC := map[string]int{}
	byIP := map[string]int{}
	for i, d := range devices {
		if d.MAC != "" {
			byMAC[d.MAC] = i
		}
		if d.IP != "" {
			byIP[d.IP] = i
		}
	}
	add := func(d DeviceView) {
		d.MAC = strings.ToLower(d.MAC)
		if _, e := net.ParseMAC(d.MAC); e != nil && net.ParseIP(d.IP) == nil {
			return
		}
		idx, ok := byMAC[d.MAC]
		if d.MAC == "" {
			ok = false
		}
		if !ok && d.IP != "" {
			idx, ok = byIP[d.IP]
		}
		if ok {
			if devices[idx].Name == "" {
				devices[idx].Name = d.Name
			}
			if devices[idx].IP == "" {
				devices[idx].IP = d.IP
			}
			return
		}
		if len(devices) >= 256 {
			return
		}
		if d.MAC != "" {
			byMAC[d.MAC] = len(devices)
		}
		if d.IP != "" {
			byIP[d.IP] = len(devices)
		}
		devices = append(devices, d)
	}
	for _, line := range strings.Split(string(leases), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && net.ParseIP(f[2]) != nil {
			name := f[3]
			if name == "*" {
				name = ""
			}
			add(DeviceView{Name: text(name), MAC: f[1], IP: f[2], Connection: "LAN", State: "known"})
		}
	}
	for _, s := range uciSections(dhcp) {
		if s.typ != "host" {
			continue
		}
		name := uv(s, "name")
		ips := strings.Fields(uv(s, "ip"))
		macs := strings.Fields(strings.ReplaceAll(uv(s, "mac"), ",", " "))
		if len(ips) == 0 {
			continue
		}
		mac := ""
		if len(macs) > 0 {
			mac = macs[0]
		}
		add(DeviceView{Name: text(name), MAC: mac, IP: ips[0], Connection: "LAN", State: "known"})
	}
	for _, fv := range list(fdb) {
		f := obj(fv)
		dev := first(f, "dev")
		master := first(f, "master")
		if !local[master] && !local[dev] {
			continue
		}
		mac := strings.ToLower(first(f, "mac"))
		hw, e := net.ParseMAC(mac)
		if e != nil || len(hw) == 0 || hw[0]&1 != 0 {
			continue
		}
		skip := false
		for _, fl := range list(f["flags"]) {
			if text(fl) == "self" || text(fl) == "permanent" {
				skip = true
			}
		}
		if skip || strings.EqualFold(first(f, "state"), "permanent") {
			continue
		}
		if idx, ok := byMAC[mac]; ok {
			if strings.HasPrefix(dev, "vxlan") {
				devices[idx].Connection = "VPN"
			}
			continue
		}
		add(DeviceView{MAC: mac, Interface: dev, Connection: "LAN", State: "recently_seen"})
	}
	sort.SliceStable(devices, func(i, j int) bool {
		if devices[i].Connection != devices[j].Connection {
			return devices[i].Connection < devices[j].Connection
		}
		return devices[i].IP < devices[j].IP
	})
	return devices
}
