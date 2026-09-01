#!/usr/bin/env bash
set -euo pipefail

cd /workspace

echo "== toolchain =="
go version
node --version
npm --version

echo "== Go tests =="
go test -count=1 ./...
go vet ./...
go test -race ./...

echo "== Benchmark policy fixtures =="
bash -n scripts/disable-legacy-speed-balancer.sh scripts/run-bounded-speed-benchmark.sh scripts/test-benchmark-policy.sh scripts/test-c1.sh scripts/install-performance-schedule.sh scripts/run-xkeen-foreground.sh scripts/test-xkeen-foreground.sh scripts/deploy.sh scripts/prepare-deploy-candidate.sh scripts/verify.sh scripts/install.sh scripts/xkeen-control-updater scripts/release-build.sh scripts/test-release.sh scripts/test-bootstrap.sh scripts/test-updater.sh scripts/test-public-hygiene.sh scripts/test-appliance.sh
bash scripts/test-benchmark-policy.sh
bash scripts/test-c1.sh
bash scripts/test-xkeen-foreground.sh
bash scripts/test-appliance.sh
bash scripts/test-release.sh
bash scripts/test-public-hygiene.sh

echo "== Web checks =="
npm --prefix web ci --prefer-offline
npm --prefix web run check
npm --prefix web audit --audit-level=high

echo "== Embedded web assets =="
find internal/webassets/dist -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -R web/dist/. internal/webassets/dist/
find internal/webassets/dist -type f -exec sed -i 's/\r$//' {} +

echo "== Artifact build =="
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
  -trimpath \
  -buildvcs=false \
  -ldflags='-s -w -X github.com/popiposter/xkeen-control/internal/buildinfo.Version=dev -X github.com/popiposter/xkeen-control/internal/buildinfo.Commit=dev -X github.com/popiposter/xkeen-control/internal/buildinfo.Channel=development' \
  -o dist/xkeen-control-linux-arm64 \
  ./cmd/xkeen-control

echo "== Repository hygiene =="
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git diff --check
else
  echo "git diff --check is run by scripts/dev-check.ps1 on the host for Windows worktrees"
fi
sha256sum dist/xkeen-control-linux-arm64
