# YachtSense Link Emulator

An unofficial **Raymarine YachtSense Link discovery emulator** for **Teltonika RUTX routers running RutOS 7**.

It lets a Raymarine Axiom MFD discover a Teltonika router as a YachtSense Link-style internet source by emulating the small part of the local-network behavior that LightHouse checks: **mDNS/DNS-SD discovery** plus the **HTTP connection-monitor service on TCP port 7777**.

The package includes a native RutOS `procd` service and a VuCI page under **Services → YachtSense Link Emulator** with separate mDNS and HTTP switches, interface selection, start/stop/restart controls, runtime status and recent log lines.

> This project is independent from Raymarine and Teltonika Networks. It contains no Raymarine firmware or proprietary source code.

## Contents

- [How does it work?](#how-does-it-work)
- [Protocol emulation](#protocol-emulation)
- [Technical background](#technical-background)
- [Networking](#networking)
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

LightHouse does not treat every ordinary DHCP router as a YachtSense Link. The Axiom first discovers a YachtSense Link candidate on the local Ethernet/RayNet segment and then performs a separate liveness check.

The emulator reproduces that sequence:

1. The Axiom browses DNS-SD services over mDNS.
2. The emulator advertises `_http._tcp.local`.
3. The Axiom reads the service TXT record.
4. The TXT `id` starts with the YachtSense Link product identifier **`E70640`**.
5. The Axiom resolves the advertised `.local` hostname to the configured IPv4 address.
6. The Axiom connection monitor requests the candidate on **TCP port `7777`**.
7. The emulator returns an HTTP **`200 OK`** response.

This is intentionally much smaller than emulating a complete YachtSense Link router.

## Protocol emulation

### mDNS / DNS-SD

The daemon publishes an `_http._tcp.local` DNS-SD service with these records:

```text
PTR  _http._tcp.local -> <service instance>
SRV  <service instance> -> <hostname>.local:80
TXT  id=E70640 AF002A4
TXT  model=Raymarine YachtSense Link
TXT  version=V142.242.530
TXT  mac=<MAC address of selected interface>
A    <hostname>.local -> <configured IPv4 address>
```

The shipped default serial is **`AF002A4`**. The serial is configurable; the MAC address is always taken dynamically from the selected Teltonika interface.

mDNS behavior:

- multicast address: `224.0.0.251`;
- UDP port: `5353`;
- outgoing IP TTL: `255`;
- default DNS record TTL: `120` seconds;
- immediate and periodic unsolicited announcements;
- replies to relevant service, instance, hostname and service-enumeration queries;
- a zero-TTL goodbye announcement during a clean shutdown.

The SRV record advertises port `80` because that is part of the discovery metadata. The Axiom liveness check is a separate service on port `7777`.

### HTTP connection monitor

When enabled, the HTTP component binds only to the configured YachtSense/RayNet-side IPv4 address and health port. Every path returns a successful response:

```http
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Cache-Control: no-store

OK
```

The default port is `7777`.

## Technical background

This project came from interoperability research into the **Raymarine Axiom / LightHouse 4** firmware and YachtSense Link network behavior.

Analysis of the Axiom ARMv7 libraries showed YachtSense-specific discovery logic in the LightHouse system functions. The relevant discovery path browses `_http._tcp` services, reads the DNS-SD TXT `id` value and uses the product identifier before the serial suffix to recognize a YachtSense Link. For the YachtSense Link this identifier is **`E70640`**.

A second LightHouse component performs the internet-source/liveness check over HTTP on **TCP port `7777`**. That explains why only supplying DHCP gateway and DNS information from a normal third-party router is not sufficient to reproduce the YachtSense Link behavior.

The emulator therefore focuses on the two behaviors that matter for this recognition path:

- `_http._tcp.local` discovery with `id=E70640 <serial>`;
- a successful HTTP response on port `7777`.

The project does **not** copy or redistribute the Raymarine implementation; it independently reproduces the observed network protocol behavior.

## Networking

### Layer 2

mDNS is link-local multicast. The selected Teltonika interface and the Axiom must therefore be in the **same Ethernet broadcast domain**. A bridge such as `br-lan` is suitable when the Axiom Ethernet/RayNet connection is part of that bridge.

### Managed RayNet-side address

The default address is:

```text
198.18.0.1/21
```

The package can add this as a secondary IPv4 address without replacing the router's normal LAN address.

Address handling is conservative:

1. If the address already exists, it is left untouched.
2. If the package adds the address, it records that fact in `/var/run`.
3. On stop it removes only an address that this package added itself.
4. On restart the previous package-managed address is removed before changed settings are applied.

Disable **Manage the IPv4 address** if you configure the address elsewhere in RutOS.

### Internet routing

The emulator supplies discovery and liveness behavior only. Actual internet access still uses normal RutOS networking:

- a working WAN/default route on the Teltonika router;
- forwarding from the Axiom-facing interface to the desired WAN;
- source NAT/masquerading where required;
- working DNS;
- appropriate firewall forwarding rules;
- DHCP only where it is part of your chosen RayNet design.

The package intentionally does not change DHCP, DNS, routing, firewall or NAT configuration.

### Ports

| Protocol | Address / port | Purpose |
|---|---|---|
| mDNS | `224.0.0.251:5353/UDP` | YachtSense Link DNS-SD discovery |
| HTTP | `<configured IP>:7777/TCP` | Axiom connection monitor |
| DNS-SD SRV value | TCP `80` | Discovery metadata only |

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

The package uses RutOS 7 components including VuCI, Lua `FunctionService`, `rpcd` ACLs, UCI and `procd`. RutOS 6 used a different WebUI architecture and is not targeted.

The exact earliest compatible RutOS 7 maintenance release has not been qualified; use a current RutOS 7 release. RUTX14 is the primary validation target.

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
- separate **mDNS** enable/disable;
- separate **HTTP health service** enable/disable;
- Axiom/RayNet interface selector;
- optional automatic IPv4 address management;
- serial, advertised version, hostname and service-instance settings;
- mDNS TTL and HTTP health-port settings;
- **Start**, **Stop**, **Restart** and **Refresh** controls;
- runtime state, detected interface/address state and health URL;
- recent `logread` lines for the service.

## Configuration

Configuration is stored in:

```text
/etc/config/yachtsense_link_emulator
```

Clean-install defaults:

```text
Service:          disabled
mDNS:             enabled
HTTP health:      enabled
Interface:        br-lan
Managed address:  198.18.0.1/21
Serial:           AF002A4
Product ID:       E70640 (fixed)
Version:          V142.242.530
Hostname:         yachtsense-link
Service instance: YachtSense Link Settings
Health port:      7777
mDNS TTL:         120 seconds
```

There is deliberately **no migration/import code** for earlier experimental versions or package names.

## Service commands

```sh
/etc/init.d/yachtsense-link-emulator status
/etc/init.d/yachtsense-link-emulator start
/etc/init.d/yachtsense-link-emulator restart
/etc/init.d/yachtsense-link-emulator stop
logread -e yachtsense-link-emulator
```

## Build from source

Requirements:

- Go 1.22 or newer;
- Bash, `tar`, `gzip`, `ar`, `jq` and standard Unix tools;
- optional `node` and `luac` for extra syntax checks.

Validate everything:

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

The IPK installs these main components:

| Path | Purpose |
|---|---|
| `/usr/sbin/yachtsense-link-emulator` | Static ARMv7 mDNS/HTTP daemon |
| `/etc/config/yachtsense_link_emulator` | UCI configuration |
| `/etc/init.d/yachtsense-link-emulator` | `procd` service |
| `/usr/lib/lua/api/services/yachtsense_link_emulator.lua` | RutOS API service |
| `/www/views/services/YachtSenseLinkEmulator.js` | VuCI management page |
| `/usr/share/rpcd/acl.d/yachtsense-link-emulator.json` | API/UCI ACL |
| `/usr/share/vuci/menu.d/yachtsense-link-emulator.json` | Services menu registration |
| `/usr/share/vuci/path.d/yachtsense-link-emulator.json` | API path mapping |

Repository layout:

```text
cmd/yachtsense-link-emulator/     Native mDNS and HTTP daemon
package/root/                     Files installed on the router
package/control/                  IPK metadata and lifecycle scripts
scripts/                          Build and validation tools
.github/workflows/                Reproducible CI build
```

Source files contain explanatory English comments. Strict JSON descriptor files are the exception because JSON does not support comments.

## Scope

Implemented:

- YachtSense Link-style mDNS/DNS-SD discovery;
- `E70640 <serial>` identity;
- interface MAC advertisement;
- HTTP `200 OK` connection-monitor endpoint;
- optional RayNet-side address management;
- RutOS service/API/VuCI management.

Not implemented:

- YachtSense Link JSON-RPC administration API;
- HTTPS administration certificates;
- Raymarine cloud connector;
- GNSS endpoints on ports `7778` or `9999`;
- NMEA 2000 / SeaTalkNG behavior;
- digital I/O;
- modem, Wi-Fi uplink or data-usage reporting;
- YachtSense firmware-update services.

Raymarine, Axiom, YachtSense and YachtSense Link are trademarks of their respective owner. This project is independent and is not endorsed by Raymarine or Teltonika Networks.

## License

[MIT](LICENSE)
