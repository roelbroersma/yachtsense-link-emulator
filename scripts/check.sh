#!/usr/bin/env bash
# Run source, syntax, metadata and package-layout checks used by CI.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="$(tr -d '[:space:]' < VERSION)"
RELEASE="${RELEASE:-1}"

UNFORMATTED="$(gofmt -l cmd)"
if [ -n "$UNFORMATTED" ]; then
  printf 'Go files require gofmt:\n%s\n' "$UNFORMATTED" >&2
  exit 1
fi
go test ./...
go vet ./...

sh -n package/root/etc/init.d/yachtsense-link-emulator
sh -n package/control/postinst
sh -n package/control/prerm
sh -n package/control/postrm
bash -n scripts/build-ipk.sh
bash -n scripts/build-pm-bundle.sh
bash -n scripts/build-current-pm-bundles.sh
bash -n scripts/source-archive.sh

for file in package/root/usr/share/rpcd/acl.d/*.json \
            package/root/usr/share/vuci/menu.d/*.json \
            package/root/usr/share/vuci/path.d/*.json; do
  jq empty "$file"
done

if command -v luac >/dev/null 2>&1; then
  luac -p package/root/usr/lib/lua/api/services/yachtsense_link_emulator.lua
fi
if command -v node >/dev/null 2>&1; then
  node --check package/root/www/views/services/YachtSenseLinkEmulator.js
fi

if [ -e package/root/www/views/services/YachtSenseLink.js ]; then
  echo 'Obsolete duplicate VuCI view YachtSenseLink.js was found.' >&2
  exit 1
fi
if grep -RniE 'migrat(e|ion)|migrated_from|/usr/local/usr/share' package/control package/root; then
  echo 'Prototype migration or legacy-path code was found.' >&2
  exit 1
fi

# Build and inspect the single firmware-independent RUTX payload.
bash scripts/build-ipk.sh
IPK="$(find dist -maxdepth 1 -name 'tlt_custom_pkg_yachtsense-link-emulator_*_arm_cortex-a7_neon-vfpv4.ipk' | sort | tail -n 1)"
test -n "$IPK"

TMPDIR_CHECK="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_CHECK"' EXIT
tar -tzf "$IPK" > "$TMPDIR_CHECK/ipk-files"
grep -Fxq './debian-binary' "$TMPDIR_CHECK/ipk-files"
grep -Fxq './control.tar.gz' "$TMPDIR_CHECK/ipk-files"
grep -Fxq './data.tar.gz' "$TMPDIR_CHECK/ipk-files"
tar -xzf "$IPK" -C "$TMPDIR_CHECK"

tar -tzf "$TMPDIR_CHECK/data.tar.gz" > "$TMPDIR_CHECK/data-files"
grep -Fxq './usr/sbin/yachtsense-link-emulator' "$TMPDIR_CHECK/data-files"
grep -Fxq './etc/init.d/yachtsense-link-emulator' "$TMPDIR_CHECK/data-files"
grep -Fxq './usr/share/vuci/menu.d/yachtsense-link-emulator.json' "$TMPDIR_CHECK/data-files"
grep -Fxq './usr/share/vuci/path.d/yachtsense-link-emulator.json' "$TMPDIR_CHECK/data-files"
grep -Fxq './usr/share/rpcd/acl.d/yachtsense-link-emulator.json' "$TMPDIR_CHECK/data-files"
grep -Fxq './www/views/services/YachtSenseLinkEmulator.js.gz' "$TMPDIR_CHECK/data-files"

tar -tzf "$TMPDIR_CHECK/control.tar.gz" > "$TMPDIR_CHECK/control-files"
grep -Fxq './postinst' "$TMPDIR_CHECK/control-files"
grep -Fxq './postinst-pkg' "$TMPDIR_CHECK/control-files"
grep -Fxq './prerm' "$TMPDIR_CHECK/control-files"
grep -Fxq './prerm-pkg' "$TMPDIR_CHECK/control-files"
grep -Fxq './postrm' "$TMPDIR_CHECK/control-files"

tar -xOzf "$TMPDIR_CHECK/control.tar.gz" ./control > "$TMPDIR_CHECK/control"
tar -xOzf "$TMPDIR_CHECK/control.tar.gz" ./postinst > "$TMPDIR_CHECK/postinst"
tar -xOzf "$TMPDIR_CHECK/control.tar.gz" ./prerm > "$TMPDIR_CHECK/prerm"
grep -Fxq 'Architecture: arm_cortex-a7_neon-vfpv4' "$TMPDIR_CHECK/control"
grep -Fxq 'Router: RUTX' "$TMPDIR_CHECK/control"
grep -Fxq 'tlt_name: yachtsense-link-emulator' "$TMPDIR_CHECK/control"
grep -Fq 'default_postinst "$0" "$@"' "$TMPDIR_CHECK/postinst"
grep -Fq 'default_prerm "$0" "$@"' "$TMPDIR_CHECK/prerm"
if grep -q '^Firmware:' "$TMPDIR_CHECK/control"; then
  echo 'Generic IPK must not be tied to a RutOS firmware release.' >&2
  exit 1
fi

# Build the two WebUI wrappers currently published by Teltonika for RUTX:
# Stable 7.24.1 and Latest 7.24.2. Only main/Firmware may differ.
bash scripts/build-current-pm-bundles.sh
STABLE="dist/yachtsense-link-emulator_${VERSION}-${RELEASE}_RUTX_00.07.24.1.tar.gz"
LATEST="dist/yachtsense-link-emulator_${VERSION}-${RELEASE}_RUTX_00.07.24.2.tar.gz"
for bundle in "$STABLE" "$LATEST"; do
  test -f "$bundle"
  tar -tzf "$bundle" > "$TMPDIR_CHECK/$(basename "$bundle").files"
  grep -Fxq './main' "$TMPDIR_CHECK/$(basename "$bundle").files"
  grep -Fq "./$(basename "$IPK")" "$TMPDIR_CHECK/$(basename "$bundle").files"
done

tar -xOzf "$STABLE" ./main > "$TMPDIR_CHECK/stable-main"
tar -xOzf "$LATEST" ./main > "$TMPDIR_CHECK/latest-main"
grep -Fxq 'Firmware: RUTX_R_00.07.24.1' "$TMPDIR_CHECK/stable-main"
grep -Fxq 'Firmware: RUTX_R_00.07.24.2' "$TMPDIR_CHECK/latest-main"
grep -Fxq 'Router: RUTX' "$TMPDIR_CHECK/stable-main"
grep -Fxq 'Router: RUTX' "$TMPDIR_CHECK/latest-main"

# Prove both wrappers carry byte-for-byte the same generic IPK.
mkdir -p "$TMPDIR_CHECK/stable" "$TMPDIR_CHECK/latest"
tar -xzf "$STABLE" -C "$TMPDIR_CHECK/stable" "./$(basename "$IPK")"
tar -xzf "$LATEST" -C "$TMPDIR_CHECK/latest" "./$(basename "$IPK")"
GENERIC_SHA="$(sha256sum "$IPK" | awk '{print $1}')"
test "$(sha256sum "$TMPDIR_CHECK/stable/$(basename "$IPK")" | awk '{print $1}')" = "$GENERIC_SHA"
test "$(sha256sum "$TMPDIR_CHECK/latest/$(basename "$IPK")" | awk '{print $1}')" = "$GENERIC_SHA"

printf 'All checks passed for %s plus current Stable/Latest WebUI wrappers.\n' "$IPK"
