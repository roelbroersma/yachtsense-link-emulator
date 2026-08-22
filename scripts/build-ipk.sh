#!/usr/bin/env bash
# Build a reproducible ARMv7 RutOS package and, when a firmware version is
# supplied, the .tar.gz container expected by the RutOS Package Manager WebUI.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
RELEASE="${RELEASE:-1}"
ARCH="${ARCH:-arm_cortex-a7_neon-vfpv4}"
ROUTER="${ROUTER:-RUTX}"
TLT_NAME="${TLT_NAME:-yachtsense-link-emulator}"
RUTOS_FIRMWARE="${RUTOS_FIRMWARE:-}"
BUILD_PM_BUNDLE="${BUILD_PM_BUNDLE:-0}"
PACKAGE="tlt_custom_pkg_yachtsense-link-emulator"
BUILD="$ROOT/build/ipk"
STAGE="$BUILD/data"
CONTROL="$BUILD/control"
DIST="$ROOT/dist"
PKG_VERSION="${VERSION}-${RELEASE}"
OUTPUT="$DIST/${PACKAGE}_${PKG_VERSION}_${ARCH}.ipk"

# Package Manager offline installs are firmware-specific. Requiring the exact
# /etc/version value avoids producing a bundle that the router will reject.
if [ "$BUILD_PM_BUNDLE" = "1" ] && [ -z "$RUTOS_FIRMWARE" ]; then
  echo 'RUTOS_FIRMWARE is required for a RutOS Package Manager bundle.' >&2
  echo 'Use: RUTOS_FIRMWARE="$(cat /etc/version)" make package-manager' >&2
  exit 1
fi
if [ -n "$RUTOS_FIRMWARE" ] && [[ "$RUTOS_FIRMWARE" != *_* ]]; then
  echo "RUTOS_FIRMWARE must be the exact /etc/version value, got: $RUTOS_FIRMWARE" >&2
  exit 1
fi

# Start from a clean staging tree so deleted source files cannot leak into the
# package. The Go binary remains fully static; the RutOS architecture string is
# package metadata and maps to the ARMv7 RUTX family.
rm -rf "$BUILD"
mkdir -p "$STAGE" "$CONTROL" "$DIST" "$ROOT/build/bin"

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

# Teltonika's ipkg-build uses gzip-compressed tar members and stores the
# compressed data archive size in Installed-Size. Build data first so the
# control metadata can contain that exact value.
(
  cd "$STAGE"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    --owner=0 --group=0 --numeric-owner -cf - . | gzip -9 -n > "$BUILD/data.tar.gz"
)
INSTALLED_SIZE="$(stat -c '%s' "$BUILD/data.tar.gz")"
FIRMWARE_FIELD=''
if [ -n "$RUTOS_FIRMWARE" ]; then
  FIRMWARE_FIELD="Firmware: ${RUTOS_FIRMWARE}\\n"
fi

# Render the control metadata used both by opkg and the Package Manager.
sed \
  -e "s/@VERSION@/${VERSION}/g" \
  -e "s/@RELEASE@/${RELEASE}/g" \
  -e "s/@INSTALLED_SIZE@/${INSTALLED_SIZE}/g" \
  -e "s|@FIRMWARE_FIELD@|${FIRMWARE_FIELD}|g" \
  "$ROOT/package/control/control.in" > "$CONTROL/control"
cp "$ROOT/package/control/conffiles" "$CONTROL/conffiles"
install -m 0755 "$ROOT/package/control/postinst" "$CONTROL/postinst"
install -m 0755 "$ROOT/package/control/prerm" "$CONTROL/prerm"
install -m 0755 "$ROOT/package/control/postrm" "$CONTROL/postrm"

(
  cd "$CONTROL"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    --owner=0 --group=0 --numeric-owner -cf - . | gzip -9 -n > "$BUILD/control.tar.gz"
)
printf '2.0\n' > "$BUILD/debian-binary"

# RutOS' own ipkg-build emits .ipk files as gzip-compressed tar archives (not
# Debian ar archives). This is the format used by the offline Package Manager.
rm -f "$OUTPUT" "$OUTPUT.sha256"
(
  cd "$BUILD"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    -cf - ./debian-binary ./data.tar.gz ./control.tar.gz | gzip -9 -n > "$OUTPUT"
)
(
  cd "$DIST"
  sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256"
)

printf 'Built RutOS IPK: %s\n' "$OUTPUT"
file "$ROOT/build/bin/yachtsense-link-emulator"

# The WebUI upload endpoint expects a gzip tar containing the IPK plus a main
# metadata file. Its Firmware value must match the router's /etc/version.
if [ "$BUILD_PM_BUNDLE" = "1" ] || [ -n "$RUTOS_FIRMWARE" ]; then
  PM="$BUILD/package-manager"
  rm -rf "$PM"
  mkdir -p "$PM"
  cp "$OUTPUT" "$PM/"

  # This hash mirrors the package-content fingerprint carried by Teltonika's
  # generated main file. An independently built package is not signed by a
  # Teltonika repository key, so the WebUI may mark it as unverified while it
  # can still parse and install it as a custom package.
  CONTENT_HASH="$(cat "$BUILD/control.tar.gz" "$BUILD/data.tar.gz" | sha256sum | awk '{print $1}')"
  IPK_BASENAME="$(basename "$OUTPUT")"
  cat > "$PM/main" <<EOF
Package: $PACKAGE
Version: $PKG_VERSION
Architecture: $ARCH
Router: $ROUTER
Firmware: $RUTOS_FIRMWARE
tlt_name: $TLT_NAME
ipk_file: $IPK_BASENAME:$CONTENT_HASH
ipk_deps:
EOF

  FW_SUFFIX="${RUTOS_FIRMWARE##*_}"
  PM_OUTPUT="$DIST/yachtsense-link-emulator_${PKG_VERSION}_${ROUTER}_${FW_SUFFIX}.tar.gz"
  rm -f "$PM_OUTPUT" "$PM_OUTPUT.sha256"
  (
    cd "$PM"
    tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
      --owner=0 --group=0 --numeric-owner -cf - ./main "./$IPK_BASENAME" | gzip -9 -n > "$PM_OUTPUT"
  )
  (
    cd "$DIST"
    sha256sum "$(basename "$PM_OUTPUT")" > "$(basename "$PM_OUTPUT").sha256"
  )
  printf 'Built RutOS Package Manager bundle: %s\n' "$PM_OUTPUT"
fi
