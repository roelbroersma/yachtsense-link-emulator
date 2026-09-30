#!/usr/bin/env python3
"""Apply reviewed integration changes to the verified 1.1.2 base before testing.

This preparation file is removed before the ordinary source tree is committed
on main. It never runs on a router and never modifies router configuration.
"""
from pathlib import Path
import subprocess
R=Path(__file__).resolve().parents[1]
def edit(path, old, new):
 p=R/path;s=p.read_text();assert old in s,(path,old);p.write_text(s.replace(old,new))
C='cmd/yachtsense-link-emulator/config.go'
edit(C,'LogLines         int      `json:"log_lines"`','LogLines         int      `json:"log_lines"`\n Dashboard bool `json:"dashboard_enabled"`\n DashboardPort int `json:"dashboard_port"`\n DashboardAllowAll bool `json:"dashboard_allow_all"`\n DashboardCIDRs string `json:"dashboard_allowed_cidrs"`')
edit(C,'Settings{MDNS: true, HTTP: true,','Settings{Dashboard: true, DashboardPort: 8088, DashboardAllowAll: true, DashboardCIDRs: "192.168.4.0/24", MDNS: true, HTTP: true,')
edit(C,'"log_level": &c.LogLevel}','"log_level": &c.LogLevel, "dashboard_allowed_cidrs": &c.DashboardCIDRs}')
edit(C,'"remove_ip_on_stop": &c.RemoveIP}','"remove_ip_on_stop": &c.RemoveIP, "dashboard_enabled": &c.Dashboard, "dashboard_allow_all": &c.DashboardAllowAll}')
edit(C,'"log_lines": &c.LogLines}','"log_lines": &c.LogLines, "dashboard_port": &c.DashboardPort}')
edit(C,'func validateSettings(c Settings) error {','''func validateSettings(c Settings) error {
 if c.DashboardPort < 1024 || c.DashboardPort > 65535 || c.DashboardPort == c.HealthPort { return errors.New("dashboard port must be 1024..65535 and different from the health port") }
 if len(c.DashboardCIDRs) > 1024 { return errors.New("dashboard source list is too long") }
 for _,v := range strings.FieldsFunc(c.DashboardCIDRs,func(r rune)bool{return r==','||unicode.IsSpace(r)}) { p,err:=netip.ParsePrefix(v);if err!=nil||!p.Addr().Is4(){return errors.New("dashboard sources must be IPv4 CIDR networks")} }
''')
edit(C,'"log_lines": strconv.Itoa(c.LogLines)}','"log_lines": strconv.Itoa(c.LogLines), "dashboard_enabled": boolText(c.Dashboard), "dashboard_port": strconv.Itoa(c.DashboardPort), "dashboard_allow_all": boolText(c.DashboardAllowAll), "dashboard_allowed_cidrs": c.DashboardCIDRs}')
D='cmd/yachtsense-link-emulator/dns.go'
edit(D,'HealthPort                           int','HealthPort                           int\n DashboardPort int')
edit(D,'// Names, identity TXT records and the SRV port preserve the existing emulator.','// Identity and health remain compatible; the SRV port may select the read-only dashboard.')
edit(D,'// SRV 80 is identity metadata; the independent Axiom health listener is 7777.\n\tsrv := append([]byte{0, 0, 0, 0, 0, 80}, hw...)','''// Keep health on its separate port; advertise the dashboard as the router page.
 port:=c.DashboardPort;if port==0{port=80}
 srv:=appendUint16([]byte{0,0,0,0},uint16(port));srv=append(srv,hw...)''')
D='cmd/yachtsense-link-emulator/daemon.go'
edit(D,'if relay {\n\t\tcfg.RelayMode','if c.Dashboard { cfg.DashboardPort=c.DashboardPort }\n if relay {\n\t\tcfg.RelayMode')
edit(D,'\tstate.flush()\n\tlog.Printf("INFO ready','''
 if c.Dashboard {
  d,stop,err:=startDashboard(dc,p,c,r,state)
  if err!=nil { log.Printf("ERROR dashboard unavailable: %s",err) } else { defer stop();defer d.Close() }
 }
 state.flush()
 log.Printf("INFO ready''')
