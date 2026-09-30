package main

// Read-only router telemetry. Missing observations are never invented.
import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"
)

func collectRouter(ctx context.Context, p Paths, r Resolved, state Runtime) RouterView {
	raw := probeAll(ctx)
	now := time.Now().UTC()
	out := RouterView{Observed: now, Firmware: strings.TrimSpace(string(readSmall("/etc/version"))), Sources: map[string]string{}, VPN: []VPNView{}}
	for k, s := range raw {
		if s.err == nil {
			out.Sources[k] = "available"
		} else {
			out.Sources[k] = "unavailable"
		}
	}
	aps, stas, associated := collectWireless(decodeSource(raw["wireless"]))
	out.WIFI = aps
	out.Internet = makeInternet(raw, stas, systemConfig("mwan3"), systemConfig("network"))
	if raw["profile"].err == nil {
		out.Profile = text(strings.TrimSpace(string(raw["profile"].data)))
	}
	out.GPS = gpsView(decodeSource(raw["gps"]), decodeSource(raw["gps_fix"]), decodeSource(raw["geofences"]), now)
	out.RMS = rmsState(decodeSource(raw["rms"]))
	if out.RMS == "unknown" {
		for _, row := range rows(decodeSource(raw["rms_api"])) {
			if s := rmsState(row); s != "unknown" {
				out.RMS = s
				break
			}
		}
	}
	out.VPN = vpnAPI(decodeSource(raw["ipsec_api"]), "IPsec")
	if raw["ipsec"].err == nil {
		observed := swanVPN(raw["ipsec"].data)
		known := map[string]bool{}
		for _, v := range observed {
			known[v.Name] = true
		}
		for i := range out.VPN {
			if !known[out.VPN[i].Name] {
				out.VPN[i].State = "disconnected"
				observed = append(observed, out.VPN[i])
			}
		}
		out.VPN = observed
	}
	out.VPN = append(out.VPN, vpnAPI(decodeSource(raw["openvpn"]), "OpenVPN")...)
	out.VPN = append(out.VPN, wgVPN(raw["wireguard"].data, raw["wg_transfer"].data, now)...)
	out.VXLAN = vxlanView(decodeSource(raw["vxlan"]))
	local := map[string]bool{r.Raynet: true}
	for _, n := range r.Apps {
		local[n] = true
	}
	for _, ap := range aps {
		local[ap.Name] = true
	}
	// Include all local netifd LAN/VLAN interfaces, not only the app bridge.
	uplinks := map[string]bool{}
	for _, link := range out.Internet.Links {
		uplinks[link.Device] = true
	}
	for _, iv := range list(obj(decodeSource(raw["interfaces"]))["interface"]) {
		i := obj(iv)
		dev := first(i, "l3_device", "device")
		if dev != "" && !uplinks[dev] && first(i, "interface") != "loopback" && len(list(i["ipv4-address"])) > 0 {
			local[dev] = true
		}
	}
	out.Devices = devicesView(decodeSource(raw["neighbors"]), associated, readSmall("/tmp/dhcp.leases"), local, r.Raynet, state.Axiom, now)
	out.Devices = mergeKnownDevices(out.Devices, systemConfig("dhcp"), readSmall("/tmp/dhcp.leases"), decodeSource(raw["fdb"]), local)
	out.Logs = publicLogs(raw["logs"].data)
	return out
}

// Fixed commands and paths above are not influenced by HTTP query parameters.
func validateRouterView(v RouterView) error {
	if len(v.Devices) > 256 || len(v.WIFI) > 16 || len(v.GPS.Fences) > 64 {
		return errors.New("telemetry limit exceeded")
	}
	return nil
}
func usablePublicIPv4(value string) bool {
	a, e := netip.ParseAddr(strings.TrimSpace(value))
	if e != nil || !a.Is4() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4"} {
		if netip.MustParsePrefix(cidr).Contains(a) {
			return false
		}
	}
	return true
}
