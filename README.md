# YachtSense Link Emulator

**YachtSense Link Emulator** makes a Teltonika RUTX behave like a Raymarine YachtSense Link on the network.

That gives an Axiom two useful things immediately:

- **Internet over its existing Ethernet / RayNet connection.** The MFD no longer has to join Wi-Fi just to get online while it is already cabled to the router.
- **Raymarine app discovery from your own Wi-Fi.** The phone can stay on the normal boat WLAN while the Axiom remains on its wired RayNet subnet.

The emulator publishes the YachtSense identity LightHouse expects, answers the YachtSense liveness check, and can selectively relay the Raymarine mDNS services used by the current mobile app.

```text
                       Teltonika RUTX
                    ┌───────────────────┐
Internet / 4G / 5G ─┤                   ├── Wi-Fi ── Phone / tablet
                    │  YachtSense Link  │             192.168.40.x
                    │     Emulator      │
                    │                   ├── RayNet ── Axiom
                    └───────────────────┘             198.18.x.x
```

With suitable routing the same idea can also be carried over a VPN. VXLAN over WireGuard/IPsec is particularly handy when you want to extend the layer-2 multicast domain; a routed VPN can also work when the relay is bound to the VPN interface and the Axiom subnet is reachable.

## How it works

### Axiom side

LightHouse does not treat every DHCP/default-gateway router as YachtSense Link. It first discovers a YachtSense service over mDNS/DNS-SD and then performs a separate liveness check.

The emulator publishes:

```text
service:  yachtsense-main Settings._http._tcp.local
hostname: yachtsense-main.local

TXT id=E70640 AF002A4
TXT model=Raymarine YachtSense Link
TXT version=V142.242.530
TXT mac=<MAC of selected RUTX interface>
```

The important product identifier is `E70640`. After resolving the service, LightHouse also checks TCP `7777`; the emulator answers with HTTP `200 OK`.

Once LightHouse accepts the RUTX as YachtSense Link, the Axiom can use its existing Ethernet/RayNet path for normal routed internet traffic. Which WAN ultimately carries that traffic is just ordinary RutOS policy: mobile WAN, Wi-Fi WAN, Ethernet WAN, failover, load balancing, and so on.

### Raymarine app side

The current Raymarine Android app uses Android NSD/mDNS directly. It first recognises the YachtSense router and then discovers the MFD itself.

The app browses these service types:

```text
_http._tcp.local
_rtsp._tcp.local
_rym_rrc._tcp.local
_raydb._tcp.local
_services._dns-sd._udp.local
```

The emulator can relay those Raymarine discovery packets between one **Axiom / RayNet interface** and one or more **Raymarine app interfaces**.

A discovery is not only a PTR record. Android NSD follows the service through `SRV`, `TXT`, `A` and `AAAA`, so the daemon learns the related instance and host names and forwards those follow-up lookups as well.

After discovery, the phone connects directly to the Axiom. The emulator is not in the video path.

Current app endpoints include:

```text
rtsp://<Axiom-IP>:8554/RAYMARINEMFD
```

with remote-control related traffic on TCP `50000` and the app's `49111` control/discovery state.

## mDNS relay

Raymarine discovery here is mDNS on:

```text
224.0.0.251 / UDP 5353
```

It is not ordinary DNS on port 53.

When the phone and Axiom already share the same Linux bridge, no reflection is needed even if they use different IP subnets. In **Automatic** mode the daemon notices that and leaves the relay off.

When they are on different interfaces/VLANs, the built-in relay forwards only the Raymarine-relevant discovery and learned follow-up names.

### Existing Avahi / mDNS

A process listening on UDP/5353 does not automatically mean a reflector is active. The WebUI distinguishes between:

- a 5353 listener;
- `avahi-daemon`;
- Avahi with `enable-reflector=yes`;
- `umdns`;
- an unknown listener.

In **Automatic** mode an existing Avahi reflector wins and the built-in reflector stays off, avoiding duplicate reflection loops. A normal Avahi responder or `umdns` is only reported; it is not assumed to be a reflector.

## Logging

The service keeps discovery visible in the logs. Typical Info messages look like:

```text
YachtSense advertisement active interface=br-raynet address=198.18.0.1
Raymarine app/client query detected source=192.168.40.54 interface=br-lan service=_rtsp._tcp.local
Axiom/MFD detected name="Axiom 7" address=198.18.0.23 service=_rtsp._tcp.local port=8554
Axiom remote-control service detected address=198.18.0.23 port=50000
Avahi reflector detected; Automatic mode will not start a second reflector
```

Debug mode adds packet direction, packet size, learned service/host names, YachtSense replies and HTTP liveness requests.

## Networking example

A common setup is:

