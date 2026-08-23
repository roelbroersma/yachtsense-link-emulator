# Changelog

## [1.0.12] - 2026-08-24

- Make `Save & apply`, Start, Restart and Stop non-blocking from VuCI by scheduling bounded rc.common/procd actions in the background.
- Add BusyBox `timeout` guards around service stop/start/enable/disable calls so a slow service transition can never hold the API request indefinitely.
- Remove the full-page VuCI spinner from YachtSense actions; buttons still show local loading state.
- Add explicit Axios timeouts for save/control/status requests and always release the local loading state in `finally`.
- Refresh status asynchronously after a requested service transition instead of waiting for the daemon inside the API request.
- Keep the working v1.0.11 daemon, `/usr/local` paths, package-root UCI handling, single-snapshot interface detection and upgrade-safe package lifecycle hooks.

## [1.0.11] - 2026-08-23

- Make in-place RutOS upgrades safe by detecting `PKG_UPGRADE=1` in package lifecycle hooks.
- Skip package-specific runtime/network cleanup during upgrades; the new package takes over the existing state after RutOS stops the old service.
- Skip rpcd/uhttpd/VuCI cleanup in the old package `postrm` during upgrades; the new package `postinst` performs the required reloads after replacement is complete.
- Keep full cleanup behavior for an actual package removal.

## [1.0.10] - 2026-08-23

- Detect the YachtSense UCI configuration directory from the RutOS custom-package root and pass it explicitly to every `uci` command.
- Read and write `/usr/local/etc/config/yachtsense_link_emulator` on normal RutOS custom-package installations, with `/etc/config` as fallback.
- Make the init script read the same package-root UCI file as the VuCI status/save/control APIs.
- Keep all configuration operations on the native RutOS `uci` CLI; no firmware-dependent Lua UCI list methods are used.
- Keep the single-snapshot interface status, `br-lan` address stabilization, checkbox focus fix, `/usr/local` executable paths and protected Start/Restart error handling.
- Add dedicated v1.0.10 APIs and VuCI entry point to avoid stale cached v1.0.7-v1.0.9 code.

## [1.0.9] - 2026-08-23

- Move all configuration writes to the native RutOS `uci` CLI, avoiding firmware-specific Lua `uci` cursor methods completely.
- Wrap Start, Restart and Stop actions so backend exceptions return a useful WebUI message instead of HTTP 500.
- Add a dedicated status API that captures all IPv4 addresses from one `ip -o -4 addr show` snapshot per refresh.
- Keep the last valid interface address briefly when an individual poll is transiently incomplete, preventing `br-lan` from visually alternating between its CIDR and `No IPv4 address detected`.
- Remove the large browser focus rectangle that remained around native checkboxes after clicking them.
- Keep opkg `dest root` path handling for `/usr/local` and storage-expansion installations.
- Route the active VuCI page only through the v1.0.9 status/save/control APIs.

## [1.0.8] - 2026-08-23

- Fix `Save & apply` on RutOS builds where the Lua `uci` binding exposes neither `add_list()` nor `set_list()`.
- Write and read `remote_interface` list values through the native RutOS `uci` CLI instead of unsupported Lua cursor list methods.
- Keep scalar UCI settings on the existing Lua cursor path.
- Retain the `/usr/local` opkg-root handling and storage-expansion compatibility introduced in v1.0.7.
- Retain managed RayNet-address ownership protection and stable-PID Start/Restart verification.
- Add dedicated v1.0.8 save/control API routes and a cache-busted VuCI entry point.

## [1.0.7] - 2026-08-23

- Resolve the daemon and init-script paths from RutOS opkg `dest root` instead of assuming `/usr/sbin` and `/etc/init.d`.
- Support the normal RutOS custom-package layout under `/usr/local`, including when storage expansion changes the physical backing storage.
- Make the init script execute the daemon from the resolved package root.
- Create absolute `/etc/rc.d` autostart links to the package init script so boot-time startup also works from `/usr/local`.
- Add dedicated v1.0.7 save/control API routes and a cache-busted VuCI entry point.
- Retain all v1.0.6 fixes for managed RayNet address ownership and stable-PID verification.

