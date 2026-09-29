# Changelog

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
