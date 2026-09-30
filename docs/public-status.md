# Public router status

The status dashboard is a separate, login-free page for the Axiom, a phone or a
browser on the permitted LAN/VPN. It is for viewing router status; the **Manage**
button opens the existing authenticated Teltonika settings page.

## Addresses and access

The defaults are **8088** for the dashboard and **7777** for the separate Axiom
HTTP connection check. Examples:

```text
http://192.168.40.1:8088/   LAN / routed VPN
http://198.18.0.1:8088/     RayNet
```

Use the actual router address when it differs. The dashboard listens on all local
IPv4 addresses when enabled, but existing firewall rules still apply. It is not
made internet-accessible by adding a WAN port-forward or by changing the firewall.

The authenticated emulator configuration exposes:

| Setting | Default | Meaning |
| --- | --- | --- |
| `dashboard_enabled` | `1` | Start the status server with the emulator |
| `dashboard_port` | `8088` | Dashboard HTTP port; must differ from the health port |
| `dashboard_allow_all` | `1` | Accept any source network that can already reach this listener |
| `dashboard_allowed_cidrs` | `192.168.4.0/24` | Extra IPv4 source networks when allow-all is disabled |

Allow-all was requested for LAN/VPN testing and is retained on upgrades. With it
disabled, the server permits loopback, the resolved RayNet subnet, app-interface
subnets and the configured extra CIDRs. The decision uses the actual TCP peer,
not forwarded HTTP headers. The server itself does not require a login to read
GPS coordinates, device names or network state, so access is controlled here and
by the router firewall. The existing administration login is unchanged.

## Display

| Section | Shown |
| --- | --- |
| Internet | Real WAN interface names, default-policy active/standby state, upstream RSSI, provider/SSID and available IP fields |
| Profile | The current RutOS profile name, such as `onWiFi` |
| GPS | Fresh position, fix state, satellites and named enabled geofences |
| Onboard WiFi | Broadcast SSIDs, band, channel, association count and state |
| VPN | Tunnel name with protocol, connection state and available time/RX/TX |
| Services | RayNet service/interface, RMS connection and configured VXLAN interfaces |
| Devices | Wi-Fi, wired LAN and RayNet hosts together, with name, IP, connection type and observation state |
| Logs | Up to 30 filtered emulator events, collapsed by default |

**RayNet has one Services row**, with interface and router CIDR. Its Running state
refers to the emulator process, not a verified internet connection or completed
mobile-app session. Axiom and Cerbo GX are individual entries in Devices, not
additional RayNet-service rows. There is no public Built-in relay/Avahi status row;
removing that display does not disable or change the relay.

Empty IP/public-IP fields, explicitly disabled WANs and mobile links confirmed
without a SIM are omitted. Unknown readings are not invented. Current Wi-Fi
associations, reachable neighbours and fresh observations can appear Online;
old leases/static entries remain Known or Seen rather than being asserted online.
The device table is searchable and does not hide wired clients in favour of Wi-Fi.

## Data sources and meaning

The daemon collects fixed, bounded, read-only commands. Public requests read a
cached, typed snapshot; they do not execute a caller-specified command or probe.

- **WAN and Wi-Fi:** netifd/iwinfo ubus data and `mwan3 status`, matched against the
  configured default IPv4 policy. This is not a claim that every existing flow or
  explicit routing rule uses that WAN. An upstream station's RSSI is distinct
  from onboard client signal. Mobile data uses `gsmctl -E/-q/-o/-z`; an unavailable
  source is not treated as proof that the SIM is absent.
- **Public IPv4:** a fixed HTTPS request to `api.ipify.org`, bound to an active
  interface, at most once a minute per interface. Failed checks remove the field.
- **Profile and GPS:** `profiles.general.profile`, the GPS API and enabled circle
  definitions from the geofencing API. Membership requires a fresh valid fix.
  The dashboard neither changes a profile nor triggers a geofence event.
- **VPN:** IPsec API plus installed CHILD_SAs, OpenVPN status and WireGuard
  handshakes/counters. A recent WireGuard handshake is labelled as such rather
  than being converted into proof of a live application session.
- **RMS and VXLAN:** RMS's reported connection state and local VXLAN link/counter
  data. VXLAN Up says the local interface is up, not that the peer is reachable.
- **Devices:** Wi-Fi association lists, IPv4 neighbour entries, local bridge
  forwarding entries, DHCP lease names and static leases. Unobserved hosts cannot
  be discovered from a lease name alone; a quiet wired host may remain Known.

Sources and snapshot timestamps remain in the JSON for diagnostics. Firmware
variations can leave fields unknown; a missing measurement is not proof that a
service is offline. No generic router logs, credentials or arbitrary raw command
output are exposed by the public endpoint.

## HTTP and Axiom integration

The server accepts only **GET/HEAD** for its HTML/CSS/JavaScript assets and
`/api/status`. There are no public configuration, restart, write or RPC-passthrough
endpoints. Assets use plain ES5 and XMLHttpRequest, without a CDN or inline-script
dependency. Responses use `Cache-Control: no-store`.

When the dashboard is enabled, its configured port is advertised in the YachtSense
DNS-SD SRV record. The earlier SRV port was 80; the health check still uses 7777.
Opening the advertised dashboard from a physical Axiom remains a separate
compatibility check from opening it in a desktop browser.

Raymarine's [original YachtSense Link instructions](https://docs.raymarine.com/81406/en-US/latest/AccessingTheWebInterfaceFromARaymar-FA2AE06D.html)
describe the top-right status menu and the genuine router's web interface. This
project supplies its own read-only page, not that full manufacturer interface.

## Validation

`bash scripts/check.sh` covers HTTP methods/source restrictions, telemetry parsing,
public/admin UI logic and the built package. In particular, tests require RayNet
under Services, no duplicate Axiom service row and preservation of Wi-Fi/LAN/RayNet
devices. `python3 tests/dashboard_browser_test.py` renders the real assets with
synthetic data at desktop, 800×480 MFD and phone sizes and checks text escaping,
conditional fields and layout. These tests do not emulate RutOS or LightHouse.

[Project purpose and installation](../README.md) · [Discovery and routing](networking.md)
