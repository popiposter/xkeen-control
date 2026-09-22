#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

echo "== Phase B backup fixtures =="
go test -count=1 ./internal/backup ./internal/auth ./internal/httpapi ./internal/nodes ./internal/appliance

echo "Phase B backup fixtures passed"
