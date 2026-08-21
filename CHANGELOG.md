# Changelog

All notable changes to this project are documented here.

## [1.0.0] - 2026-08-21

### Added

- Static ARMv7 YachtSense Link mDNS responder with the real `yachtsense-main` service identity.
- Default public YachtSense serial `AF002A4` and dynamic selected-interface MAC advertisement.
- Optional HTTP connection-monitor responder on TCP port 7777.
- Separate Axiom/RayNet and Raymarine app network roles.
- Raymarine-aware mDNS relay for `_http._tcp`, `_rtsp._tcp`, `_rym_rrc._tcp`, `_raydb._tcp` and DNS-SD enumeration.
- Learning of DNS-SD instance and SRV host names so follow-up Android NSD resolution is relayed safely.
- Automatic no-relay behavior when Axiom and app roles use the same interface.
- Detection of UDP/5353 listeners, Avahi, Avahi reflector mode and `umdns`.
- Automatic avoidance of a second reflector when Avahi reflection is already active.
- Info/Debug logging with Raymarine app/client queries, Axiom/MFD discovery and relay direction.
- Runtime last-seen Axiom/client state exposed in VuCI.
- Conservative management of a RayNet-style IPv4 address.
- RutOS 7 UCI and procd integration.
- VuCI page under **Services → YachtSense Link Emulator**.
- Start/stop/restart, interface selection, live status and recent logs.
- Reproducible `ipq40xx` IPK and source-archive build scripts.
- Unit, syntax, descriptor and package-layout checks.
