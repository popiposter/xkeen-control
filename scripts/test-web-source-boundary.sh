#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
node --input-type=module - "$ROOT" <<'JS'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
const root = process.argv[2]
const decoder = new TextDecoder('utf-8', { fatal: true })
for (const area of ['src', 'tests', 'unit']) {
    const dir = join(root, 'web', area)
    for (const name of readdirSync(dir, { recursive: true })) {
        if (!/\.(?:jsx?|css)$/.test(name)) continue
        try { decoder.decode(readFileSync(join(dir, name))) }
        catch { throw new Error(`Frontend source must be UTF-8: web/${area}/${name}`) }
    }
}
console.log('Frontend source encoding is valid UTF-8')
JS
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
