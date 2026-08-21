#!/bin/sh
set -eu

# This is the public bootstrap entrypoint. The release URL, repository and
# asset names are constants; there is deliberately no URL or command input.
REPO="https://github.com/popiposter/xkeen-control"
# release-build.sh replaces this line in the published installer asset with
# the exact stable semver that owns the asset. The source checkout remains an
# explicit operator-gated template and never falls back to a mutable URL.
STABLE_RELEASE_VERSION=""
DOWNLOAD_ROOT=""
CHANNEL="${XKEEN_CONTROL_CHANNEL:-stable}"
VERSION="${XKEEN_CONTROL_VERSION:-}"
BIN="/opt/sbin/xkeen-control"
INIT="/opt/etc/init.d/S99xkeen-control"
UPDATER="/opt/libexec/xkeen-control-updater"
ROOT_DIR="/opt/etc/xkeen-control"
AUTH_DIR="${ROOT_DIR}/auth"
STATE_DIR="${ROOT_DIR}/state"
TMP_ROOT="/tmp/xkeen-control/panel-bootstrap"

fail() { echo "ERROR: $1" >&2; exit 1; }
[ "$(id -u)" = "0" ] || fail "root is required"
[ -d /opt ] || fail "/opt is required"
command -v opkg >/dev/null 2>&1 || fail "Entware opkg is required"
[ -w /opt ] || fail "/opt is not writable"

case "$(uname -m)" in
	aarch64|arm64) ;;
	*) fail "unsupported architecture; only linux/arm64 is supported" ;;
esac

free_kb="$(df -Pk /opt | awk 'NR == 2 { print $4 }')"
case "$free_kb" in ''|*[!0-9]*) fail "unable to determine free space" ;; esac
[ "$free_kb" -ge 131072 ] || fail "at least 128 MiB free space is required"

need_tool() {
	tool="$1"
	package="$2"
	if command -v "$tool" >/dev/null 2>&1; then return 0; fi
	if [ "${UPDATED:-0}" != "1" ]; then
		opkg update >/dev/null 2>&1 || fail "package index update failed"
		UPDATED=1
		export UPDATED
	fi
	opkg install "$package" >/dev/null 2>&1 || fail "required prerequisite is unavailable: $package"
	command -v "$tool" >/dev/null 2>&1 || fail "required prerequisite is unavailable: $tool"
}

# Only explicitly missing prerequisites are installed. A blanket package
# upgrade is intentionally absent from this bootstrap.
need_tool curl curl
need_tool jq jq
need_tool sha256sum coreutils-sha256sum

if [ -e "$BIN" ]; then
	[ -x "$BIN" ] || fail "existing panel path is not an executable managed install"
	metadata="$($BIN version --json 2>/dev/null || true)"
	echo "$metadata" | jq -e '.product == "xkeen-control" and (.version | type == "string") and (.sourceCommit | type == "string")' >/dev/null 2>&1 || fail "existing panel identity is not trusted"
	# Existing installs use the installed binary's pinned-signature path. The
	# bootstrap never replaces a valid install from mutable bootstrap URLs.
	if [ "$CHANNEL" = "beta" ] && [ -z "$VERSION" ]; then fail "beta rerun requires an explicit version"; fi
	if [ "$CHANNEL" = "stable" ] && [ -n "$VERSION" ]; then fail "stable rerun does not accept an arbitrary version"; fi
	if [ -n "$VERSION" ]; then
		exec "$BIN" self-update --channel "$CHANNEL" --apply "$VERSION"
	fi
	exec "$BIN" self-update --channel "$CHANNEL" --apply
fi

case "$CHANNEL" in
	stable)
		[ -z "$VERSION" ] || fail "stable bootstrap does not accept an arbitrary version"
	[ -n "$STABLE_RELEASE_VERSION" ] || fail "stable installer is not pinned to a qualified release"
	DOWNLOAD_ROOT="${REPO}/releases/download/v${STABLE_RELEASE_VERSION}"
	EXPECTED_VERSION="$STABLE_RELEASE_VERSION"
		;;
	beta)
		[ -n "$VERSION" ] || fail "beta bootstrap requires an explicit version"
		printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$' || fail "beta version must be a full prerelease semver"
		DOWNLOAD_ROOT="${REPO}/releases/download/v${VERSION}"
		EXPECTED_VERSION="$VERSION"
		;;
	*) fail "channel must be stable or beta" ;;
esac

rm -rf "$TMP_ROOT"
mkdir -p "$TMP_ROOT/assets" "$AUTH_DIR" "$STATE_DIR"
chmod 700 "$TMP_ROOT" "$TMP_ROOT/assets" "$ROOT_DIR" "$AUTH_DIR" "$STATE_DIR"

