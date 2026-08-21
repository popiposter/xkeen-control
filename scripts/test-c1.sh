#!/usr/bin/env bash
set -euo pipefail

cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
go test -count=1 ./internal/c1 ./internal/xrayapi ./internal/httpapi ./internal/nodes
