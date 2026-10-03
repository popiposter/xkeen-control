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
# Exercise the actual web dispatcher independently of unrelated Go/helper lanes.
# A rejected advisory audit must prevent both build and browser work.
awk '/^run_web_checks\(\) \{/ {capture=1} capture {print} capture && /^}/ {exit}' "$ROOT/scripts/dev-check.sh" > "$work/web-dispatch.sh"
printf '#!/bin/sh\necho dependencies >> trace\n' > "$work/scripts/web-dependencies.sh"
printf '#!/bin/sh\necho audit >> trace\nexit 42\n' > "$work/scripts/npm-audit.sh"
for script in test-web-source-boundary verify-webassets; do
	printf '#!/bin/sh\necho build >> trace\n' > "$work/scripts/$script.sh"
done
printf '#!/bin/sh\necho npm >> trace\n' > "$work/bin/npm"
set +e
(cd "$work"; PATH="$work/bin:$PATH" bash -e -c '. ./web-dispatch.sh; mode=--full; run_web_checks') > "$work/audit.log" 2>&1
audit_status=$?
set -e
test "$audit_status" -eq 42
test "$(cat "$work/trace")" = "$(printf 'dependencies\naudit')"
echo "Development dispatch fixtures passed"
