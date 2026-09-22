#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

mode="${1:---fast}"
case "$mode" in
	--fast|--full) ;;
	*) echo "usage: $0 [--fast|--full]" >&2; exit 2 ;;
esac

lane_enabled() {
	local value="${!1:-0}"
	[ "$mode" = "--full" ] || [ "$value" = "1" ]
}

run_shell_fixtures() {
	echo "== Unique shell and integration fixtures =="
	bash -n scripts/*.sh scripts/xkeen-control-updater
	bash scripts/test-benchmark-policy.sh
	bash scripts/test-xkeen-foreground.sh
	if [ "$mode" = "--full" ]; then
		XKEEN_STRESS_RACE=1 bash scripts/test-components.sh --fixtures-only
	else
		bash scripts/test-components.sh --fixtures-only
	fi
	bash scripts/test-appliance.sh
	bash scripts/test-release.sh --fixtures-only
}

run_web_checks() {
	echo "== Web checks =="
	npm --prefix web ci --ignore-scripts --prefer-offline
	npm --prefix web run build
	if [ "$mode" = "--full" ]; then
		if [ "${XKEEN_PLAYWRIGHT_INSTALL:-0}" = "1" ]; then
			(cd web && npx playwright install --with-deps chromium)
		fi
		npm --prefix web run test:ui
		bash scripts/npm-audit.sh
	fi
	bash scripts/verify-webassets.sh
}

echo "== toolchain =="
go version
node --version
npm --version
echo "qualification mode: ${mode#--}"
echo "selected lanes: go=${XKEEN_CHECK_GO:-0} helpers=${XKEEN_CHECK_HELPERS:-0} web=${XKEEN_CHECK_WEB:-0} artifact=${XKEEN_CHECK_ARTIFACT:-0}"

if lane_enabled XKEEN_CHECK_GO; then
	echo "== Go tests =="
	go test -count=1 ./...
	go vet ./...
	if [ "$mode" = "--full" ]; then
		go test -race ./...
	fi
fi

if lane_enabled XKEEN_CHECK_HELPERS; then
	run_shell_fixtures
fi

if lane_enabled XKEEN_CHECK_WEB; then
	run_web_checks
fi

if lane_enabled XKEEN_CHECK_ARTIFACT; then
	echo "== Artifact build =="
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
	  -trimpath \
	  -buildvcs=false \
	  -ldflags='-s -w -X github.com/popiposter/xkeen-control/internal/buildinfo.Version=dev -X github.com/popiposter/xkeen-control/internal/buildinfo.Commit=dev -X github.com/popiposter/xkeen-control/internal/buildinfo.Channel=development' \
	  -o dist/xkeen-control-linux-arm64 \
	  ./cmd/xkeen-control
	sha256sum dist/xkeen-control-linux-arm64
fi

echo "== Repository hygiene =="
bash scripts/test-public-hygiene.sh
echo "git diff --check is run by scripts/dev-check.ps1 on the host"
