# RutOS package layout

The IPK installs the following components.

## Runtime daemon

`/usr/sbin/yachtsense-link-emulator`

A static ARMv7 Go binary implementing mDNS discovery and the optional HTTP
responder.

## UCI configuration

`/etc/config/yachtsense_link_emulator`

Stores the master switch, component switches, interface, address and advertised
identity. It is listed as an opkg conffile so local changes survive an ordinary
same-package update.

## procd service

`/etc/init.d/yachtsense-link-emulator`

Reads UCI, optionally adds the configured address, exports daemon environment
variables and supervises the process with procd.

## RutOS API service

`/usr/lib/lua/api/services/yachtsense_link_emulator.lua`

Implements status, save, start, stop and restart endpoints. The API validates a
complete form before committing UCI and exposes recent logs and interface
information to VuCI.

## Strict JSON registrations

JSON has no comment syntax, so these files are intentionally valid strict JSON:

- `/usr/share/rpcd/acl.d/yachtsense-link-emulator.json` grants API/UCI access;
- `/usr/share/vuci/menu.d/yachtsense-link-emulator.json` creates the Services
  menu entry;
- `/usr/share/vuci/path.d/yachtsense-link-emulator.json` maps the HTTP API path
  to the Lua service.

## VuCI view

`/www/views/services/YachtSenseLinkEmulator.js`

A dependency-free Vue component using the Axios instance supplied by RutOS. A
deterministic gzip copy is installed beside it for web-server content
negotiation.

## Package lifecycle scripts

- `postinst` applies the current v1 UCI master switch and reloads rpcd/uhttpd;
- `prerm` stops the service and removes only package-managed runtime state;
- `postrm` reloads rpcd/uhttpd after file removal.

There is deliberately no importer or migration code for earlier experimental
package names or configuration formats.
