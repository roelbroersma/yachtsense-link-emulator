# YachtSense Link Emulator

**Use your Teltonika as the wired internet router for a Raymarine Axiom, while keeping your normal boat Wi-Fi.**

YachtSense Link Emulator supplies the YachtSense-specific discovery identity and
HTTP connection check that an Axiom / LightHouse installation expects from a
YachtSense Link router. The Teltonika still handles the actual internet connection,
DNS, routing, firewall and WAN failover; the emulator does not replace those services.

It has three complementary functions:

1. **Internet over Ethernet / RayNet.** An Axiom already wired to the boat router
   can use that path instead of making a separate Wi-Fi connection just for internet.
2. **Raymarine app discovery.** A selective mDNS relay makes the MFD's advertised
   services visible on the chosen phone/tablet networks. Video and control traffic
   still go directly to the Axiom, not through an application proxy.
3. **A router status page.** A compact, login-free dashboard shows the active WAN
   and received signal, boat Wi-Fi, devices, profile, GPS, VPN and services. Settings
   and service controls remain in the authenticated Teltonika WebUI.

```text
 Internet: marina Wi-Fi / mobile / wired WAN
                         |
                   Teltonika RUTX14
                   + this emulator
                     /         \
     Normal boat Wi-Fi         Ethernet / RayNet
       Phone / tablet          Axiom + other wired devices
       192.168.40.x             198.18.x.x
```

