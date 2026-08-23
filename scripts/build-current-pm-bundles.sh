#!/usr/bin/env bash
# Build WebUI upload wrappers for the current RUTX stable and latest firmware.
# Update these two values when Teltonika promotes/releases a new RUTX firmware.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUTOS_STABLE="${RUTOS_STABLE:-RUTX_R_00.07.24.1}"
RUTOS_LATEST="${RUTOS_LATEST:-RUTX_R_00.07.24.2}"

# Build the generic payload once. Both wrapper archives must contain the exact
# same IPK; only their top-level main metadata differs in Firmware:.
bash "$ROOT/scripts/build-ipk.sh"
bash "$ROOT/scripts/build-pm-bundle.sh" "$RUTOS_STABLE"

if [ "$RUTOS_LATEST" != "$RUTOS_STABLE" ]; then
  bash "$ROOT/scripts/build-pm-bundle.sh" "$RUTOS_LATEST"
fi

printf 'Current RUTX wrappers: stable=%s latest=%s\n' "$RUTOS_STABLE" "$RUTOS_LATEST"
