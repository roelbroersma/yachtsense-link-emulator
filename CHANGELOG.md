# Changelog

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

- Fix `Save & apply` by writing `remote_interface` as a real UCI list with `add_list` instead of passing a Lua table to `cursor:set`.
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
