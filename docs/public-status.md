# Public router status

The separate Go HTTP server serves only GET/HEAD assets and a typed, cached `/api/status` snapshot. It never exposes the authenticated API, arbitrary RPC methods, configuration, credentials, command execution or write endpoints. `Manage` links to the existing HTTPS administration page.

## Access

`dashboard_enabled=1`, `dashboard_port=8088`, `dashboard_allow_all=1` are the requested test defaults. This binds IPv4 on all local addresses; existing router firewalls still apply. Disabling `dashboard_allow_all` permits loopback, detected RayNet and app-network subnets plus `dashboard_allowed_cidrs`. The latter initially contains `192.168.4.0/24` for the existing VPN. Forwarded headers never grant access. Administrative changes still require a Teltonika session.

## Data sources

The root daemon samples fixed, bounded read-only commands. Interface/Wi-Fi/association data uses ubus; the active default-policy member uses the actual mwan3 policy selected by the configured IPv4 default rule, not the first policy printed. Existing flows and explicit policy rules can use another WAN. Mobile telemetry uses `gsmctl -E/-q/-o/-z`; received RSSI is associated with its station or modem, not onboard clients. Public IPv4 is queried only for active links, at most once a minute per device through the fixed HTTPS endpoint `api.ipify.org`; failures remove the field.

Profile comes from `profiles.general.profile`. GPS position uses `/gps/position/status`; geofences use enabled circle definitions and a fresh fix, without changing profiles or firing events. Missing or old fixes never count as inside a fence. VPN status uses installed CHILD_SAs, OpenVPN status and WireGuard handshakes; VXLAN Up refers to the local interface, not proof of a reachable peer. RMS reads its actual connection state.

Devices combine current Wi-Fi associations, IPv4 neighbours, local bridge forwarding entries, DHCP names and static leases. Known/stale entries are not labelled online; devices never observed cannot be invented. Only bounded emulator event logs are returned. Missing measurements are omitted or rendered as a short unknown state, without raw diagnostic output in the public page.

The collector is observational and does not modify networking. Firmware variations may leave individual fields empty pending a real-device sample.

## Compatibility and validation

The Axiom router-page SRV port is changed to 8088 when the dashboard is enabled; the existing health listener remains separate at 7777. Opening the new port from an Axiom must be verified on the actual device. Embedded assets use plain ES5 and XMLHttpRequest, with no CDN or inline-script dependency.

Run `bash scripts/check.sh` for Go race tests, finite RPC/adapter and package validation. Optional `python3 tests/dashboard_browser_test.py` exercises real assets with synthetic telemetry at desktop, 800x480 MFD and mobile sizes. Browser fixtures are test data, not observations from the router.

References: Teltonika Gsmctl commands and GPS documentation, published RutOS SDK profile script, RMS enum definitions, native iproute2/strongSwan status outputs.