```text
RUTX br-lan:      192.168.40.1/24
RUTX RayNet side: 198.18.0.1/21
Phone:            192.168.40.50/24
Axiom:            198.18.0.23/21
```

The phone discovers the MFD through mDNS relay and then sends normal unicast traffic to `198.18.0.23`. The RUTX routes that traffic to RayNet.

If both subnets live on the same bridge, mDNS is already visible directly at layer 2 and the relay is unnecessary.

## RutOS WebUI

After installation open:

```text
Services → YachtSense Link Emulator
```

The page provides the Axiom/RayNet interface, one or more app-side interfaces, YachtSense identity settings, optional `198.18.0.1/21` address management, relay mode, Info/Debug logging, 5353/Avahi/umdns diagnostics, last detected Axiom and last app/client query, plus Start/Stop/Restart controls.

A clean install starts disabled so the correct interfaces can be selected first.

## Installation

There are deliberately two packaging layers.

### 1. Generic RUTX IPK

The actual software is built once as:

```text
tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_arm_cortex-a7_neon-vfpv4.ipk
```

That IPK is **not tied to a RutOS patch release**. It is the same payload whether the router runs 7.24.1 or 7.24.2.

Install it directly over SSH with:

```sh
opkg install /tmp/tlt_custom_pkg_yachtsense-link-emulator_1.0.0-1_arm_cortex-a7_neon-vfpv4.ipk
```

### 2. Package Manager WebUI wrappers

`System → Package Manager → Upload` expects Teltonika's `.tar.gz` package container rather than a loose IPK. RutOS also checks the `Firmware:` value in that wrapper against the running firmware release.

The wrapper is therefore firmware-specific, but **the IPK inside it is not**. Only the tiny `main` metadata file differs.

Current RUTX14 releases, checked 2026-08-23:

| Channel | RutOS | Release date | WebUI bundle |
|---|---|---|---|
| Stable | `RUTX_R_00.07.24.1` | 2026-07-20 | `yachtsense-link-emulator_1.0.0-1_RUTX_00.07.24.1.tar.gz` |
| Latest | `RUTX_R_00.07.24.2` | 2026-08-13 | `yachtsense-link-emulator_1.0.0-1_RUTX_00.07.24.2.tar.gz` |

Teltonika's current RUTX14 firmware list is here: https://wiki.teltonika-networks.com/view/RUTX14_Firmware_Downloads

Both wrapper archives contain the **same byte-for-byte generic IPK**. CI verifies that explicitly.

Because these packages are built outside Teltonika's repository they do not carry a Teltonika digital signature. RutOS can therefore show the verification screen as **Unauthorized**; Teltonika's Package Manager documentation explicitly allows proceeding with an unsigned uploaded package after that warning.

For another RutOS release, build only a new wrapper:

```sh
RUTOS_FIRMWARE=RUTX_R_00.07.24.2 make package-manager
```

To build the current Stable + Latest pair in one go:

```sh
make package-manager-current
```

## Build

Requirements are Go 1.22+, Bash and standard Unix packaging tools.

```sh
make check
make package
make package-manager-current
make source
```

`make check` runs the Go tests and syntax checks, builds the generic RUTX IPK, builds both current Package Manager wrappers and verifies that the IPK embedded in Stable and Latest is byte-identical.

Generated files are placed in `dist/`.

## Default configuration

```text
Service:                  disabled
YachtSense mDNS:          enabled
HTTP health:              enabled
Axiom interface:          br-lan
Raymarine app interface:  br-lan
Relay mode:               auto
Managed address:          198.18.0.1/21
Serial:                   AF002A4
Product ID:               E70640
Version:                  V142.242.530
Hostname:                 yachtsense-main
Service instance:         yachtsense-main Settings
Health port:              7777
mDNS TTL:                 120
Log level:                info
```

UCI configuration lives in:

```text
/etc/config/yachtsense_link_emulator
```

Useful commands:

```sh
/etc/init.d/yachtsense-link-emulator status
/etc/init.d/yachtsense-link-emulator restart
logread -e yachtsense-link-emulator
ss -lunp | grep ':5353'
```

## Technical background

The implementation is based on protocol behaviour observed in current Raymarine components:

- LightHouse recognises YachtSense through `_http._tcp` and the `E70640` product ID, then performs a TCP/7777 liveness check.
- YachtSense Link publishes the `yachtsense-main` service identity.
- Raymarine Android 2.3.16 uses Android `NsdManager` for `_http`, `_raydb`, `_rtsp`, `_rym_rrc` and service enumeration.
- The app recognises `yachtsense-main` as YachtSense Link, but still connects directly to the MFD for screen view/control.

That separation is what makes the setup useful: the RUTX only has to reproduce YachtSense discovery and carry the relevant network traffic; the Axiom and Raymarine app continue speaking their normal protocols directly.

## License

[MIT](LICENSE)
