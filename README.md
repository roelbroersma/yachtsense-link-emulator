# YachtSense Link Emulator 1.1.1 — RutOS 7.25.3

This release branch restores the exact application and packaging sources of the previously supplied 1.1.1 build. It publishes that same package as a standalone IPK for diagnosing the generic WebUI installation failure. It does not contain a new runtime fix and does not claim successful installation on the target router.

Target confirmed by the router's CLI:

- RUTX14 STM32; `RUTX_R_00.07.25.3`; ARMv7.
- opkg 0.6.3 (libsolv 0.7.28).
- Package architecture: `cortexa7hf-neon-vfpv4`.
- Logical package root: `/usr/local`.

The publication workflow rebuilds with Go 1.23.2 and verifies the complete IPK and WebUI-wrapper digests against the original supplied files before publishing. Only the IPK and firmware-specific WebUI upload are release assets. The existing default branch and old release tags are left unchanged.

## Reproduce the package

With Go 1.23.2 and Python 3.13.5:

```sh
python3 scripts/build.py --firmware RUTX_R_00.07.25.3
```

The IPK is written to `build/`; the WebUI wrapper is written to `dist/`. The full test suite and original test results remain in the previously supplied `yachtsense-link-emulator_1.1.1_source-en-tests.zip`; this branch contains the sources needed to reproduce the published package, not a claim of new hardware testing.

## CLI installation for diagnosis

Run as root on the RUTX14:

```sh
cd /tmp || exit 1
wget -O yachtsense-1.1.1.ipk 'https://github.com/roelbroersma/yachtsense-link-emulator/releases/download/v1.1.1/tlt_custom_pkg_yachtsense-link-emulator_1.1.1-1_cortexa7hf-neon-vfpv4.ipk' && (
  opkg -V4 install --force-reinstall /tmp/yachtsense-1.1.1.ipk
  rc=$?
  printf '\nOPKG_EXIT=%s\n' "$rc"
) 2>&1 | tee /tmp/yachtsense-install.log
```

Return the full output, especially the first dependency, signature, archive or package-script error. Do not force dependencies or architecture and do not remove the installed package first: the unchanged installation attempt is the evidence needed to diagnose the failure. Configuration and network settings are not changed by this publication process.
