#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
diff -ru --strip-trailing-cr "$ROOT/web/dist" "$ROOT/internal/webassets/dist"
echo "embedded web assets match the production build"
