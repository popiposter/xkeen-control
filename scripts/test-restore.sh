#!/usr/bin/env bash
set -euo pipefail

cd /workspace

echo "== Phase C1 restore fixtures =="
go test -count=1 ./internal/restore

echo "Phase C1 restore fixtures passed"