p=R/'package/root/etc/config/yachtsense_link_emulator'
p.write_text(p.read_text()+"\n\toption dashboard_enabled '1'\n\toption dashboard_port '8088'\n\toption dashboard_allow_all '1'\n\toption dashboard_allowed_cidrs '192.168.4.0/24'\n")
J='package/root/www/views/services/YachtSenseLinkEmulatorV1120.js'
edit(J,'log_lines:40});','log_lines:40,dashboard_enabled:true,dashboard_port:8088,dashboard_allow_all:true,dashboard_allowed_cidrs:"192.168.4.0/24"});')
edit(J,'h("h4","Individual components")','''h("h4","Status page"),
 this.toggle("dashboard_enabled","Read-only status page"),
 this.toggle("dashboard_allow_all","Allow all source networks"),
 h("div",{class:"ys-form-grid"},[
  this.field("dashboard_port","Status port","number","",{min:1024,max:65535}),
  this.form.dashboard_allow_all?null:this.field("dashboard_allowed_cidrs","Additional source networks","text","IPv4 CIDRs, comma-separated")
 ]),
 h("h4","Individual components")''')
edit(J,'"This does not change the advertised SRV identity port 80."','"Separate from the read-only status page."')
p=R/J;s=p.read_text().replace('1.1.2','1.2.0').replace('1120','1200');p.unlink();(p.parent/'YachtSenseLinkEmulatorV1200.js').write_text(s)
p=R/'package/root/www/assets/yachtsense-link-emulator-v1120.css';p.rename(p.with_name('yachtsense-link-emulator-v1200.css'))
for path in ['package/root/usr/share/vuci/menu.d/yachtsense-link-emulator.json','tests/ui_test.mjs','tests/package_test.py','scripts/check.sh']:
 p=R/path;p.write_text(p.read_text().replace('V1120','V1200').replace('v1120','v1200'))
p=R/'package/root/usr/libexec/yachtsense-link-emulator-reload-ui';p.write_text(p.read_text().replace('1.1.2','1.2.0'))
p=R/'scripts/check.sh';p.write_text(p.read_text().replace('node tests/ui_test.mjs','node --check cmd/yachtsense-link-emulator/dashboard/status.js\nnode tests/ui_test.mjs'))
(R/'VERSION').write_text('1.2.0\n')
p=R/'CHANGELOG.md';p.write_text('''## 1.2.0 — 2026-09-30

- Add a compact, login-free, read-only router dashboard on port 8088, advertised through YachtSense; health stays on its separate port.
- Show the actual default-policy failover uplink and its received RSSI, real interface names, optional public IPv4, onboard SSIDs, Wi-Fi and wired/known devices.
- Add the actual RutOS profile, GPS/named geofences, VPN timers/counters, RMS and VXLAN status.
- Hide absent SIMs, disabled WANs, missing IP fields, policy weights and redundant explanatory text.
- Add configurable source-network access, initially allowing all sources for the requested VPN test without changing firewall rules or admin authentication.
- Preserve the complete 1.1.2 installation and RPC permission fixes; add HTTP, telemetry and browser tests.

New telemetry and Axiom page integration require real-device validation.

'''+p.read_text())
p=R/'README.md';p.write_text('''# Router status dashboard — 1.2.0

For RUTX14 / RutOS RUTX_R_00.07.25.3. Upload the release `.tar.gz` without extracting or removing the previous package. The new read-only page is available at `http://192.168.40.1:8088/` (LAN/VPN) and `http://198.18.0.1:8088/` (RayNet), subject to existing firewall rules. `Manage` retains normal router authentication.

The status-page configuration includes enable, port, **Allow all source networks** (on for this requested test release), and extra source CIDRs when restriction is enabled. See [public status](docs/public-status.md) for data sources, limits, tests and deployment details.

The new page and parsers are tested with synthetic telemetry; observations from the actual router remain to be checked. Existing v1.1.2 integration documentation follows.

'''+p.read_text())
subprocess.run(['gofmt','-w',*map(str,(R/'cmd/yachtsense-link-emulator').glob('*.go'))],check=True)
print('Prepared ordinary 1.2.0 source files; previous RPC, API and installer integration retained')
