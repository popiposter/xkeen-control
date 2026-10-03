#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT/web"
mode="${1:---clean}"
case "$mode" in --clean|--reuse) ;; *) exit 2 ;; esac
stamp=node_modules/.xkeen-dependencies
identity=$( { sha256sum package.json package-lock.json; node --version; npm --version; node -p 'process.platform + "/" + process.arch'; } | sha256sum | cut -d' ' -f1)
if [ "$mode" = --reuse ] && [ -f "$stamp" ] && [ "$(cat "$stamp")" = "$identity" ] && npm ls --depth=0 --silent >/dev/null 2>&1; then
    echo 'Reusing matching local npm dependencies'
else
    npm ci --ignore-scripts --prefer-offline
    printf '%s\n' "$identity" > "$stamp"
fi
