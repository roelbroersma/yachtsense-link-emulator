# Protocol notes

## Purpose

The emulator implements the minimum local-network behavior used by a Raymarine
Axiom to recognize a YachtSense Link-style internet source. No proprietary
Raymarine code is distributed.

## Discovery sequence

1. The Axiom browses DNS-SD services on the local link using mDNS.
2. The emulator advertises an `_http._tcp.local` service.
3. The Axiom reads the service TXT record.
4. A service whose `id` value starts with `E70640` is treated as a YachtSense
   Link candidate.
5. The Axiom resolves the advertised `.local` hostname to the configured IPv4
   address.
6. A separate connection monitor requests the candidate address on TCP port
   `7777`; an HTTP status in the successful `2xx` range is accepted.

## Published records

The unsolicited response and query replies contain:

- PTR: `_http._tcp.local` → configured service instance;
- SRV: configured service instance → configured `.local` hostname, port `80`;
- TXT:
  - `id=E70640 <serial>`;
  - `model=Raymarine YachtSense Link`;
  - `version=<configured version>`;
  - `mac=<selected interface MAC>`;
- A: configured `.local` hostname → configured IPv4 address;
- PTR: `_services._dns-sd._udp.local` → `_http._tcp.local`.

The SRV record uses port `80` to resemble a real discovery advertisement. The
connection-monitor responder is deliberately separate and binds to port `7777`.

## mDNS behavior

- IPv4 multicast group: `224.0.0.251`;
- UDP port: `5353`;
- outgoing IP TTL: `255`;
- default DNS record TTL: `120` seconds;
- immediate and periodic unsolicited announcements;
- replies to relevant service, instance, host and service-enumeration queries;
- zero-TTL goodbye announcement during a clean shutdown.

The socket selects the configured source IPv4 address for multicast output. The
configured address must therefore exist on the selected interface before the
mDNS component starts.

## HTTP behavior

The optional HTTP component binds only to the configured IPv4 address and port.
Every path returns:

```http
HTTP/1.1 200 OK
Content-Type: text/plain; charset=utf-8
Cache-Control: no-store

OK
```

## Deliberately not implemented

- YachtSense Link JSON-RPC administration API;
- HTTPS administration service and certificates;
- Raymarine cloud connector;
- GNSS endpoints on ports `7778` or `9999`;
- NMEA 2000 / SeaTalkNG behavior;
- digital input/output controls;
- modem, Wi-Fi uplink or data-usage reporting;
- firmware update services.
