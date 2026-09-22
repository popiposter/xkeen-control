#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

fixtures_only=false
if [ "${1:-}" = "--fixtures-only" ]; then
	fixtures_only=true
	shift
fi
[ "$#" -eq 0 ] || { echo "usage: $0 [--fixtures-only]" >&2; exit 2; }

if [ "$fixtures_only" = false ]; then
	# The Go fixtures create all component paths below t.TempDir. The Phase C/D
	# transaction tests use synthetic archives, geodata, binaries and runtimes
	# only; this entrypoint deliberately never reads router paths or invokes a
	# component.
	go test -count=1 ./internal/components ./internal/httpapi
	go test -race -count=1 ./internal/components

fi

# This repeated race regression is deliberate stress coverage, not a duplicate
# package sweep. Focused runs retain it; aggregate fast mode may omit it while
# aggregate full mode opts in explicitly.
if [ "$fixtures_only" = false ] || [ "${XKEEN_STRESS_RACE:-0}" = "1" ]; then
	go test -race -count=5 -run '^TestComponentWriteWindowPreservesLateRecoveryHTTPResponse$' ./cmd/xkeen-control
fi

# F3 exposes only the authenticated policy surface plus the existing manual
# backend routes. Keep generic scheduler and automatic-mutation surfaces
# rejected at source level.
if grep -R -n -E '/api/v1/components/(schedule|auto)' internal/httpapi; then
	echo "prohibited component lifecycle route detected" >&2
	exit 1
fi
if grep -R -n -E 'opkg[[:space:]]+upgrade|xkeen[[:space:]]+-u[xk]|xkeen[[:space:]]+-i|xkeen[[:space:]]+-fixed|Run[[:space:]]*\([[:space:]]*command' internal/components cmd/xkeen-control; then
	echo "prohibited Phase C/D command surface detected" >&2
	exit 1
fi
if grep -R -n -E 'update-geodata\.sh|gh-proxy\.com|ghfast\.top' internal/components cmd/xkeen-control; then
	echo "legacy geodata updater or proxy fallback referenced by Phase D" >&2
	exit 1
fi

echo "component fixtures passed"
