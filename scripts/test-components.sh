#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

# The Go fixtures create all component paths below t.TempDir. The Phase C
# transaction tests use synthetic archives, binaries and runtimes only; this
# entrypoint deliberately never reads router paths or invokes a component.
go test -count=1 ./internal/components ./internal/httpapi
go test -race -count=1 ./internal/components

# Phase C is an internal primitive. Keep its HTTP/UI mutation boundary closed
# and reject the prohibited operational surfaces at source level.
if grep -R -n -E '/api/v1/components/(preview|apply|rollback|policy)' internal/httpapi; then
	echo "Phase C HTTP mutation route detected" >&2
	exit 1
fi
if grep -R -n -E 'opkg[[:space:]]+upgrade|xkeen[[:space:]]+-u[xk]|xkeen[[:space:]]+-i|xkeen[[:space:]]+-fixed|Run[[:space:]]*\([[:space:]]*command' internal/components cmd/xkeen-control; then
	echo "prohibited Phase C command surface detected" >&2
	exit 1
fi

echo "component fixtures passed"
