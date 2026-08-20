# Networking

## Layer-2 requirement

mDNS is link-local multicast. The selected Teltonika interface and the Axiom
must be in the same Ethernet broadcast domain. A normal bridge such as `br-lan`
is suitable when the Axiom's cable is part of that bridge.

## Managed address

The default `198.18.0.1/21` address follows the RayNet/YachtSense-style network
used for the emulator. The service can add it as a secondary address without
removing the router's existing LAN address.

Address handling is conservative:

1. If the address already exists, it is left untouched.
2. If the package adds the address, it writes a small state file in `/var/run`.
3. On stop, it removes only the address recorded in that state file.
4. On restart, an old managed address is removed before new settings are applied.

Disable **Manage the IPv4 address** when the address is configured elsewhere.

## Internet routing

The emulator only supplies discovery and liveness behavior. For actual internet
access the selected network still needs ordinary router configuration:

- a usable default route on the Teltonika router;
- forwarding from the Axiom-facing interface to the selected WAN path;
- source NAT/masquerading where required;
- working DNS resolution;
- firewall rules that permit the intended forwarding;
- DHCP only when that is part of the chosen RayNet design.

These settings are intentionally not modified by the package because network
and firewall policies differ between installations.

## Ports

| Protocol | Address/port | Purpose |
|---|---|---|
| mDNS | `224.0.0.251:5353/UDP` | DNS-SD discovery |
| HTTP | `<configured IP>:7777/TCP` | Axiom connection monitor |
| HTTP SRV value | port `80` | Discovery metadata only |

If another mDNS responder is running, Linux socket reuse normally allows both
services. The emulator answers only names associated with its own advertised
service.
