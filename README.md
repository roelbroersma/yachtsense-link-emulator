# YachtSense Link Emulator

An unofficial **Raymarine YachtSense Link emulator and Raymarine app discovery relay** for **Teltonika RUTX routers running RutOS 7**.

The package allows a Raymarine Axiom MFD to recognize a Teltonika router as a YachtSense Link-style internet source and, when the phone/tablet and Axiom are on different routed interfaces or VLANs, selectively relays the mDNS/DNS-SD discovery needed by the current Raymarine mobile app.

It includes:

- YachtSense Link mDNS/DNS-SD identity emulation;
- the Axiom HTTP connection-monitor responder on TCP `7777`;
- a Raymarine-aware mDNS relay between an **Axiom / RayNet interface** and one or more **Raymarine app interfaces**;
- detection of existing UDP/5353 listeners, Avahi and `umdns`;
- automatic avoidance of a second reflector when an Avahi reflector is already active;
- Info/Debug discovery logging including app/client queries and detected Axiom/MFD services;
- a native RutOS `procd` service and VuCI page under **Services → YachtSense Link Emulator**.

> This project is independent from Raymarine and Teltonika Networks. It contains no Raymarine firmware, APKs or proprietary source code.

## Contents

- [How does it work?](#how-does-it-work)
- [Protocol emulation](#protocol-emulation)
- [Raymarine app discovery relay](#raymarine-app-discovery-relay)
- [mDNS, DNS-SD and ordinary DNS](#mdns-dns-sd-and-ordinary-dns)
- [Existing mDNS / Avahi handling](#existing-mdns--avahi-handling)
- [Technical background](#technical-background)
- [Networking and routing](#networking-and-routing)
- [Logging and diagnostics](#logging-and-diagnostics)
- [Compatibility](#compatibility)
- [Installation](#installation)
- [Using the RutOS WebUI](#using-the-rutos-webui)
- [Configuration](#configuration)
- [Service commands](#service-commands)
- [Build from source](#build-from-source)
- [Package layout](#package-layout)
- [Scope](#scope)
- [License](#license)

## How does it work?

There are two independent discovery paths.

### 1. Axiom discovers the YachtSense Link

LightHouse does not treat every DHCP/default-gateway router as a YachtSense Link. The Axiom first discovers a YachtSense candidate on the local network and then performs a separate liveness check.

The emulator reproduces that sequence:

1. Axiom browses DNS-SD over mDNS.
2. The emulator advertises `_http._tcp.local`.
3. The TXT record contains `id=E70640 <serial>`.
4. LightHouse recognizes product ID `E70640` as YachtSense Link.
5. The service resolves to the configured RayNet-side router address.
6. The Axiom connection monitor probes TCP port `7777`.
7. The emulator returns HTTP `200 OK`.

### 2. The Raymarine mobile app discovers YachtSense and the Axiom

The current Raymarine Android app does **not** ask YachtSense Link to proxy the MFD. It performs local Android NSD/mDNS discovery itself.

The app first recognizes a YachtSense Link-style onboard device by an `_http._tcp` service whose **service name contains `yachtsense-main`**. It then discovers Axiom services directly on the network and connects directly to the Axiom IP address for screen mirroring and touch control.

That means a phone can stay on a normal Teltonika Wi-Fi network while the Axiom is on a different RayNet/routed subnet, provided:

- Raymarine mDNS discovery reaches both sides;
- normal IP routing exists between the phone and Axiom;
- firewall rules allow the required unicast MFD traffic.

## Protocol emulation

### YachtSense Link mDNS / DNS-SD identity

The daemon publishes:

```text
PTR  _http._tcp.local
     -> yachtsense-main Settings._http._tcp.local

SRV  yachtsense-main Settings._http._tcp.local
     -> yachtsense-main.local:80

TXT  id=E70640 AF002A4
TXT  model=Raymarine YachtSense Link
TXT  version=V142.242.530
TXT  mac=<MAC address of selected Teltonika interface>

A    yachtsense-main.local
     -> <interface-local router IPv4 address>
```

Clean-install identity defaults:

```text
Product ID:       E70640
Serial:           AF002A4
Hostname:         yachtsense-main
Service instance: yachtsense-main Settings
Version:          V142.242.530
```

The serial is configurable. The MAC address is read dynamically from the interface on which that advertisement is transmitted.

mDNS behavior:

- multicast group: `224.0.0.251`;
- UDP port: `5353`;
- outgoing IP TTL/hop limit: `255`;
- default DNS record TTL: `120` seconds;
- immediate and periodic unsolicited advertisements;
- replies to service, instance, host and DNS-SD enumeration queries;
- zero-TTL goodbye advertisements during clean shutdown.

### HTTP connection monitor

The HTTP liveness service binds to the configured **Axiom / RayNet-side router address** and returns:

```http
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Cache-Control: no-store

OK
```

Default port: `7777/TCP`.

The `_http._tcp` SRV record still advertises port `80`, matching the real YachtSense discovery metadata. The port-7777 liveness check is separate.

## Raymarine app discovery relay

### Service types found in Raymarine Android 2.3.16

Reverse engineering of the current Android app showed these Android NSD service types:

| Service | Use in the app |
|---|---|
| `_http._tcp.local` | Onboard devices including YachtSense Link |
| `_rtsp._tcp.local` | MFD screen/video discovery |
| `_rym_rrc._tcp.local` | Raymarine remote-control discovery |
| `_raydb._tcp.local` | Raymarine database/device discovery |
| `_services._dns-sd._udp.local` | DNS-SD service enumeration |

The built-in reflector only forwards those Raymarine-relevant browses and **learned follow-up names** associated with their DNS-SD resolution.

It does not intentionally reflect arbitrary Bonjour/mDNS services such as printers, smart-home devices or other multicast discovery protocols.

### Relay direction

The relay uses a star topology:

```text
Raymarine app network                    Axiom / RayNet network
(br-lan, Wi-Fi, VLAN, ...)               (br-raynet, VLAN, Ethernet, ...)

mDNS query
_http / _rtsp / _rym / _raydb  ───────►
                                          Axiom
                                  ◄──────  mDNS response
                                           PTR / SRV / TXT / A / AAAA
```

Queries flow **from each configured app interface to the Axiom interface**. Relevant responses and unsolicited Raymarine service announcements flow **from the Axiom interface to each configured app interface**.

The relay does not copy app-network mDNS traffic between app interfaces and does not act as a generic multicast bridge.

### Follow-up DNS-SD resolution

A discovery is more than the first PTR query. For example:

```text
PTR  _rtsp._tcp.local
     -> Axiom 7._rtsp._tcp.local

SRV  Axiom 7._rtsp._tcp.local
     -> axiom-123.local:8554

TXT  ...
A    axiom-123.local
     -> 198.18.0.23
```

Android NSD may subsequently query the instance or host name. The daemon therefore learns Raymarine instance and SRV target names from Axiom responses for a limited period and permits only those related follow-up queries through the relay.

### Same-interface case

If the Axiom role and the phone/tablet role use the same Linux bridge/interface, no reflector is needed. mDNS is already in the same layer-2 broadcast domain even when the devices use different IPv4 subnets.

Example:

```text
br-lan
  router 192.168.40.1/24
  router 198.18.0.1/21

phone  192.168.40.50/24
Axiom  198.18.0.23/21
```

The phone can hear Axiom mDNS directly at layer 2. Its later unicast connection to `198.18.0.23` goes through its default gateway and is routed by the RUTX back onto the same bridge.

In this situation **Automatic** relay mode reports that reflection is unnecessary and stays inactive.

## mDNS, DNS-SD and ordinary DNS

Raymarine device discovery here is **not ordinary DNS on port 53**.

DNS-SD uses normal DNS record formats such as:

- PTR;
- SRV;
- TXT;
- A;
- AAAA.

But these discovery records are exchanged using **mDNS on UDP port 5353** to multicast group `224.0.0.251`.

Ordinary DNS on UDP/TCP port `53` remains normal RutOS DNS/internet name resolution and is not modified or reflected by this package.

After discovery, the phone uses ordinary **unicast IP traffic** directly to the Axiom.

Known current app endpoints include:

```text
rtsp://<Axiom-IP>:8554/RAYMARINEMFD
```

and remote-control related ports:

| Port | Role observed in current app |
|---|---|
| `8554/TCP` | RTSP / view-only screen stream |
| `50000/TCP` | Touch-capable remote-control connection |
| `49111` | `DISABLE_PORT` state used by MFD discovery/control logic |

The emulator does not proxy these TCP streams.

## Existing mDNS / Avahi handling

A process listening on UDP/5353 is **not automatically an mDNS reflector**.

For example:

- `avahi-daemon` can publish/listen locally with reflection disabled;
- `umdns` normally provides local mDNS/DNS-SD behavior;
- another application may also bind UDP/5353 using socket reuse.

The WebUI therefore reports these separately:

```text
UDP/5353 listener detected
Avahi running
Avahi reflector active / inactive
umdns running / not detected
listener process/socket lines
```

### Automatic relay mode

`Automatic` is the recommended default:

1. If Axiom and app roles are on the same interface, built-in reflection stays off.
2. If they are on different interfaces and no existing reflector is detected, the built-in Raymarine relay starts.
3. If a running `avahi-daemon` has `enable-reflector=yes`, the built-in relay does **not** start a second reflector.
4. A normal Avahi responder, `umdns`, or an unknown UDP/5353 listener is reported but is not automatically treated as a reflector.
5. The daemon uses `SO_REUSEADDR`/`SO_REUSEPORT` and attempts to coexist with other well-behaved mDNS responders.

When an Avahi reflector is active in Automatic mode, the emulator advertises YachtSense on the Axiom side and relies on the existing reflector for cross-interface mDNS. This avoids duplicate YachtSense instances on the remote network.

### Force mode

`Force built-in relay` exists for diagnostics. It starts the internal relay even when Avahi reflection is detected and logs a warning. Running two reflectors across the same networks can create duplicate packets or loops, so Force should not be the normal setting.

### Disabled mode

`Disabled` turns off cross-interface reflection. Direct YachtSense advertisement can still be enabled independently.

## Technical background

This project came from interoperability research into **Raymarine Axiom / LightHouse 4**, **YachtSense Link firmware**, and the current **Raymarine Android app**.

### Axiom-side findings

Analysis of Axiom ARMv7 LightHouse libraries identified YachtSense-specific service discovery. LightHouse browses `_http._tcp`, reads the DNS-SD TXT `id`, and recognizes the product identifier before the serial suffix. YachtSense Link uses:

```text
E70640
```

The real YachtSense Link firmware advertises an HTTP DNS-SD service with values equivalent to:

```text
hostname: yachtsense-main
service:  yachtsense-main Settings._http._tcp
TXT id:   E70640 <serial>
model:    Raymarine YachtSense Link
```

A separate Axiom component probes port `7777` for connection/liveness status.

### Mobile-app findings

Raymarine Android 2.3.16 uses Android `NsdManager` and mDNS directly. Its discovery code creates search executors for `_http._tcp`, `_raydb._tcp`, `_rtsp._tcp`, `_rym_rrc._tcp` and DNS-SD service enumeration.

The app classifies an onboard `_http._tcp` service as YachtSense Link when the service name contains `yachtsense-main` or the legacy development name `imx8mmevk`.

When a YachtSense Link is found, the app records that a 4G router is present. It still discovers MFD remote-view/control services directly and later connects to the MFD IP rather than tunneling the screen through YachtSense Link.

The repository contains only the independent implementation derived from those observable protocol behaviors. It does not redistribute decompiled application code.

## Networking and routing

### Axiom / RayNet interface

The package has one explicit **Axiom / RayNet interface**. This can be a bridge, Ethernet device or VLAN such as:

```text
br-raynet
eth0.20
br-lan
```

Default managed address:

```text
198.18.0.1/21
```

If **Manage the RayNet IPv4 address** is enabled, the package adds that address as a secondary address. It records only addresses it added itself and removes only its own managed address.

### Raymarine app interfaces

One or more app-side interfaces can be selected, for example:

```text
br-lan
guest
boat-wifi
```

These are the networks on which phones/tablets running the Raymarine app are expected.

### Unicast routing is still required

mDNS reflection only solves discovery. The phone still connects directly to the Axiom address afterward.

The router therefore needs normal routing/firewall policy between the app network and RayNet, including the relevant Axiom ports. Depending on the installation this can require:

- an IP route/direct-connected RayNet interface;
- firewall forwarding from app-side zone to RayNet-side zone;
- return routing from the Axiom side;
- NAT only if the chosen network design genuinely requires it.

The package intentionally does **not** rewrite DHCP, ordinary DNS, firewall zones, forwarding or NAT because those policies are installation-specific.

### Internet access for Axiom

YachtSense emulation also does not itself select the RUTX WAN. Actual Axiom internet traffic uses normal RutOS routing, failover, firewall and NAT configuration.

## Logging and diagnostics

The daemon has two log levels.

### Info

Designed for normal use. Example events:

```text
YachtSense advertisement active interface=br-raynet address=198.18.0.1 ...
Raymarine app/client query detected source=192.168.40.54 interface=br-lan service=_rtsp._tcp.local
Axiom/MFD detected name="Axiom 7" address=198.18.0.23 service=_rtsp._tcp.local port=8554
Axiom remote-control service detected name="Axiom 7" address=198.18.0.23 port=50000
Avahi reflector detected; Automatic mode will not start a second reflector
```

Repeated client browse logs are rate-limited so normal mDNS chatter does not fill `logread`.

### Debug

Adds individual relay direction, packet size, follow-up learned host/instance queries, YachtSense query replies and HTTP health requests.

### Runtime status shown in VuCI

The status page exposes:

- relay active/inactive and reason;
- last Axiom/MFD name and IP;
- last discovered Axiom service and port;
- last Raymarine app/client source IP and interface;
- last requested service;
- last activity time;
- UDP/5353 listeners;
- Avahi running/reflector state;
- `umdns` state;
- recent service logs.

The log uses **`Raymarine app/client`** rather than claiming every source is definitely a phone. At the mDNS layer the daemon can know the source IP/interface and requested Raymarine service, but not the physical device type with certainty.

## Compatibility

Version **1.0.0** targets the Teltonika **RUTX / RutOS 7** platform.

| Item | Support |
|---|---|
| Operating system | **RutOS 7.x** |
| Package target | **`ipq40xx` / ARMv7** |
| Primary development target | **RUTX14** |
| Expected RUTX models | RUTX08, RUTX09, RUTX10, RUTX11, RUTX12, RUTX14, RUTX50, RUTXR1 |
| RutOS 6 | Not supported |
| RUTM / RUTC / OTD / ARM64 targets | Not included in v1.0.0 |

The package uses RutOS 7 components including VuCI, Lua `FunctionService`, `rpcd` ACLs, UCI and `procd`. The exact earliest compatible RutOS 7 maintenance release has not been qualified; use a current RutOS 7 release.

Official Teltonika references:

- [Product firmware updates](https://wiki.teltonika-networks.com/view/Product_Firmware_Updates)
- [RUTX14 package downloads](https://wiki.teltonika-networks.com/view/RUTX14_Package_Downloads)
- [RutOS SDK instructions](https://wiki.teltonika-networks.com/view/RUTOS_Software_Development_Kit_%28SDK%29_Instruction)

## Installation

Install the release IPK through **System → Package Manager → Upload**, or use SSH:

```sh
scp tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_ipq40xx.ipk root@192.168.1.1:/tmp/
ssh root@192.168.1.1
opkg install /tmp/tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_ipq40xx.ipk
```

The service is intentionally **disabled after a clean installation**. Refresh or reopen VuCI after installation.

## Using the RutOS WebUI

Open:

```text
Services → YachtSense Link Emulator
```

The page provides:

- master enable/disable;
- YachtSense mDNS enable/disable;
- HTTP health-service enable/disable;
- Axiom / RayNet interface selector;
- one or more Raymarine app-side interface selectors;
- `Automatic`, `Force` or `Disabled` discovery relay mode;
- optional RayNet IPv4 address management;
- serial, advertised version, hostname and service-instance settings;
- Info/Debug log level;
- UDP/5353 / Avahi / `umdns` diagnostics;
- Axiom and remote-client last-seen status;
- **Start**, **Stop**, **Restart** and **Refresh** controls;
- recent `logread` lines.

## Configuration

Configuration is stored in:

```text
/etc/config/yachtsense_link_emulator
```

Clean-install defaults:

```text
Service:                disabled
YachtSense mDNS:        enabled
HTTP health:            enabled
Axiom interface:        br-lan
Raymarine app interface: br-lan
Relay mode:             auto
Managed address:        198.18.0.1/21
Serial:                 AF002A4
Product ID:             E70640 (fixed)
Version:                V142.242.530
Hostname:               yachtsense-main
Service instance:       yachtsense-main Settings
Health port:            7777
mDNS TTL:               120 seconds
Log level:              info
```

There is deliberately **no importer for earlier experimental configuration formats**. Version 1 starts with the clean configuration above.

## Service commands

```sh
/etc/init.d/yachtsense-link-emulator status
/etc/init.d/yachtsense-link-emulator start
/etc/init.d/yachtsense-link-emulator restart
/etc/init.d/yachtsense-link-emulator stop
logread -e yachtsense-link-emulator
```

Useful manual diagnostics:

```sh
ss -lunp | grep ':5353'
pidof avahi-daemon
pidof umdns
cat /var/run/yachtsense-link-emulator.runtime
```

## Build from source

Requirements:

- Go 1.22 or newer;
- Bash, `tar`, `gzip`, `ar`, `jq` and standard Unix tools;
- optional `node` and `luac` for extra syntax checks.

Validate source, tests, WebUI syntax and the actual IPK:

```sh
make check
```

Build the IPK:

```sh
make package
```

Build the source archive:

```sh
make source
```

Generated files are written to `dist/`.

## Package layout

| Path | Purpose |
|---|---|
| `/usr/sbin/yachtsense-link-emulator` | Static ARMv7 mDNS/HTTP/relay daemon |
| `/etc/config/yachtsense_link_emulator` | UCI configuration |
| `/etc/init.d/yachtsense-link-emulator` | `procd` service and Avahi reflector detection |
| `/usr/lib/lua/api/services/yachtsense_link_emulator.lua` | RutOS API, status and mDNS environment inspection |
| `/www/views/services/YachtSenseLinkEmulator.js` | VuCI management/diagnostics page |
| `/usr/share/rpcd/acl.d/yachtsense-link-emulator.json` | API/UCI ACL |
| `/usr/share/vuci/menu.d/yachtsense-link-emulator.json` | Services menu registration |
| `/usr/share/vuci/path.d/yachtsense-link-emulator.json` | API path mapping |

Repository layout:

```text
cmd/yachtsense-link-emulator/     Native daemon and tests
package/root/                     Files installed on the router
package/control/                  IPK metadata and lifecycle scripts
scripts/                          Build and validation tools
.github/workflows/                Reproducible CI build
```

All source files contain explanatory English comments. Strict JSON descriptor files are the exception because JSON does not support comments.

## Scope

Implemented in v1:

- YachtSense Link-style mDNS/DNS-SD discovery;
- `E70640 <serial>` identity and dynamic interface MAC;
- HTTP `200 OK` connection-monitor endpoint;
- Raymarine-selective cross-interface mDNS relay;
- learned DNS-SD instance/host follow-up forwarding;
- same-interface no-relay detection;
- Avahi reflector conflict avoidance;
- UDP/5353, Avahi and `umdns` diagnostics;
- Axiom/MFD and remote-client discovery logs;
- optional RayNet-side address management;
- RutOS service/API/VuCI management.

Not implemented:

- proxying of RTSP video or touch/control TCP connections;
- automatic firewall/zone/NAT configuration;
- YachtSense Link JSON-RPC administration API;
- HTTPS administration certificates;
- Raymarine cloud connector;
- YachtSense onboard WebSocket/data service on port `7778`;
- GNSS service on port `9999`;
- NMEA 2000 / SeaTalkNG behavior;
- digital I/O;
- modem, Wi-Fi uplink or data-usage reporting;
- YachtSense firmware-update services.

Raymarine, Axiom, YachtSense and YachtSense Link are trademarks of their respective owner. This project is independent and is not endorsed by Raymarine or Teltonika Networks.

## License

[MIT](LICENSE)
