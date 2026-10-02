#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
fixture=$(mktemp "$ROOT/web/tests/tailwind-source-XXXXXX.js")
trap 'rm -f -- "$fixture"' EXIT
# Comments in tests must not become production CSS or invalidate embedded bytes.
printf '// w-[98765px]\n' > "$fixture"
npm --prefix "$ROOT/web" run build
if grep -q '98765px' "$ROOT"/web/dist/assets/*.css; then
    echo 'production CSS scanned test-only content' >&2
    exit 1
fi
echo 'Production CSS excludes test-only source'
