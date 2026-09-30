# Discovery, wired internet and app networking

The emulator adds the Raymarine-specific recognition layer to an already
configured Teltonika network. It does not transport the internet connection or
proxy the Axiom's screen. The protocol details below describe **this project's
implementation and recorded observations**, not a universal specification for
all Raymarine products or mobile-app versions.

## Axiom internet over Ethernet / RayNet

The original goal is to let an Axiom already connected by RayNet use the boat
router's internet connection without a second Axiom Wi-Fi association.

```text
Axiom -- Ethernet / RayNet --> Teltonika -- selected WAN --> Internet
                                  |
                   YachtSense discovery + HTTP health
```

The project advertises `_http._tcp.local` with the YachtSense product ID `E70640`.
The Axiom recognises the candidate; the emulator also answers the separate
connection check on TCP 7777. The owner confirmed online software-update checking
in this arrangement without a separate Axiom Wi-Fi internet connection.

The actual packet path still needs the router's RayNet address, an appropriate
Axiom gateway and DNS, forwarding rules and WAN routing/NAT. A successful HTTP
health check alone is not an internet test. The package leaves those network
settings to RutOS, including marina Wi-Fi/mobile failover.

### Interface selection

Automatic selection looks for the existing configured IPv4 address, normally
`198.18.0.1`, on a local interface. In one installation this is `eth0.3`; the name
is not a universal RayNet requirement. A manually selected interface must own the
address, unless address management was explicitly enabled.

`manage_ip=0` and `remove_ip_on_stop=0` are the defaults. With address management
enabled, the program tracks an address it adds itself; it does not own a
pre-existing address merely because that address matches the setting.

## YachtSense identity and ports

The default advertisement is equivalent to:

```text
PTR _http._tcp.local
    -> yachtsense-main Settings._http._tcp.local
SRV yachtsense-main Settings._http._tcp.local
    -> yachtsense-main.local:8088
TXT id=E70640 AF002A4
TXT model=Raymarine YachtSense Link
TXT version=V142.242.530
TXT mac=<selected interface MAC>
A   yachtsense-main.local
    -> <router address on the advertising network>
```

The serial/version text is emulated identity metadata, not the installed package
version and not a claim to run genuine YachtSense firmware. The hostname and
instance can be changed in the authenticated configuration.

| Port | Role in this project |
| --- | --- |
| UDP 5353 | mDNS/DNS-SD discovery on `224.0.0.251` |
| TCP 7777 | Axiom HTTP liveness check; returns `200 OK` and `OK` |
| TCP 8088 | Default read-only router dashboard; advertised through SRV when enabled |
| TCP 80 / HTTPS administration | Existing router WebUI; normal authentication remains |

The SRV port follows the dashboard setting; with the dashboard disabled it falls
back to 80. The HTTP health port is separate. The Axiom's YachtSense menu opening
the new dashboard and the Axiom reaching the internet are different tests.

## Raymarine app discovery and direct traffic

The selective relay handles these service types and related learned lookups:

| DNS-SD service | Intended use |
| --- | --- |
| `_http._tcp.local` | YachtSense / HTTP device discovery |
| `_rtsp._tcp.local` | MFD video-stream discovery |
| `_rym_rrc._tcp.local` | Remote-control discovery |
| `_raydb._tcp.local` | Raymarine MFD/database discovery |
| `_services._dns-sd._udp.local` | DNS-SD service enumeration |

PTR, SRV, TXT and associated address records are needed together: a service name
without its target host/address is not enough. The code retains learned names
when filtering follow-up queries and replies. Ordinary DNS on port 53 is not
reflected, and this is not a general-purpose forwarder for all Bonjour traffic.
The implementation listens/transmits IPv4 mDNS; carrying an AAAA record does not
make it an IPv6 multicast relay.

```text
Phone / app network             Teltonika                Axiom / RayNet
      -- mDNS service query --> selective relay ----------------->
      <----- PTR/SRV/TXT/address records --------------------------
      -- direct routed MFD connection --------------------------->
```

Recorded Axiom advertisements included **RTSP TCP 8554**, **remote control TCP
50000**, and **RayDB port 49111**. Use the actual SRV records of the MFD when
checking ports; especially the RayDB example is not a guaranteed fixed port for
every device. A previous protocol investigation also identified the stream path
`rtsp://<Axiom-IP>:8554/RAYMARINEMFD`. This release does not implement or proxy
those video/control protocols.

