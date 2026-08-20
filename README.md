# YachtSense Link Emulator

An unofficial **Raymarine YachtSense Link discovery emulator** packaged for
**Teltonika RUTX routers running RutOS 7**.

The package allows a Raymarine Axiom chartplotter on the same Ethernet/RayNet
segment to discover a Teltonika router as a YachtSense Link-style internet
source. It provides:

- the mDNS/DNS-SD identity expected by Axiom;
- the HTTP connection-monitor endpoint expected on TCP port `7777`;
- optional management of the RayNet-side IPv4 address;
- a native RutOS `procd` service;
- a VuCI page under **Services → YachtSense Link Emulator**;
- independent mDNS and HTTP switches, interface selection, start/stop/restart,
  live status and recent log lines.

The project does **not** contain Raymarine firmware or proprietary source code.
It implements only the small network behavior observed during interoperability
research.

## Compatibility

Version **1.0.0** intentionally targets one Teltonika platform:

| Item | Support |
|---|---|
| Operating system | **RutOS 7.x only** |
| Package architecture | **`ipq40xx` / ARMv7** |
| Primary development target | **RUTX14** |
| Expected RUTX models | RUTX08, RUTX09, RUTX10, RUTX11, RUTX12, RUTX14, RUTX50, RUTXR1 |
| RutOS 6 | Not supported |
| RUTM / RUTC / OTD / newer ARM64 platforms | Not included in v1.0.0 |

The prebuilt IPK follows the RUTX package platform used by Teltonika. The exact
earliest RutOS 7 maintenance release has not been qualified; use a current
RutOS 7 release. Teltonika's current RUTX firmware and package lists are
available from its official wiki:

- [Product firmware updates](https://wiki.teltonika-networks.com/view/Product_Firmware_Updates)
- [RUTX14 package downloads](https://wiki.teltonika-networks.com/view/RUTX14_Package_Downloads)
- [RutOS SDK instructions](https://wiki.teltonika-networks.com/view/RUTOS_Software_Development_Kit_%28SDK%29_Instruction)

See [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) for the support boundary.

## Installation

Download the release IPK and install it through **System → Package Manager**, or
copy it to the router and use `opkg`:

```sh
scp tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_ipq40xx.ipk root@192.168.1.1:/tmp/
ssh root@192.168.1.1
opkg install /tmp/tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_ipq40xx.ipk
```

The package is disabled after a clean installation. Reopen or refresh VuCI and
go to:

```text
Services → YachtSense Link Emulator
```

Select the interface connected to the Axiom, review the address, enable the
service, then choose **Save & apply**.

## Default network settings

```text
Interface:       br-lan
Managed address: 198.18.0.1/21
mDNS:            enabled
HTTP health:     enabled on TCP 7777
Service:         disabled until configured
```

When **Manage the IPv4 address** is enabled, the package adds the configured
address as a secondary address. It records only an address it added itself and
therefore does not remove a pre-existing user-owned address.

The emulator does not create DHCP, DNS, routing, firewall or NAT rules. Configure
those separately in RutOS for the desired internet path. See
[docs/NETWORKING.md](docs/NETWORKING.md).

## Emulated discovery identity

The daemon publishes an `_http._tcp.local` DNS-SD service with records matching
the identity checked by Axiom:

```text
id=E70640 RUTX001
model=Raymarine YachtSense Link
version=V142.242.530
mac=<MAC address of selected interface>
```

It also returns HTTP status `200` on the configured address and port `7777`.
The product prefix `E70640` and the successful HTTP response are the relevant
recognition markers; the serial suffix and display labels are configurable.

Technical details are in [docs/PROTOCOL.md](docs/PROTOCOL.md).

## Build from source

Requirements on the build computer:

- Go 1.22 or newer;
- Bash, `tar`, `gzip`, `ar`, `jq` and standard Unix tools;
- optional `node` and `luac` for additional syntax checks.

Build and validate:

```sh
make check
```

Build only the IPK:

```sh
make package
```

Build a source archive:

```sh
make source
```

Generated files are written to `dist/`.

## Repository layout

```text
cmd/yachtsense-link-emulator/     Native mDNS and HTTP daemon
package/root/                     Files installed on the router
package/control/                  IPK metadata and lifecycle scripts
scripts/                          Build and validation tools
docs/                             Protocol, networking and compatibility notes
.github/workflows/                Reproducible CI build
```

Source files contain explanatory comments. JSON descriptor files are the only
exception because standard JSON has no comment syntax; their keys and filenames
are kept self-describing instead.

## Service commands

```sh
/etc/init.d/yachtsense-link-emulator status
/etc/init.d/yachtsense-link-emulator start
/etc/init.d/yachtsense-link-emulator restart
/etc/init.d/yachtsense-link-emulator stop
logread -e yachtsense-link-emulator
```

Configuration is stored in:

```text
/etc/config/yachtsense_link_emulator
```

## Scope

This project emulates discovery and the basic internet-source liveness check. It
does not implement the YachtSense Link administration API, cloud service,
NMEA 2000 gateway, GNSS service, digital I/O or firmware-update functions.

Raymarine, Axiom, YachtSense and YachtSense Link are trademarks of their
respective owner. This project is independent and is not endorsed by Raymarine
or Teltonika Networks.

## License

[MIT](LICENSE)
