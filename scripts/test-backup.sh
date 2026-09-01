#!/usr/bin/env bash
set -euo pipefail

cd /workspace

echo "== Phase B backup fixtures =="
go test -count=1 ./internal/backup ./internal/auth ./internal/httpapi ./internal/nodes ./internal/appliance

echo "Phase B backup fixtures passed"
