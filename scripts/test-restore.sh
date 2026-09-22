#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

echo "== Phase C1 restore fixtures =="
go test -count=1 ./internal/restore

echo "Phase C1 restore fixtures passed"
