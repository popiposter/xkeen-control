#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

# Setup fixtures use temporary fixed paths and synthetic metadata only. They do
# not download candidate bodies, invoke a lifecycle command, touch router
# paths, or exercise a live Setup Apply.
go test -count=1 ./internal/components -run '^TestSetup'
go test -count=1 ./internal/httpapi -run '^TestSetup'
go test -count=1 ./internal/nodes -run '^TestCommandActivatorVerifiesEmptySetupBaseline$'
go test -count=1 ./internal/appliance -run '^TestProductDefaultMatchesCheckedInPolicy$'

echo "setup fixtures passed"
