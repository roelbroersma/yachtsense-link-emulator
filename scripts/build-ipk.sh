#!/usr/bin/env bash
# Build the firmware-independent RUTX IPK used by both SSH/opkg installs and
# the firmware-specific Package Manager wrapper archives.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
RELEASE="${RELEASE:-1}"
ARCH="${ARCH:-arm_cortex-a7_neon-vfpv4}"
PACKAGE="tlt_custom_pkg_yachtsense-link-emulator"
BUILD="$ROOT/build/ipk"
STAGE="$BUILD/data"
CONTROL="$BUILD/control"
DIST="$ROOT/dist"
PKG_VERSION="${VERSION}-${RELEASE}"
OUTPUT="$DIST/${PACKAGE}_${PKG_VERSION}_${ARCH}.ipk"

# Start clean so the generated IPK depends only on source and not on a previous
# firmware wrapper build. The actual daemon is static ARMv7 code.
rm -rf "$BUILD"
mkdir -p "$STAGE" "$CONTROL" "$DIST" "$ROOT/build/bin"

CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath \
  -ldflags="-s -w -X main.packageVersion=${VERSION}" \
  -o "$ROOT/build/bin/yachtsense-link-emulator" \
  "$ROOT/cmd/yachtsense-link-emulator"

cp -a "$ROOT/package/root/." "$STAGE/"
install -D -m 0755 \
  "$ROOT/build/bin/yachtsense-link-emulator" \
  "$STAGE/usr/sbin/yachtsense-link-emulator"

gzip -9 -n -c \
  "$ROOT/package/root/www/views/services/YachtSenseLinkEmulator.js" \
  > "$STAGE/www/views/services/YachtSenseLinkEmulator.js.gz"

# Match RutOS' ipkg-build layout: gzip-compressed data/control tar members in a
# gzip-compressed outer tar archive. Installed-Size is the compressed data size.
(
  cd "$STAGE"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    --owner=0 --group=0 --numeric-owner -cf - . | gzip -9 -n > "$BUILD/data.tar.gz"
)
INSTALLED_SIZE="$(stat -c '%s' "$BUILD/data.tar.gz")"

sed \
  -e "s/@VERSION@/${VERSION}/g" \
  -e "s/@RELEASE@/${RELEASE}/g" \
  -e "s/@INSTALLED_SIZE@/${INSTALLED_SIZE}/g" \
  "$ROOT/package/control/control.in" > "$CONTROL/control"
cp "$ROOT/package/control/conffiles" "$CONTROL/conffiles"

# RutOS packages do not put package-specific logic directly in postinst/prerm.
# The normal wrappers source /lib/functions.sh and call default_postinst/prerm;
# those functions then source postinst-pkg/prerm-pkg and perform the standard
# init-script, ACL, rpcd and VuCI route lifecycle handling.
cat > "$CONTROL/postinst" <<'EOF'
#!/bin/sh
[ "${IPKG_NO_SCRIPT:-}" = "1" ] && exit 0
[ -s "${IPKG_INSTROOT:-}/lib/functions.sh" ] || exit 0
. "${IPKG_INSTROOT:-}/lib/functions.sh"
default_postinst "$0" "$@"
EOF

cat > "$CONTROL/prerm" <<'EOF'
#!/bin/sh
[ -s "${IPKG_INSTROOT:-}/lib/functions.sh" ] || exit 0
. "${IPKG_INSTROOT:-}/lib/functions.sh"
default_prerm "$0" "$@"
EOF

install -m 0755 "$ROOT/package/control/postinst" "$CONTROL/postinst-pkg"
install -m 0755 "$ROOT/package/control/prerm" "$CONTROL/prerm-pkg"
install -m 0755 "$ROOT/package/control/postrm" "$CONTROL/postrm"
chmod 0755 "$CONTROL/postinst" "$CONTROL/prerm"

(
  cd "$CONTROL"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    --owner=0 --group=0 --numeric-owner -cf - . | gzip -9 -n > "$BUILD/control.tar.gz"
)
printf '2.0\n' > "$BUILD/debian-binary"

rm -f "$OUTPUT" "$OUTPUT.sha256"
(
  cd "$BUILD"
  tar --format=gnu --sort=name --mtime='UTC 2020-01-01' \
    --owner=0 --group=0 --numeric-owner \
    -cf - ./debian-binary ./data.tar.gz ./control.tar.gz | gzip -9 -n > "$OUTPUT"
)
(
  cd "$DIST"
  sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256"
)

printf 'Built firmware-independent RutOS IPK: %s\n' "$OUTPUT"
file "$ROOT/build/bin/yachtsense-link-emulator"
