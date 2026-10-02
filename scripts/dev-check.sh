#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

mode="${1:---fast}"
case "$mode" in
	--fast|--full) ;;
	*) echo "usage: $0 [--fast|--full]" >&2; exit 2 ;;
esac

if [ "$mode" = --fast ]; then
	for name in XKEEN_CHECK_GO XKEEN_CHECK_HELPERS XKEEN_CHECK_WEB XKEEN_CHECK_ARTIFACT; do
		case "${!name:-}" in
			0|1) ;;
			*) echo "fast mode requires an explicit 0/1 selection for $name; use scripts/dev-check.ps1" >&2; exit 2 ;;
		esac
	done
fi

echo "== Repository hygiene =="
bash scripts/test-public-hygiene.sh

lane_enabled() {
	local value="${!1:-0}"
	[ "$mode" = "--full" ] || [ "$value" = "1" ]
}

run_shell_fixtures() {
	echo "== Unique shell and integration fixtures =="
	for script in scripts/*.sh scripts/xkeen-control-updater packaging/S99xkeen-control; do
		bash -n "$script"
	done
	bash scripts/test-dev-check-dispatch.sh
	bash scripts/test-build-embedded.sh
	bash scripts/test-web-dependencies.sh
	node --test scripts/dev-check-go.test.mjs
	node --test scripts/native-admission-entry.test.mjs scripts/native-admission-verify.test.mjs scripts/native-admission-hook-verify.test.mjs
	bash scripts/test-keenetic-env.sh
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
	if [ "$mode" = --full ]; then
		bash scripts/web-dependencies.sh --clean
	else
		bash scripts/web-dependencies.sh --reuse
	fi
	if [ "$mode" = --full ] || [ "${XKEEN_CHECK_WEB_BUILD:-1}" != 0 ]; then
		bash scripts/test-web-source-boundary.sh
		bash scripts/verify-webassets.sh
	else
		echo 'Web source unchanged: production build and embedded comparison omitted'
	fi
	npm --prefix web run test:unit
	if [ "$mode" = "--full" ]; then
		if [ "${XKEEN_PLAYWRIGHT_INSTALL:-0}" = "1" ]; then
			(cd web && npx playwright install --with-deps chromium)
		fi
		npm --prefix web run test:ui
		bash scripts/npm-audit.sh
	elif [ "${XKEEN_CHECK_UI-*}" = '*' ]; then
		npm --prefix web run test:ui
	elif [ -n "${XKEEN_CHECK_UI:-}" ]; then
		read -r -a specs <<< "$XKEEN_CHECK_UI"
		npm --prefix web run test:ui -- "${specs[@]}"
	fi
}

echo "== toolchain =="
go version
node --version
npm --version
echo "qualification mode: ${mode#--}"
echo "selected lanes: go=${XKEEN_CHECK_GO:-0} helpers=${XKEEN_CHECK_HELPERS:-0} web=${XKEEN_CHECK_WEB:-0} artifact=${XKEEN_CHECK_ARTIFACT:-0}"

if lane_enabled XKEEN_CHECK_GO; then
	echo "== Go tests =="
	packages=(./...)
	if [ "$mode" = --fast ]; then
		mapfile -t packages < <(node scripts/dev-check-go.mjs)
		[ "${#packages[@]}" -gt 0 ] || { echo 'empty Go plan' >&2; exit 1; }
	fi
	printf 'Selected Go packages: %s\n' "${packages[*]}"
	go test -count=1 "${packages[@]}"
	go vet "${packages[@]}"
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

echo "git diff --check is run by scripts/dev-check.ps1 on the host"
