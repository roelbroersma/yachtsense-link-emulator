# Compatibility

## Supported target in v1.0.0

The release package is built as `ipq40xx` with a static `linux/arm/v7` daemon and
uses the RutOS 7 VuCI/API layout.

Expected devices in the Teltonika RUTX family:

- RUTX08
- RUTX09
- RUTX10
- RUTX11
- RUTX12
- RUTX14
- RUTX50
- RUTXR1

RUTX14 is the primary development target. Hardware-specific validation on every
listed model is still welcome.

## Operating-system boundary

Only RutOS 7 is targeted. The project uses:

- the RutOS 7 Vue-based VuCI frontend;
- Lua `FunctionService` API modules;
- `rpcd` ACL and VuCI menu/path descriptors;
- UCI and `procd` service management.

RutOS 6 used a different WebUI architecture and is not supported. The exact
first compatible RutOS 7 maintenance version has not been qualified, so a
current RutOS 7 release is recommended.

## Not included in the v1.0.0 binary release

- RUTM and RUTC families;
- OTD, TRB and other Teltonika product families;
- ARM64/aarch64 targets;
- legacy RUT2xx/RUT9xx targets;
- generic OpenWrt without the Teltonika VuCI/API framework.

The Go daemon itself is portable, but each additional platform needs a matching
binary architecture and package/WebUI validation before it should be advertised
as supported.

## Official references

- [Teltonika product firmware updates](https://wiki.teltonika-networks.com/view/Product_Firmware_Updates)
- [RUTX14 package downloads](https://wiki.teltonika-networks.com/view/RUTX14_Package_Downloads)
- [RutOS SDK instructions](https://wiki.teltonika-networks.com/view/RUTOS_Software_Development_Kit_%28SDK%29_Instruction)
