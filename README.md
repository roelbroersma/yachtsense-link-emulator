# YachtSense Link Emulator

**YachtSense Link Emulator** makes a Teltonika RUTX router look like a Raymarine YachtSense Link to both **Axiom / LightHouse** and the **Raymarine mobile app**.

That fixes two slightly odd limitations in the normal Raymarine setup:

1. An Axiom that is already connected to the router by **Ethernet / RayNet** can use that same wired connection for internet access. It no longer needs a second connection over the Axiom's own Wi-Fi just to get online.
2. The Raymarine app can discover and control the Axiom from your **normal boat Wi-Fi**, even when the phone and Axiom live on different routed subnets or VLANs.

To LightHouse, the Teltonika presents the same YachtSense Link discovery identity that Raymarine expects. To the mobile app, it also makes the Axiom's remote-view services visible on the selected app networks.

A typical setup looks like this:

```text
                     Teltonika RUTX
                  ┌───────────────────┐
Internet / 4G/5G ─┤                   ├─ Wi-Fi ─ Phone / tablet
                  │  YachtSense Link  │           192.168.40.x
                  │     Emulator      │
                  │                   ├─ Ethernet / RayNet ─ Axiom
                  └───────────────────┘                    198.18.x.x
```

The Axiom can now reach the internet through the RUTX over Ethernet, while the phone can stay on the boat's normal Wi-Fi and still use Raymarine screen mirroring and remote control.

With suitable routing and multicast transport, the same design can also be extended across a VPN. A bridged overlay such as **VXLAN over WireGuard/IPsec** is particularly useful because it can carry the layer-2 multicast discovery used by mDNS. A routed VPN can work as well when the emulator relays mDNS onto the VPN interface and the Axiom IP is routable from the remote client.

## Contents

