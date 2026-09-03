#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

# The Go fixtures create all component paths below t.TempDir. This focused
# entrypoint deliberately never reads router paths or invokes a component.
go test -count=1 ./internal/components ./internal/httpapi

echo "component inventory fixtures passed"
