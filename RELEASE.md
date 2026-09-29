## RUTX14 · RutOS RUTX_R_00.07.25.3

Same **1.1.1** package as previously supplied, now also available as a standalone IPK for CLI diagnosis. No runtime changes in this publication.

- **CLI / SSH:** `tlt_custom_pkg_yachtsense-link-emulator_1.1.1-1_cortexa7hf-neon-vfpv4.ipk`
- **WebUI upload:** `yachtsense-link-emulator_1.1.1-1_RUTX_00.07.25.3.tar.gz` — upload without extracting.

The build checks that both files are byte-identical to the original files. Architecture: `cortexa7hf-neon-vfpv4`; installation root: `/usr/local`.

**Diagnostic prerelease:** the router accepted the revised compatibility metadata but its WebUI still reported a generic installation failure. Actual installation is not confirmed. Use `opkg -V4 install` on the IPK to obtain the underlying error. No signature, dependency or architecture bypass is applied.
