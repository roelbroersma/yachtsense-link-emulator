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

# Build and inspect the actual release package as the final integration check.
./scripts/build-ipk.sh
IPK="$(find dist -maxdepth 1 -name 'tlt_custom_pkg_yachtsense-link-emulator_*.ipk' | sort | tail -n 1)"
ar t "$IPK" | grep -Fxq 'debian-binary'
ar t "$IPK" | grep -Fxq 'control.tar.gz'
ar t "$IPK" | grep -Fxq 'data.tar.gz'
TMPDIR_CHECK="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_CHECK"' EXIT
( cd "$TMPDIR_CHECK" && ar x "$ROOT/$IPK" )
tar -tzf "$TMPDIR_CHECK/data.tar.gz" | grep -Fxq './www/views/services/YachtSenseLinkEmulator.js.gz'

printf 'All checks passed for %s\n' "$IPK"