fetch() {
	name="$1"
	curl --fail --silent --show-error --location --max-redirs 3 \
		--connect-timeout 10 --max-time 120 --proto '=https' --proto-redir '=https' \
		--user-agent 'xkeen-control-installer/1' \
		"${DOWNLOAD_ROOT}/${name}" -o "${TMP_ROOT}/${name}"
}

fetch release-manifest.json
fetch release-manifest.sig
fetch SHA256SUMS
for name in xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater install.sh; do fetch "$name"; done

jq -e --arg channel "$CHANNEL" --arg version "$EXPECTED_VERSION" '.schemaVersion == 1 and .product == "xkeen-control" and .version == $version and .channel == $channel and .os == "linux" and .architecture == "arm64" and ((.artifacts | map(.name) | sort) == ["S99xkeen-control", "install.sh", "xkeen-control-linux-arm64", "xkeen-control-updater"])' "$TMP_ROOT/release-manifest.json" >/dev/null || fail "release manifest identity does not match bootstrap policy"

# First-install trust starts at GitHub HTTPS. Internal manifest, size and hash
# consistency is checked before any asset is installed; the downloaded panel
# is not executed to verify its own release signature.
while IFS="$(printf '\t')" read -r name size hash; do
	case "$name" in
		xkeen-control-linux-arm64|S99xkeen-control|xkeen-control-updater|install.sh) ;;
		*) fail "release manifest contains an unexpected asset" ;;
	esac
	file="$TMP_ROOT/assets/$name"
	[ -f "$file" ] || fail "release asset is missing"
	actual_size="$(wc -c < "$file" | tr -d '[:space:]')"
	[ "$actual_size" = "$size" ] || fail "release asset size mismatch"
	actual_hash="$(sha256sum "$file" | awk '{print $1}')"
	[ "$actual_hash" = "$hash" ] || fail "release asset hash mismatch"
done <<EOF
$(jq -r '.artifacts[] | [.name, (.size | tostring), .sha256] | @tsv' "$TMP_ROOT/release-manifest.json")
EOF

sum_count=0
sum_names=""
while read -r expected name; do
	case "$name" in
		release-manifest.json|release-manifest.sig) file="$TMP_ROOT/$name" ;;
		xkeen-control-linux-arm64|S99xkeen-control|xkeen-control-updater|install.sh) file="$TMP_ROOT/assets/$name" ;;
		*) fail "checksum list contains an unexpected file" ;;
	esac
	case " $sum_names " in
		*" $name "*) fail "checksum list contains a duplicate file" ;;
	esac
	actual_hash="$(sha256sum "$file" | awk '{print $1}')"
	[ "$actual_hash" = "$expected" ] || fail "SHA256SUMS verification failed"
	sum_count=$((sum_count + 1))
	sum_names="$sum_names $name"
done < "$TMP_ROOT/SHA256SUMS"
[ "$sum_count" -eq 6 ] || fail "checksum list is incomplete"

mkdir -p "$(dirname "$BIN")" "$(dirname "$INIT")" "$(dirname "$UPDATER")"
cp "$TMP_ROOT/assets/xkeen-control-linux-arm64" "$BIN.new"
cp "$TMP_ROOT/assets/S99xkeen-control" "$INIT.new"
cp "$TMP_ROOT/assets/xkeen-control-updater" "$UPDATER.new"
chmod 755 "$BIN.new" "$INIT.new" "$UPDATER.new"
mv -f "$BIN.new" "$BIN"
mv -f "$INIT.new" "$INIT"
mv -f "$UPDATER.new" "$UPDATER"
rm -rf "$TMP_ROOT"

if [ ! -e "${AUTH_DIR}/password.bcrypt" ]; then
	"$BIN" password bootstrap
fi

"$INIT" start >/dev/null
health="$(curl --fail --silent --show-error --max-time 10 http://127.0.0.1:8787/healthz)" || fail "panel health check failed"
[ "$health" = "ok" ] || fail "panel health response was not generic ok"
metadata="$($BIN version --json)" || fail "installed build metadata is unavailable"
echo "$metadata" | jq -e '.product == "xkeen-control" and (.sourceCommit | type == "string") and (.sourceCommit | length == 40)' >/dev/null || fail "installed build provenance is invalid"
echo "xkeen-control installed: $metadata"
echo "Management listener remains loopback by default; use an SSH tunnel unless an exact private address is configured explicitly."
echo "XKeen/Xray/configuration readiness is reported by the authenticated panel. Missing components remain Setup Mode; no upstream interactive installer was invoked."
