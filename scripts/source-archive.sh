#!/usr/bin/env bash
# Create a source archive without generated build and distribution directories.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
OUTPUT="$ROOT/dist/yachtsense-link-emulator-${VERSION}-source.tar.gz"
mkdir -p "$ROOT/dist"
rm -f "$OUTPUT" "$OUTPUT.sha256"

tar \
  --sort=name \
  --mtime='UTC 2020-01-01' \
  --owner=0 \
  --group=0 \
  --numeric-owner \
  --exclude='./.git' \
  --exclude='./build' \
  --exclude='./dist' \
  --transform="s#^\.#yachtsense-link-emulator-${VERSION}#" \
  -czf "$OUTPUT" \
  -C "$ROOT" .

# Store only the archive basename so verification works after downloading it.
(
  cd "$ROOT/dist"
  sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256"
)
printf 'Built %s\n' "$OUTPUT"
