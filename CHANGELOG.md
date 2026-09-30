# Changelog

## 1.2.2 — 2026-09-30

- Move RayNet into Services as one interface/address and emulator-state row.
- Remove the standalone public RayNet card and Built-in relay/Avahi display row.
- Keep Axiom, Cerbo GX, Wi-Fi and wired hosts in Devices; leave forwarding and discovery logic unchanged.
- Restore purpose-first documentation covering wired Axiom internet, app discovery, dashboard use and VPN/routing requirements.
- Review every Markdown guide, distinguish field observations from unverified app connectivity, and explain ports 8088 versus 7777.
- Add dashboard regression tests and use new 1.2.2 package metadata and management asset names.

## 1.2.1 — local test only

- Increase package/program versions and refresh management asset names so a new upload is newer than 1.2.0.
- No functionality change; this test build was supplied directly, not published as a GitHub release.

## 1.2.0 — 2026-09-30

- Add a compact, login-free, read-only router dashboard on port 8088, advertised through YachtSense; health stays on its separate port.
- Show the actual default-policy failover uplink and its received RSSI, real interface names, optional public IPv4, onboard SSIDs, Wi-Fi and wired/known devices.
- Add the actual RutOS profile, GPS/named geofences, VPN timers/counters, RMS and VXLAN status.
- Hide absent SIMs, disabled WANs, missing IP fields, policy weights and redundant explanatory text.
- Add configurable source-network access, initially allowing all sources for the requested VPN test without changing firewall rules or admin authentication.
- Preserve the complete 1.1.2 installation and RPC permission fixes; add HTTP, telemetry and browser tests.

New telemetry and Axiom page integration require real-device validation.

## 1.1.2 — 2026-09-30

- Consolidate the 1.1.1-2 WebUI feed-index repair and 1.1.1-3 authenticated RPC bridge.
- Ship the confirmed rpcd **publish and access** permissions for the six helper methods.
- Keep the separate uhttpd bus ACL and read/write session ACLs.
- Apply ACLs before refreshing rpcd, uhttpd, session rights and WebUI menu registration.
- Use versioned browser asset URLs and show unavailable status as Unknown, not Off.
- Preserve configuration and the existing network/discovery daemon implementation.
- Track focused package, permission, adapter and helper regression tests in source.
- Document field findings and distinguish local/CI checks from router validation.

## 1.1.1

- Select RutOS 7.25 Yocto architecture and the standard ar IPK format.
- Remove the unnecessary libc dependency from the static Go package.

## 1.1.0

- Rebuild management around a bounded native backend, persistent network choices,
  observed runtime status, a selective discovery relay and a simplified WebUI.
