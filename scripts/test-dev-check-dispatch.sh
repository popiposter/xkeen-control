#!/usr/bin/env bash
set -euo pipefail
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/scripts" "$work/packaging" "$work/bin"
cp "$ROOT/scripts/dev-check.sh" "$work/scripts/"
printf '#!/bin/sh\nexit 0\n' > "$work/scripts/test-public-hygiene.sh"
for command in go node npm; do
	printf '#!/bin/sh\nexit 0\n' > "$work/bin/$command"
	chmod +x "$work/bin/$command"
done
if (unset XKEEN_CHECK_GO XKEEN_CHECK_HELPERS XKEEN_CHECK_WEB XKEEN_CHECK_ARTIFACT; bash "$work/scripts/dev-check.sh" --fast) > "$work/missing.log" 2>&1; then
	echo "fast mode accepted missing selection" >&2; exit 1
fi
grep -q 'requires an explicit' "$work/missing.log"
printf '#!/bin/sh\nexit 0\n' > "$work/scripts/000-valid.sh"
printf '#!/bin/sh\nif true; then\n' > "$work/scripts/zzz-invalid.sh"
if PATH="$work/bin:$PATH" XKEEN_CHECK_GO=0 XKEEN_CHECK_HELPERS=1 XKEEN_CHECK_WEB=0 XKEEN_CHECK_ARTIFACT=0 bash "$work/scripts/dev-check.sh" --fast > "$work/syntax.log" 2>&1; then
	echo "syntax check missed a later file" >&2; exit 1
fi
grep -q 'zzz-invalid.sh' "$work/syntax.log"
echo "Development dispatch fixtures passed"
