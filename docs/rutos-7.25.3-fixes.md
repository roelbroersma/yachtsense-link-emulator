# RutOS 7.25.3 integration: confirmed failures and fixes

## Packaging

RutOS 7.25.3 on the tested RUTX14 reports opkg 0.6.3/libsolv, package root
`/usr/local`, and architecture `cortexa7hf-neon-vfpv4`. The old OpenWrt name
`arm_cortex-a7_neon-vfpv4` is not in its accepted architecture list. The builder
selects the architecture and ar container from the firmware target, not merely
the output filename.

The native non-legacy Package Manager installs an uploaded application by package
name from `custom_packages`, using a temporary lists directory. Hence a valid
upload needs a Packages index containing the filename, size and complete-IPK
hashes. This is separate from the payload/control hash in `main`. Version 1.1.1-2
with this index installed successfully through the WebUI.

## Menu/API and operating-system permissions

Three different failures must not be conflated:

1. A stale API route table returned HTTP 404, even though the menu and JS existed.
   Reloading uhttpd registered the adapter and restored HTTP 200.
2. The WebUI required API ACL patterns covering the normalized trailing slash.
   Separately, a direct subprocess of uhttpd could not read the root-only config
   and runtime state. Running the webserver as root or making all configs writable
   is not the solution: the API now invokes a finite root-owned RPC helper.
3. On this firmware rpcd itself runs as user `rpcd`. It needs permission both to
   **publish** `yachtsense-link-emulator` and to **access** its six own methods.
   Without publish, `ubus list` could not find the object. With only publish, the
   object appeared, but a method call still returned ubus 4. Adding access made
   RPC status and the authenticated WebUI API succeed with the real running state.

The installer ships both OS-level ACLs plus the session ACL. Refresh order is:
bus ACL reload; rpcd registration restart; session ACL reload; uhttpd route reload;
menu notification. The refresh is bounded and detached from the upload request
so an HTTP reload does not prevent the package transaction from completing.
It does not restart networking or modify the emulator's configuration.

Tests inspect these files from inside the built IPK. In particular, separate
checks require both rpcd publish and rpcd access, so either omission fails CI.
The helper rejects arbitrary methods, paths, commands, NULs and oversized payloads.

## Discovery is separate from management

Successful management only establishes WebUI/API access. The iPhone app still
requires mDNS discovery and a bidirectional routed unicast path to the Axiom.
In the user's topology a discovery-only VXLAN connects home to the boat LAN,
while IPsec separately carries traffic to the RayNet subnet. Both subnet pairs
must be negotiated. MikroTik NAT bypass and FastTrack exceptions must use the
correct RayNet prefix; bridge multicast success alone does not verify TCP access.

Do not infer app connectivity from Axiom advertisements or green daemon status.
The currently retained daemon may log different services from one Axiom repeatedly.
The log is not evidence of a disconnect/reconnect cycle. Diagnose app traffic with
packet captures on the home LAN and boat VXLAN/bridge, including IPv4 and IPv6
mDNS queries and routed connections to the Axiom. No speculative network changes
are included in this maintenance release.

References:
- Teltonika RutOS code: https://github.com/wirelane/teltonika-gpl-sdk-rut951
- OpenWrt ubus/rpcd source: https://git.openwrt.org/project/ubus.git
- Multicast DNS source-address and overlay-subnet rules: https://www.rfc-editor.org/rfc/rfc6762#section-11
- MikroTik IPsec/NAT/FastTrack: https://help.mikrotik.com/docs/spaces/ROS/pages/11993097/IPsec
