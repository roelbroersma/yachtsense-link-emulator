# Changelog

## [1.0.0] - 2026-08-21

- Static ARMv7 YachtSense Link mDNS responder using the `yachtsense-main` identity.
- Default YachtSense serial `AF002A4` with the selected RUTX interface MAC advertised dynamically.
- HTTP connection-monitor responder on TCP 7777.
- Separate Axiom/RayNet and Raymarine app network roles.
- Raymarine-aware mDNS relay for `_http._tcp`, `_rtsp._tcp`, `_rym_rrc._tcp`, `_raydb._tcp` and DNS-SD enumeration.
- Learned DNS-SD instance/host follow-up forwarding for Android NSD resolution.
- Automatic no-relay mode when both sides are already on the same interface.
- UDP/5353, Avahi, Avahi-reflector and `umdns` detection.
- Info/Debug logs for app/client queries, Axiom discovery and relay direction.
- RutOS 7 UCI/procd integration and VuCI management page.
- One firmware-independent `arm_cortex-a7_neon-vfpv4` RUTX IPK.
- Small firmware-specific Package Manager wrappers around that same IPK.
- WebUI wrappers for the current RUTX Stable `RUTX_R_00.07.24.1` and Latest `RUTX_R_00.07.24.2` releases.
- CI checks that prove both wrappers contain a byte-identical generic IPK.
