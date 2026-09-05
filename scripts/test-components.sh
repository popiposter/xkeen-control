#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

# The Go fixtures create all component paths below t.TempDir. The Phase C/D
# transaction tests use synthetic archives, geodata, binaries and runtimes
# only; this entrypoint deliberately never reads router paths or invokes a
# component.
go test -count=1 ./internal/components ./internal/httpapi
go test -race -count=1 ./internal/components

# Repeat the scaled late-recovery HTTP regression under race detection.
go test -race -count=5 -run '^TestComponentWriteWindowPreservesLateRecoveryHTTPResponse$' ./cmd/xkeen-control

# F1 exposes only the four authenticated manual backend routes. Keep policy,
# scheduler and automatic-mutation surfaces rejected at source level.
if grep -R -n -E '/api/v1/components/(policy|schedule|auto)' internal/httpapi; then
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
