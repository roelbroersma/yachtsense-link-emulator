#!/usr/bin/env bash
# Wrap the single firmware-independent RUTX IPK in the small .tar.gz container
# expected by RutOS System -> Package Manager -> Upload.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
RELEASE="${RELEASE:-1}"
ARCH="${ARCH:-arm_cortex-a7_neon-vfpv4}"
ROUTER="${ROUTER:-RUTX}"
TLT_NAME="${TLT_NAME:-yachtsense-link-emulator}"
PACKAGE="tlt_custom_pkg_yachtsense-link-emulator"
PKG_VERSION="${VERSION}-${RELEASE}"
FIRMWARE="${1:-${RUTOS_FIRMWARE:-}}"
DIST="$ROOT/dist"
BUILD="$ROOT/build/package-manager"
IPK="$DIST/${PACKAGE}_${PKG_VERSION}_${ARCH}.ipk"

if [ -z "$FIRMWARE" ]; then
  echo 'Usage: scripts/build-pm-bundle.sh RUTX_R_00.07.xx[.x]' >&2
  exit 1
fi
case "$FIRMWARE" in
  RUTX_R_*) ;;
  *) echo "Expected a RUTX firmware string, got: $FIRMWARE" >&2; exit 1 ;;
esac

# The IPK itself is deliberately independent of RutOS patch releases. Build it
# once if it is not already present; every firmware wrapper embeds these exact
# same bytes.
if [ ! -f "$IPK" ]; then
  "$ROOT/scripts/build-ipk.sh"
fi

rm -rf "$BUILD"
mkdir -p "$BUILD/ipk" "$BUILD/wrapper"
cp "$IPK" "$BUILD/wrapper/"

tar -xzf "$IPK" -C "$BUILD/ipk"

# Teltonika's PM preparation step hashes data.tar.gz followed by control.tar.gz.
# This value belongs in main's ipk_file field. Unsigned local packages are shown
# by RutOS as Unauthorized but the Package Manager UI still allows proceeding.
CONTENT_HASH="$(cat "$BUILD/ipk/data.tar.gz" "$BUILD/ipk/control.tar.gz" | sha256sum | awk '{print $1}')"
IPK_BASENAME="$(basename "$IPK")"

cat > "$BUILD/wrapper/main" <<EOF
Package: $PACKAGE
Version: $PKG_VERSION
Router: $ROUTER
Firmware: $FIRMWARE
tlt_name: $TLT_NAME
ipk_file: $IPK_BASENAME:$CONTENT_HASH
ipk_deps:
EOF

FW_SUFFIX="${FIRMWARE##*_}"
OUTPUT="$DIST/yachtsense-link-emulator_${PKG_VERSION}_${ROUTER}_${FW_SUFFIX}.tar.gz"
rm -f "$OUTPUT" "$OUTPUT.sha256"
(
  cd "$BUILD/wrapper"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    --owner=0 --group=0 --numeric-owner \
    -cf - ./main "./$IPK_BASENAME" | gzip -9 -n > "$OUTPUT"
)
(
  cd "$DIST"
  sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256"
)

printf 'Built Package Manager wrapper for %s: %s\n' "$FIRMWARE" "$OUTPUT"