The phone therefore needs both discovery and direct bidirectional IP access.
The current project's home-VPN test observed iPhone queries reaching the boat
and matching Axiom advertisements being forwarded back. Complete viewing/control
from that iPhone remains unconfirmed; the application accepting and using those
records cannot be inferred from a green discovery indicator.

## Existing Avahi and same-interface networks

The authenticated **Discovery** setting supports:

| Mode | Behaviour |
| --- | --- |
| Automatic | Use direct discovery for one shared interface, an eligible existing Avahi reflector, or the built-in relay when appropriate |
| Built-in | Select the built-in relay; refuse an overlapping/unverified existing Avahi reflector rather than creating a loop |
| Avahi | Require an existing reflector eligible for the selected interfaces |
| Disabled | Disable cross-interface relay, without inherently disabling the separate YachtSense identity or health listener |

A process merely listening on UDP 5353 is not proof of reflection. A normal
Avahi responder and `umdns` are not automatically treated as cross-network
reflectors. The package inspects Avahi but does not rewrite its configuration.

If the app and RayNet use the same interface, the engine selects the direct path
and does not create a needless relay. Sharing a layer-2 bridge does not by itself
make IP addresses in different subnets mutually routable; gateways/firewalls
still matter for the later unicast traffic.

The public dashboard intentionally hides relay-engine details. The authenticated
configuration and CLI diagnostics retain the selected engine, detected reflector,
listeners and errors.

## Remote access over VPN / VXLAN

A VPN can carry routed MFD traffic. An explicitly configured multicast path, such
as a discovery-only VXLAN bridge, can deliver mDNS to the app-side interface:

```text
Home phone -- home LAN -- encrypted VPN -- boat router -- RayNet -- Axiom
                     \---- discovery-only VXLAN ----/
```

These are distinct requirements:

- The tunnel must cover the **VXLAN endpoints**, not just the Axiom subnet.
- The remote phone must be able to route to the **advertised Axiom address**, and
  the Axiom must have a return path through the boat router.
- mDNS queries and replies must reach the correct segments; TTL, source-address
  rules and bridge filtering can matter even when UDP 5353 packets are visible.
- NAT and acceleration rules must not cause traffic intended for IPsec to bypass
  the negotiated policy. MTU and latency also affect a later media session.

For the example used in testing, home `192.168.4.0/24` needs both boat
`192.168.40.0/24` (including the VXLAN endpoint) and RayNet `198.18.0.0/21` in the
relevant IPsec selectors. A working IKE peer does not prove both CHILD_SAs exist.
On the tested Teltonika/MikroTik combination, separate compatible child policies
were needed for the two subnet pairs. Do not broadly bridge DHCP and unrelated
LAN traffic merely to transport discovery.

A purely routed VPN requires an explicit mechanism to carry/relay multicast;
selecting a VPN interface in the emulator does not supply that mechanism on its
own. No VPN, VXLAN, firewall, NAT or FastTrack rule is installed by this package.

## Check each layer separately

A useful verification order is: existing IP/routing configuration, YachtSense
recognition on the Axiom, an Axiom online software check, app queries at the boat,
responses at the phone's LAN, and finally direct MFD connection attempts. Router
status-page access on 8088 and the 7777 health check do not replace the last step.

Repeated advertisements for different services on one Axiom are discovery
observations, not necessarily repeated disconnects. A DHCP entry or stale
neighbour is not a current online test either.

## Implementation and external references

The project's [DNS implementation](../cmd/yachtsense-link-emulator/dns.go),
[daemon](../cmd/yachtsense-link-emulator/daemon.go) and
[engine selection](../cmd/yachtsense-link-emulator/status.go) define its behaviour.
For the underlying protocols and original products, see
[RFC 6762, Multicast DNS](https://www.rfc-editor.org/rfc/rfc6762),
[Teltonika RUTX14 failover](https://wiki.teltonika-networks.com/view/RUTX14_Failover),
[MikroTik IPsec](https://help.mikrotik.com/docs/spaces/ROS/pages/11993097/IPsec), and
[Raymarine's original router-page instructions](https://docs.raymarine.com/81406/en-US/latest/AccessingTheWebInterfaceFromARaymar-FA2AE06D.html).
Manufacturer instructions for a genuine YachtSense Link are not a guarantee
that every behaviour is reproduced by this emulator.

[Back to installation](../README.md) · [Dashboard](public-status.md)
