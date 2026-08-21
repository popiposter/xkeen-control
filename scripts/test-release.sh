#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
go test -count=1 ./internal/release ./internal/update ./internal/auth ./internal/httpapi ./cmd/xkeen-control ./cmd/xkeen-release
if grep -Eq 'xkeen[[:space:]]+-i' "$ROOT/scripts/install.sh"; then
	echo 'upstream interactive installer must not be invoked' >&2
	exit 1
fi
if grep -Eq 'opkg[[:space:]]+upgrade' "$ROOT/scripts/install.sh"; then
	echo 'blanket opkg upgrade is forbidden' >&2
	exit 1
fi
if grep -Eq 'releases/latest|latest/download' "$ROOT/scripts/install.sh"; then
	echo 'mutable latest release URLs are forbidden' >&2
	exit 1
fi
grep -Fq 'password bootstrap' "$ROOT/scripts/install.sh"
grep -Fq 'self-update --channel' "$ROOT/scripts/install.sh"
grep -Fq 'STABLE_RELEASE_VERSION' "$ROOT/scripts/install.sh"
grep -Fq 'full prerelease semver' "$ROOT/scripts/install.sh"
grep -Fq '/opt/etc/xkeen-control/previous/panel' "$ROOT/scripts/xkeen-control-updater"
grep -Fq 'panel-update' "$ROOT/scripts/xkeen-control-updater"
grep -Fq 'release-assets.githubusercontent.com' "$ROOT/scripts/install.sh"

bash "$ROOT/scripts/test-bootstrap.sh"
bash "$ROOT/scripts/test-updater.sh"
