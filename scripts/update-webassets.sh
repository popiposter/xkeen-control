#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

npm --prefix web ci --ignore-scripts --prefer-offline
npm --prefix web run build
find internal/webassets/dist -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -R web/dist/. internal/webassets/dist/
find internal/webassets/dist -type f -exec sed -i 's/\r$//' {} +
echo "updated tracked embedded web assets; review the resulting diff"
