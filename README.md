# Router status dashboard — 1.2.0

For RUTX14 / RutOS RUTX_R_00.07.25.3. Upload the release `.tar.gz` without extracting or removing the previous package. The new read-only page is available at `http://192.168.40.1:8088/` (LAN/VPN) and `http://198.18.0.1:8088/` (RayNet), subject to existing firewall rules. `Manage` retains normal router authentication.

The status-page configuration includes enable, port, **Allow all source networks** (on for this requested test release), and extra source CIDRs when restriction is enabled. See [public status](docs/public-status.md) for data sources, limits, tests and deployment details.

The new page and parsers are tested with synthetic telemetry; observations from the actual router remain to be checked. Existing v1.1.2 integration documentation follows.

# YachtSense Link Emulator

**Release 1.1.2 — Teltonika RUTX14, RutOS RUTX_R_00.07.25.3.**

Upload `yachtsense-link-emulator_1.1.2-1_RUTX_00.07.25.3.tar.gz` unchanged via
**System → Package Manager → Upload package**. Do not extract it or remove the
previous package first. Existing settings are retained. Wait about 20 seconds,
then sign out and back in before opening **Services → YachtSense Link Emulator**.
The standalone `.ipk` is for CLI installation/diagnostics, not a firmware image.
This is a third-party package, not signed or endorsed by Teltonika or Raymarine.

## What 1.1.2 fixes

The release incorporates the field-tested 1.1.1-2/1.1.1-3 packaging and API fixes,
plus the confirmed `rpcd` authorization fix. Both `publish` and narrowly scoped
`access` permissions are necessary: publishing exposes the RPC object, while
access allows the non-root RPC loader to locate it again when a method is called.
These permissions now live in tracked package files instead of manual CLI edits.

The WebUI upload includes `Packages` and `Packages.gz` because RutOS 7.25 installs
uploaded packages through a temporary local feed. An IPK alone is not sufficient
inside that wrapper. All package/control/index metadata agree on version and
Yocto architecture `cortexa7hf-neon-vfpv4`.

The authenticated Lua adapter uses a finite root-owned RPC helper. It supports
only status, diagnostics, save, start, stop and restart; there is no arbitrary
command execution endpoint. Configurations remain root-only. Separate bus ACLs
permit `uhttpd` to call the helper and `rpcd` to publish and find it. The installer
reloads bus ACLs before restarting RPC registration, reloads session permissions,
and refreshes the web backend and menus. Browser assets have new versioned URLs.

An unavailable status now shows **Unknown**, not a false **Off** or **Not running**.
The daemon's network/discovery implementation is unchanged from 1.1.1; only its
compiled package-version string changes. This release does not claim to solve
Raymarine iPhone discovery across every VPN topology.

## Networking

Fresh installations are disabled until enabled. Automatic RayNet selection finds
the interface already owning the configured address, normally `198.18.0.1`.
IP address management is off by default. Explicit existing network choices are
preserved; change a legacy manual choice to Automatic when required.

The built-in daemon publishes the YachtSense identity and supplies the HTTP
health endpoint. The built-in selective relay or an existing Avahi reflector can
handle cross-network discovery. Avahi is not modified by this package. Axiom and
app detections report observed traffic, not a proven remote-control session.

The package does not edit firewall, DHCP, routes, IPsec or VXLAN settings. With a
VPN, the Axiom subnet must be routed in both directions and mDNS must reach the
selected app interface. Repeated Axiom service advertisements alone do not prove
that the phone has received them. See `docs/rutos-7.25.3-fixes.md`.

## CLI diagnostics

```sh
opkg status tlt_custom_pkg_yachtsense-link-emulator
/usr/local/usr/sbin/yachtsense-link-emulator --api diagnostics
ubus -v list yachtsense-link-emulator
ubus call yachtsense-link-emulator status '{}'
api get /yachtsense-link-emulator-v1100/status
```

A successful `api` process exit is not sufficient: inspect `http_code`, `success`
and the nested application result. The latter must report `ok: true` and no
configuration read error.

## Rebuild and test

Linux, Go 1.22+, Python 3.10+, Node 22, GNU ar, Bash and LuaJIT (or texlua) are used.
The reproducible release workflow pins Go 1.23.2 and Python 3.13.5. There are no
external Go dependencies.

```sh
LUA_TEST=luajit bash scripts/check.sh
```

This runs Go tests with the race detector, Go vet, UI logic tests, mocked Lua
adapter/RPC tests, the ARMv7 crossbuild and checks of the actual release archives.
Optional visual fixture checks use `python3 tests/layout_test.py` with Playwright
and `/usr/bin/chromium`. They do not emulate a full RutOS installation.

The preceding build plus the two RPC ACL repairs was confirmed on the user's
RUTX14: RPC exit 0, HTTP 200 and current running status. The consolidated 1.1.2
install/upgrade still requires confirmation on the router; local tests and CI
are not represented as hardware tests. Raw user logs, MAC addresses and secrets
are not committed to this repository.