**Current release: 1.2.2 · RUTX14 · RutOS `RUTX_R_00.07.25.3`.**
[Download the release](https://github.com/roelbroersma/yachtsense-link-emulator/releases/tag/v1.2.2)

This is an independent, third-party project, not Raymarine or Teltonika firmware.
It emulates the relevant recognition and discovery functions, not the complete
YachtSense Link product, cloud service or hardware.

## Install or upgrade

Download **`yachtsense-link-emulator_1.2.2-1_RUTX_00.07.25.3.tar.gz`** from the
release and upload it, **without extracting**, through **System → Package Manager
→ Upload package**. Do not use the firmware-upgrade page or GitHub's automatic
“Source code” archives. The standalone `.ipk` is the CLI alternative.

An upgrade does not require removing the previous package. Existing configuration
is retained. After the upload completes, allow about 20 seconds for menu/API
registration and sign out and back in to the router WebUI.

Open **Services → YachtSense Link Emulator**. On a new installation, enable the
service and use **Automatic** for the RayNet interface: it finds the interface
already owning the configured address, normally `198.18.0.1/21`. Select the network
used by the Raymarine app, for example `br-lan`. Upgrades preserve existing choices.

Configure the underlying RayNet address, Axiom gateway/DNS and permitted routing
in RutOS first. IP-address management is off by default; the emulator does not
silently reconfigure a VLAN, DHCP, firewall, VPN or WAN connection.

An identical package version is not a new upgrade. Version 1.2.1 was a local test
build; **1.2.2 is newer than both 1.2.0 and 1.2.1**.

## Open the right page

| Purpose | Default address / port | Login |
| --- | --- | --- |
| Router status from LAN or VPN | `http://192.168.40.1:8088/` | No; read-only |
| Router status from RayNet | `http://198.18.0.1:8088/` | No; read-only |
| Emulator settings | **Services → YachtSense Link Emulator** in the normal router WebUI | Yes |
| Axiom connection check | TCP **7777** on the RayNet-side router address | HTTP `200 OK`, not a settings page |
| YachtSense / MFD discovery | UDP **5353**, multicast `224.0.0.251` | mDNS, not a webpage |

The LAN address is an example; use your router's actual address. The status port
is configurable and is advertised in the YachtSense DNS-SD service. With the
status page disabled the advertised port falls back to `80`; the **7777 health
listener remains separate**. The dashboard starts with the enabled emulator.

On an Axiom, the **YachtSense Link** item in the top-right status area opens the
router page. The project's dashboard is served at its advertised port; use the
direct URL above when checking whether a particular LightHouse version follows
that advertisement. The original YachtSense router's settings interface is not
reproduced or opened without authentication.

## What the dashboard shows

WAN cards use real interface names, such as `wan1` and the configured mobile
interface name. RSSI belongs to the upstream harbour access point or mobile modem,
not an onboard phone. Absent SIMs, explicitly disabled WANs and unavailable IP
fields are omitted. The active profile is the actual RutOS profile, not one
inferred from a GPS location.

**Services** contains one **RayNet** row with its router interface/address and
emulator state, alongside **RMS** and any **VXLAN** interfaces. **Axiom, Cerbo GX,
Wi-Fi clients and wired LAN devices are listed in Devices**, not duplicated in a
separate RayNet card. The public page no longer shows the relay-engine status;
its controls and diagnostics remain available after login. This is a display
change, not a change to discovery forwarding.

[Dashboard fields, access control and data sources →](docs/public-status.md)

## Discovery, routing and remote access

The emulator publishes a YachtSense Link identity (`E70640`) using mDNS/DNS-SD
and answers the connection monitor. On different app/RayNet interfaces it can
selectively relay Raymarine service records and their related lookups. Automatic
mode can instead use an eligible existing Avahi reflector; it does not change
Avahi configuration or create a second overlapping reflector.

Seeing an Axiom in a device list proves only that it has been observed. For app
viewing/control, the phone must also receive and accept the discovery replies,
reach the advertised MFD address/ports, and receive the return traffic.

A home VPN can carry that routed traffic. A discovery-only VXLAN or another
explicit multicast path may also be needed; an ordinary routed VPN does not
itself extend mDNS. The package neither creates the VPN nor opens its firewall.

[How the protocol works, ports, VLANs, Avahi and VPN/VXLAN →](docs/networking.md)

## Compatibility and verification

The supplied package targets **RUTX14 / RutOS 7.25.3**, using the Yocto architecture
name `cortexa7hf-neon-vfpv4` and installation root `/usr/local`. Earlier 7.24 packages
are separate historical downloads, not interchangeable with this package. Other
routers and firmware versions are not covered by this release's validation.

During field testing, the owner confirmed Axiom software-update checking over
RayNet without a separate Axiom Wi-Fi internet connection. The corrected WebUI,
RPC permissions and discovery observations were also confirmed on that router.
**Complete Raymarine iPhone viewing/control through the home VPN has not yet been
confirmed.** Those observations do not establish universal LightHouse compatibility.

The release is built and checked with automated Go, JavaScript, Lua-adapter and
archive tests. Browser tests use synthetic router data at desktop, phone and
800×480 sizes. They do not replace installation and display testing on the router
and physical Axiom.

## Troubleshooting

Keep these checks separate: **package installation**, **WebUI/API**, **public
status page**, **Axiom internet**, and **Raymarine app connectivity**. A green
Package Manager entry does not prove all five.

```sh
opkg status tlt_custom_pkg_yachtsense-link-emulator
/usr/local/usr/sbin/yachtsense-link-emulator --api diagnostics
ubus -v list yachtsense-link-emulator
ubus call yachtsense-link-emulator status '{}'
api get /yachtsense-link-emulator-v1100/status
curl --max-time 5 http://127.0.0.1:8088/api/status
```

`v1100` in that API route is the retained interface identifier, not the installed
package version. Check the returned HTTP/application result, not just the CLI
exit code. A 404 route, 403 permission error and missing ubus object have different
causes; do not solve them by making all router configuration publicly writable.

[RutOS installation and permission troubleshooting →](docs/rutos-7.25.3-fixes.md)

## Build and contribute

The build uses Linux, Go, Python, Node.js, GNU `ar`, Bash and LuaJIT (or `texlua`).
CI pins Go **1.23.2**, Python **3.13.5** and Node **22**. The Go binary is statically
cross-compiled for ARMv7 without external Go dependencies.

```sh
LUA_TEST=luajit bash scripts/check.sh
# Optional real-browser layout checks; requires Playwright and Chromium.
python3 tests/dashboard_browser_test.py
```

The builder creates the WebUI upload in `dist/` and the CLI IPK in `build/`.
Every release uses a new `VERSION`, matching archive metadata and versioned
management assets; existing release files are not silently replaced. Report the
firmware/package version and the relevant diagnostic result when reporting a bug.

[Change history](CHANGELOG.md) · [Release notes](RELEASE.md) · [MIT license](LICENSE)