- [What this changes](#what-this-changes)
- [How does it work?](#how-does-it-work)
- [Protocol](#protocol)
- [Raymarine app discovery relay](#raymarine-app-discovery-relay)
- [Internet over Ethernet](#internet-over-ethernet)
- [Screen mirroring from your own Wi-Fi](#screen-mirroring-from-your-own-wi-fi)
- [Remote access over VPN / VXLAN](#remote-access-over-vpn--vxlan)
- [mDNS and DNS-SD](#mdns-and-dns-sd)
- [Existing mDNS / Avahi handling](#existing-mdns--avahi-handling)
- [Logging and diagnostics](#logging-and-diagnostics)
- [Technical background](#technical-background)
- [Compatibility](#compatibility)
- [Installation](#installation)
- [RutOS WebUI](#rutos-webui)
- [Configuration](#configuration)
- [Build](#build)
- [License](#license)

## What this changes

### Axiom internet without switching to Wi-Fi

A normal Ethernet router is not automatically treated by LightHouse as a YachtSense Link internet source. That is why an Axiom can be physically connected to a router over Ethernet and still ask you to configure Wi-Fi when it needs internet access.

The emulator adds the YachtSense Link discovery and liveness behaviour that LightHouse checks. Once the RUTX is recognised as YachtSense Link, the Axiom can use the **existing Ethernet / RayNet path** to the router.

The RUTX then handles internet access like any other routed client:

```text
Axiom
  │ Ethernet / RayNet
  ▼
RUTX
  ├─ 4G / 5G
  ├─ Wi-Fi WAN
  ├─ Ethernet WAN
  └─ RutOS failover / load balancing
```

There is no need for the Axiom itself to associate with the RUTX Wi-Fi just to reach the internet.

### Raymarine app on your own Wi-Fi

The current Raymarine app discovers the MFD directly with mDNS/DNS-SD. It does not send the screen through YachtSense Link.

So the phone can remain connected to the normal RUTX Wi-Fi while the Axiom remains wired. The emulator makes the necessary Raymarine discovery visible across the selected interfaces, after which the app connects directly to the Axiom IP for video and control.

For example:

```text
Phone                     RUTX                         Axiom
192.168.40.50              192.168.40.1                198.18.0.23
     │                          │                            │
     └──── normal Wi-Fi ────────┤──── routed RayNet ────────┘

mDNS discovery: selectively relayed
RTSP/control:    normal routed unicast traffic
```

## How does it work?

There are two separate discovery paths.

### 1. Axiom recognises the RUTX as YachtSense Link

LightHouse browses `_http._tcp` over mDNS. The emulator publishes the YachtSense Link identity:

```text
id=E70640 AF002A4
model=Raymarine YachtSense Link
hostname=yachtsense-main
service=yachtsense-main Settings._http._tcp.local
```

LightHouse recognises product ID `E70640`, resolves the service to the RUTX RayNet-side address and then performs the YachtSense connection check on TCP port `7777`.

The emulator answers that check with HTTP `200 OK`.

### 2. The Raymarine app discovers the Axiom

The mobile app first sees `yachtsense-main` as an onboard YachtSense Link device. It then performs its own mDNS searches for the MFD.

The current Android app browses:

```text
_http._tcp.local
_rtsp._tcp.local
_rym_rrc._tcp.local
_raydb._tcp.local
_services._dns-sd._udp.local
```

The emulator can selectively relay those searches between the **Raymarine app interface(s)** and the **Axiom / RayNet interface**.

When the app has resolved the Axiom, it talks directly to the MFD. YachtSense Link is not in the video or touch-control data path.

## Protocol

### YachtSense Link advertisement

The default DNS-SD identity is:

```text
PTR  _http._tcp.local
     -> yachtsense-main Settings._http._tcp.local

SRV  yachtsense-main Settings._http._tcp.local
     -> yachtsense-main.local:80

TXT  id=E70640 AF002A4
TXT  model=Raymarine YachtSense Link
TXT  version=V142.242.530
TXT  mac=<MAC of selected Teltonika interface>

A    yachtsense-main.local
     -> <RUTX address on that network>
```

Defaults:

```text
Product ID:       E70640
Serial:           AF002A4
Hostname:         yachtsense-main
Service instance: yachtsense-main Settings
Version:          V142.242.530
```

The MAC address is read from the selected RUTX interface at runtime.

### Connection monitor

The Axiom also checks the YachtSense candidate on:

```text
TCP 7777
```

The emulator responds:

```http
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Cache-Control: no-store

OK
```

The DNS-SD SRV record still advertises TCP port `80`, just like the real YachtSense Link. Port `7777` is a separate LightHouse connection/liveness check.

## Raymarine app discovery relay

The relay is deliberately Raymarine-aware rather than a generic Bonjour reflector.

It recognises these service types:

| Service | Purpose |
|---|---|
| `_http._tcp.local` | Onboard devices / YachtSense Link |
| `_rtsp._tcp.local` | MFD screen stream discovery |
| `_rym_rrc._tcp.local` | Raymarine remote control |
| `_raydb._tcp.local` | MFD / Raymarine database discovery |
| `_services._dns-sd._udp.local` | DNS-SD service enumeration |

A DNS-SD discovery does not stop at the first PTR record. A typical Axiom announcement looks like:

```text
PTR  _rtsp._tcp.local
     -> Axiom 7._rtsp._tcp.local

SRV  Axiom 7._rtsp._tcp.local
     -> axiom-123.local:8554

TXT  ...
A    axiom-123.local
     -> 198.18.0.23
```

The emulator therefore learns the related instance and hostname while discovery is active and also relays the required follow-up `SRV`, `TXT`, `A` and `AAAA` lookups.

### Relay direction

```text
Raymarine app network                 Axiom / RayNet network

_http/_rtsp/_rym/_raydb queries ─────────────►
                                             Axiom
                                  ◄─────────── PTR/SRV/TXT/A/AAAA
```

The app-side networks are not bridged together and unrelated Bonjour traffic is not intentionally forwarded.

### Same bridge: no reflector needed

If the phone and Axiom are already on the same Linux bridge, mDNS is visible at layer 2 even when their IP addresses are in different subnets.

Example:

```text
br-lan
  RUTX 192.168.40.1/24
  RUTX 198.18.0.1/21

Phone 192.168.40.50/24
Axiom 198.18.0.23/21
```

The phone hears the Axiom's multicast directly. When it later opens `198.18.0.23`, it sends that unicast packet to its normal gateway and the RUTX routes it back onto the same bridge.

In **Automatic** mode the emulator detects this and does not start a needless reflector.

## Internet over Ethernet

This is one of the main reasons for the emulator.

An Axiom may already be hard-wired into the boat network but still refuse to treat an arbitrary Ethernet router as its internet source. LightHouse has specific YachtSense Link discovery logic, so simply supplying a DHCP gateway is not the same thing.

By reproducing the YachtSense identity and liveness check, the RUTX becomes the recognised wired internet path.

The actual internet traffic is then completely normal routing:

```text
Axiom 198.18.x.x
      │
      ▼
RUTX 198.18.0.1
      │
      ├─ mobile WAN
      ├─ marina Wi-Fi WAN
      └─ any other RutOS WAN/failover path
```

DHCP, DNS, NAT, firewalling and WAN selection remain ordinary RutOS configuration. The emulator only supplies the Raymarine-specific recognition layer that was missing.

## Screen mirroring from your own Wi-Fi

Once discovery is visible and the Axiom IP is routable, the Raymarine app connects directly to the MFD.

Current app behaviour includes:

```text
rtsp://<Axiom-IP>:8554/RAYMARINEMFD
```

and these remote-view/control ports:

| Port | Use |
|---|---|
| `8554/TCP` | RTSP screen stream |
| `50000/TCP` | Touch-capable remote control |
| `49111` | Control/discovery state used by the app |

So screen mirroring can work while the phone remains on your own RUTX Wi-Fi. There is no requirement for the phone to join the Axiom's own Wi-Fi when the discovery and routing are available through the router.

The video and touch-control TCP sessions are not proxied by the emulator; they are simply routed between the phone and Axiom.

## Remote access over VPN / VXLAN

The same principle is not limited to Wi-Fi on the boat.

If a remote network can see the Raymarine mDNS discovery and can route to the Axiom IP, the Raymarine app can in principle use the same direct MFD endpoints across that path.

A practical topology is:

```text
Home / remote LAN
      │
      │ WireGuard / IPsec
      │       + VXLAN
      ▼
RUTX / boat LAN ───── RayNet ───── Axiom
```

A **VXLAN or other L2 overlay** is convenient because mDNS is link-local multicast and can travel over the extended bridge just as it does locally. In that design the phone can appear to be on an app-side layer-2 network at the boat.

A pure routed VPN can also be used when:

- the VPN interface is selected as a Raymarine app interface for the relay;
- UDP/5353 discovery is delivered to that interface;
- the Axiom subnet is routed across the tunnel;
- the firewall permits the MFD's RTSP/control ports in both directions.

For remote use, latency and tunnel MTU obviously matter more than they do on the local boat Wi-Fi, especially for the RTSP stream.

## mDNS and DNS-SD

Raymarine discovery here uses **mDNS on UDP/5353**, not ordinary DNS on port 53.

```text
IPv4 multicast: 224.0.0.251
UDP port:        5353
```

DNS-SD uses familiar DNS record types such as `PTR`, `SRV`, `TXT`, `A` and `AAAA`, but exchanges them over that multicast channel.

Ordinary RutOS DNS on UDP/TCP `53` remains normal internet name resolution and is untouched by the emulator.

After discovery, the app switches to ordinary unicast IP connections to the Axiom.

## Existing mDNS / Avahi handling

Something listening on UDP/5353 does not automatically mean that a reflector already exists.

The package distinguishes between:

- a process listening on UDP/5353;
- `avahi-daemon` running as a normal mDNS responder;
- Avahi with `enable-reflector=yes`;
- `umdns`;
- an unknown 5353 listener.

The WebUI shows what it finds, including the process/socket information.

### Automatic

`Automatic` is the normal mode.

- Same Axiom/app interface: no relay is needed.
- Different interfaces and no reflector: the built-in Raymarine relay runs.
- Avahi reflector already active: the built-in reflector stays off to avoid duplicate reflection loops.
- Avahi without reflection, `umdns` or another reusable 5353 listener: reported, but not automatically treated as a reflector.

The daemon uses reusable multicast sockets so it can normally coexist with other mDNS responders.

### Force

`Force built-in relay` ignores the Avahi-reflector safeguard and is intended for testing or unusual configurations.

### Disabled

`Disabled` turns off cross-interface discovery reflection while leaving YachtSense Link emulation available.

## Logging and diagnostics

The service keeps the useful network events visible instead of making mDNS a black box.

Normal **Info** logging includes messages such as:

```text
YachtSense advertisement active interface=br-raynet address=198.18.0.1
Raymarine app/client query detected source=192.168.40.54 interface=br-lan service=_rtsp._tcp.local
Axiom/MFD detected name="Axiom 7" address=198.18.0.23 service=_rtsp._tcp.local port=8554
Axiom remote-control service detected address=198.18.0.23 port=50000
Avahi reflector detected; Automatic mode will not start a second reflector
```

Repeated browse messages are rate-limited.

**Debug** logging adds packet direction, packet size, learned follow-up names, YachtSense query replies and HTTP liveness requests.

The VuCI status page shows:

- YachtSense advertisement state;
- built-in relay active/inactive and the reason;
- Axiom / RayNet interface;
- app-side interface(s);
- last detected Axiom/MFD and IP;
- last MFD service and port;
- last Raymarine app/client source IP;
- last requested service;
- UDP/5353 listeners;
- Avahi and `umdns` state;
- recent service log lines.

## Technical background

The protocol behaviour was derived from the current Raymarine components rather than guessed from generic router behaviour.

### Axiom / LightHouse

Analysis of the Axiom ARMv7 LightHouse libraries shows a YachtSense-specific discovery path. LightHouse browses `_http._tcp`, reads the DNS-SD TXT `id` and checks the product identifier before the serial suffix.

For YachtSense Link that identifier is:

```text
E70640
```

The YachtSense Link firmware itself publishes:

```text
hostname: yachtsense-main
service:  yachtsense-main Settings._http._tcp
TXT id:   E70640 <serial>
model:    Raymarine YachtSense Link
```

A separate LightHouse component checks the candidate over TCP `7777`.

That is why a normal DHCP/default-gateway router is not equivalent to YachtSense Link, and why emulating just these network behaviours is enough to make the wired router recognisable to the Axiom.

### Raymarine mobile app

Raymarine Android 2.3.16 uses Android `NsdManager` directly. Its discovery code browses `_http._tcp`, `_raydb._tcp`, `_rtsp._tcp`, `_rym_rrc._tcp` and service enumeration.

The app classifies an onboard `_http._tcp` service as YachtSense Link when the service name contains `yachtsense-main` (and also accepts the older development name `imx8mmevk`).

After finding the router, it still discovers the Axiom directly. Screen mirroring therefore remains a direct phone-to-MFD RTSP/control connection, which is exactly why exposing the relevant discovery across your own networks works.

## Compatibility

Version **1.0.0** targets **Teltonika RUTX routers running RutOS 7**.

| Item | Support |
|---|---|
| Operating system | RutOS 7.x |
| Package architecture | `arm_cortex-a7_neon-vfpv4` / ARMv7 |
| Primary target | RUTX14 |
| RUTX models using this platform | RUTX08, RUTX09, RUTX10, RUTX11, RUTX12, RUTX14, RUTX50, RUTXR1 |

The package uses normal RutOS 7 components: UCI, `procd`, VuCI, `rpcd` ACLs and the RutOS Lua API layer.

## Installation

There is one **firmware-independent RUTX IPK** and small firmware-labelled wrappers only for the RutOS WebUI upload screen.

The program itself is not rebuilt for every RutOS patch release. Both WebUI wrappers contain the exact same IPK bytes; only the top-level `main` file's `Firmware:` value differs because RutOS' offline uploader checks that field.

Current RUTX14 releases (checked 2026-08-23):

| Channel | RutOS | Release date | WebUI wrapper |
|---|---|---:|---|
| **Stable** | `RUTX_R_00.07.24.1` | 2026-07-20 | `yachtsense-link-emulator_1.0.0-1_RUTX_00.07.24.1.tar.gz` |
| **Latest** | `RUTX_R_00.07.24.2` | 2026-08-13 | `yachtsense-link-emulator_1.0.0-1_RUTX_00.07.24.2.tar.gz` |

### RutOS Package Manager WebUI

For **System → Package Manager → Upload**, use the wrapper matching the firmware installed on the router.

Build both current wrappers with:

```sh
make package-manager-current
```

Or generate a wrapper for any specific RutOS release without rebuilding the IPK:

```sh
make package
RUTOS_FIRMWARE=RUTX_R_00.07.24.2 make package-manager
```

Because this project is built outside Teltonika's package repository, RutOS reports the upload as an **Unauthorized file**. That is the expected signature warning for a locally built package; the package contents and checksum can still validate and the UI offers **Install anyway**.

### SSH / opkg

The generic package is:

```text
tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_arm_cortex-a7_neon-vfpv4.ipk
```

It deliberately contains **no `Firmware:` field** and is the same payload used inside every WebUI wrapper.

Install it directly:

```sh
scp tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_arm_cortex-a7_neon-vfpv4.ipk root@192.168.1.1:/tmp/
ssh root@192.168.1.1
opkg install /tmp/tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_arm_cortex-a7_neon-vfpv4.ipk
```

Then open:

```text
Services → YachtSense Link Emulator
```

A clean installation starts disabled so the Axiom and app interfaces can be selected before the service is enabled.

## RutOS WebUI

The VuCI page contains:

- master enable/disable;
- YachtSense mDNS enable/disable;
- HTTP liveness service enable/disable;
- Axiom / RayNet interface;
- one or more Raymarine app interfaces;
- `Automatic`, `Force` or `Disabled` relay mode;
- optional `198.18.0.1/21` address management;
- YachtSense serial/version/hostname/service settings;
- Info/Debug logging;
- UDP/5353, Avahi and `umdns` diagnostics;
- detected Axiom and remote-client activity;
- Start, Stop, Restart and Refresh controls.

## Configuration

UCI configuration:

```text
/etc/config/yachtsense_link_emulator
```

Defaults:

```text
Service:                 disabled
YachtSense mDNS:         enabled
HTTP health:             enabled
Axiom interface:         br-lan
Raymarine app interface: br-lan
Relay mode:              auto
Managed address:         198.18.0.1/21
Serial:                  AF002A4
Product ID:              E70640
Version:                 V142.242.530
Hostname:                yachtsense-main
Service instance:        yachtsense-main Settings
Health port:             7777
mDNS TTL:                120
Log level:               info
```

Service commands:

```sh
/etc/init.d/yachtsense-link-emulator status
/etc/init.d/yachtsense-link-emulator start
/etc/init.d/yachtsense-link-emulator restart
/etc/init.d/yachtsense-link-emulator stop
logread -e yachtsense-link-emulator
```

Useful diagnostics:

```sh
ss -lunp | grep ':5353'
pidof avahi-daemon
pidof umdns
cat /var/run/yachtsense-link-emulator.runtime
```

## Build

Requirements: Go 1.22+, Bash and the usual Unix packaging tools.

```sh
make check
make package
make package-manager-current
make source
```

`make check` builds and validates the generic IPK plus the current Stable and Latest WebUI wrappers, including a byte-for-byte check that both wrappers embed the same IPK.

Main package files:

| Path | Purpose |
|---|---|
| `/usr/sbin/yachtsense-link-emulator` | Static ARMv7 mDNS/HTTP/relay daemon |
| `/etc/config/yachtsense_link_emulator` | UCI configuration |
| `/etc/init.d/yachtsense-link-emulator` | `procd` service and reflector detection |
| `/usr/lib/lua/api/services/yachtsense_link_emulator.lua` | VuCI API and diagnostics |
| `/www/views/services/YachtSenseLinkEmulator.js` | RutOS management page |

## License

[MIT](LICENSE)
