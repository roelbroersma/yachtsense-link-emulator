#!/usr/bin/env bash
# Run source, syntax, metadata and package-layout checks used by CI.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Go source must be formatted and all protocol unit tests must pass.
UNFORMATTED="$(gofmt -l cmd)"
if [ -n "$UNFORMATTED" ]; then
  printf 'Go files require gofmt:\n%s\n' "$UNFORMATTED" >&2
  exit 1
fi
go test ./...
go vet ./...

# BusyBox-compatible shell files are also valid under a normal POSIX shell.
sh -n package/root/etc/init.d/yachtsense-link-emulator
sh -n package/control/postinst
sh -n package/control/prerm
sh -n package/control/postrm
bash -n scripts/build-ipk.sh
bash -n scripts/source-archive.sh

# JSON descriptors cannot contain comments because JSON has no comment syntax.
for file in package/root/usr/share/rpcd/acl.d/*.json \
            package/root/usr/share/vuci/menu.d/*.json \
            package/root/usr/share/vuci/path.d/*.json; do
  jq empty "$file"
done

# Validate optional language tooling when it is installed on the build host.
if command -v luac >/dev/null 2>&1; then
  luac -p package/root/usr/lib/lua/api/services/yachtsense_link_emulator.lua
fi
if command -v node >/dev/null 2>&1; then
  node --check package/root/www/views/services/YachtSenseLinkEmulator.js
fi

# Only the final VuCI view may exist in the package tree.
if [ -e package/root/www/views/services/YachtSenseLink.js ]; then
  echo 'Obsolete duplicate VuCI view YachtSenseLink.js was found.' >&2
  exit 1
fi

# Prevent accidental reintroduction of prototype migration or /usr/local copies.
if grep -RniE 'migrat(e|ion)|migrated_from|/usr/local/usr/share' package/control package/root; then
  echo 'Prototype migration or legacy-path code was found.' >&2
  exit 1
fi

# Build and inspect the actual RutOS-format release package. Teltonika's
# ipkg-build emits a gzip-compressed tar .ipk, so do not regress to Debian ar.
./scripts/build-ipk.sh
IPK="$(find dist -maxdepth 1 -name 'tlt_custom_pkg_yachtsense-link-emulator_*_arm_cortex-a7_neon-vfpv4.ipk' | sort | tail -n 1)"
test -n "$IPK"
tar -tzf "$IPK" | grep -Fxq './debian-binary'
tar -tzf "$IPK" | grep -Fxq './control.tar.gz'
tar -tzf "$IPK" | grep -Fxq './data.tar.gz'

TMPDIR_CHECK="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_CHECK"' EXIT
tar -xzf "$IPK" -C "$TMPDIR_CHECK"
tar -tzf "$TMPDIR_CHECK/data.tar.gz" | grep -Fxq './www/views/services/YachtSenseLinkEmulator.js.gz'
tar -xOzf "$TMPDIR_CHECK/control.tar.gz" ./control > "$TMPDIR_CHECK/control"
grep -Fxq 'Architecture: arm_cortex-a7_neon-vfpv4' "$TMPDIR_CHECK/control"
grep -Fxq 'Router: RUTX' "$TMPDIR_CHECK/control"
grep -Fxq 'tlt_name: yachtsense-link-emulator' "$TMPDIR_CHECK/control"

printf 'All checks passed for %s\n' "$IPK"