## [1.0.6] - 2026-08-23

- Attempt to fix `Save & apply` with UCI `cursor:set_list()`; later RutOS testing showed this method is also unavailable in the target Lua binding and is superseded by v1.0.8.
- Preserve an existing RayNet address when `Manage RayNet IPv4 address` is disabled by relinquishing stale package ownership before restart.
- Verify Start/Restart with a stable daemon PID across several seconds, so a procd crash/respawn loop is no longer reported as success.
- Add dedicated v1.0.6 save/control API routes and a cache-busted VuCI entry point.
- Keep GitHub releases minimal: only the WebUI-uploadable RutOS 7.24.1 and 7.24.2 wrapper packages are published.

## [1.0.5] - 2026-08-23

- Make `Save & apply` schema-proof by transporting the complete form as one JSON string instead of separate FunctionService boolean/list fields.
- Add strict network preflight validation: when managed addressing is disabled, the configured RayNet CIDR must already exist on the selected Axiom interface.
- Reject Raymarine app interfaces that do not currently have an IPv4 address when they are distinct from the Axiom interface.
- Add verified Start/Restart actions that wait for the daemon to remain alive before reporting success.
- Return the latest YachtSense service log lines when procd accepts a start but the daemon immediately exits.
- Add a cache-busted `YachtSenseLinkEmulatorV105.js` entry point plus dedicated v1.0.5 save/control API routes and ACL permissions.

## [1.0.4] - 2026-08-23

- Fix HTTP 422 on `Save & apply`: RutOS FunctionService now receives `remote_interfaces` as an explicitly declared `{ list = true }` option.
- Add a schema-safe save API that declares every submitted form field before applying detailed validation.
- Add the save/config API routes to the package ACL so authenticated VuCI users can write the configuration.
- Use a versioned `YachtSenseLinkEmulatorV104.js` entry point and versioned module import to force browser/VuCI cache invalidation after package upgrades.
- Keep the v1.0.3 serialized status refresh and transient interface-address stabilization.

## [1.0.3] - 2026-08-23

- Attempt to write `remote_interface` as a UCI list with `add_list()`; target RutOS testing later showed this cursor method is unavailable.
- Add a dedicated validated configuration-save endpoint that accepts normal JSON `false` boolean values and returns useful error messages.
- Prevent overlapping five-second status requests in the VuCI page.
- Keep the last valid IPv4 interface snapshot across up to two transient empty samples, preventing `br-lan` and other interfaces from visually jumping between a CIDR and `No IPv4 address detected`.
- Extend package checks for the configuration endpoint, CSP-safe wrapper and external stylesheet.

## [1.0.2] - 2026-08-23

- Fix the VuCI layout under RutOS Content Security Policy by moving package styling to an external same-origin stylesheet.
- Add a CSP-safe wrapper view that loads the stylesheet from `/assets/yachtsense-link-emulator.css`.
- Rework the page into a readable responsive two-column card layout with proper spacing, forms, status badges, interface cards, actions and log panels.
- Mark the YachtSense page as the package `main_page` so Package Manager links directly to the intended view.

## [1.0.1] - 2026-08-23

- Fix RutOS package lifecycle integration by using the native `default_postinst` and `default_prerm` wrappers.
- Install project-specific hooks as `postinst-pkg` and `prerm-pkg`, matching the Teltonika RutOS package build system.
- Let RutOS reload rpcd ACLs and VuCI path/menu routes after installation instead of restarting services manually.
- Reload VuCI routes explicitly after package removal.
- Remove the fragile post-install `chmod` of `/usr/sbin/yachtsense-link-emulator`; executable mode is now validated in the package payload.
- Extend CI checks to verify the daemon, init script, ACLs, VuCI descriptors and native RutOS lifecycle scripts are present in the IPK.

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
