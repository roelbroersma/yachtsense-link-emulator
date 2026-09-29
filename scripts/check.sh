#!/usr/bin/env bash
# Validate source and actual release archives without touching a router.
set -euo pipefail
cd "$(dirname "$0")/.."
go test -race -count=1 ./...
go vet ./...
node --check --input-type=module < package/root/www/views/services/YachtSenseLinkEmulatorV1120.js
node tests/ui_test.mjs
"${LUA_TEST:-texlua}" tests/lua_adapter_test.lua
"${LUA_TEST:-texlua}" tests/rpc_helper_test.lua
python3 scripts/build.py --firmware RUTX_R_00.07.25.3
python3 tests/package_test.py
python3 tests/builder_test.py
