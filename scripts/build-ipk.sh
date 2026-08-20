#!/usr/bin/env bash
# Build a reproducible ARMv7 RutOS IPK from the repository source tree.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
RELEASE="${RELEASE:-1}"
ARCH="ipq40xx"
PACKAGE="tlt_custom_pkg_yachtsense-link-emulator"
BUILD="$ROOT/build/ipk"
STAGE="$BUILD/data"
CONTROL="$BUILD/control"
DIST="$ROOT/dist"
OUTPUT="$DIST/${PACKAGE}_${VERSION}-${RELEASE}_${ARCH}.ipk"

# Start from a clean staging tree so deleted source files cannot leak into IPKs.
rm -rf "$BUILD"
mkdir -p "$STAGE" "$CONTROL" "$DIST" "$ROOT/build/bin"

# Produce a static ARMv7 executable suitable for the Qualcomm IPQ40xx RUTX line.
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath \
  -ldflags="-s -w -X main.packageVersion=${VERSION}" \
  -o "$ROOT/build/bin/yachtsense-link-emulator" \
  "$ROOT/cmd/yachtsense-link-emulator"

# Copy the package filesystem and add the freshly compiled native executable.
cp -a "$ROOT/package/root/." "$STAGE/"
install -D -m 0755 \
  "$ROOT/build/bin/yachtsense-link-emulator" \
  "$STAGE/usr/sbin/yachtsense-link-emulator"

# Add the compressed VuCI asset expected by RutOS web-server negotiation.
gzip -9 -n -c \
  "$ROOT/package/root/www/views/services/YachtSenseLinkEmulator.js" \
  > "$STAGE/www/views/services/YachtSenseLinkEmulator.js.gz"

# Calculate Installed-Size in KiB and render the control metadata placeholders.
INSTALLED_SIZE="$(du -sk "$STAGE" | awk '{print $1}')"
sed \
  -e "s/@VERSION@/${VERSION}/g" \
  -e "s/@RELEASE@/${RELEASE}/g" \
  -e "s/@INSTALLED_SIZE@/${INSTALLED_SIZE}/g" \
  "$ROOT/package/control/control.in" > "$CONTROL/control"
cp "$ROOT/package/control/conffiles" "$CONTROL/conffiles"
install -m 0755 "$ROOT/package/control/postinst" "$CONTROL/postinst"
install -m 0755 "$ROOT/package/control/prerm" "$CONTROL/prerm"
install -m 0755 "$ROOT/package/control/postrm" "$CONTROL/postrm"

# IPK files are ar archives containing debian-binary and two gzip-compressed tarballs.
printf '2.0\n' > "$BUILD/debian-binary"
(
  cd "$CONTROL"
  tar --sort=name --mtime='UTC 2020-01-01' --owner=0 --group=0 --numeric-owner \
    -czf "$BUILD/control.tar.gz" .
)
(
  cd "$STAGE"
  tar --sort=name --mtime='UTC 2020-01-01' --owner=0 --group=0 --numeric-owner \
    -czf "$BUILD/data.tar.gz" .
)

rm -f "$OUTPUT" "$OUTPUT.sha256"
(
  cd "$BUILD"
  ar crD "$OUTPUT" debian-binary control.tar.gz data.tar.gz
)
# Store only the package basename so verification works after downloading it.
(
  cd "$DIST"
  sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256"
)

printf 'Built %s\n' "$OUTPUT"
file "$ROOT/build/bin/yachtsense-link-emulator"
