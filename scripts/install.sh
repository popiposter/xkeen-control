#!/bin/sh
set -eu
set -f

# This is the public bootstrap entrypoint. The release URL, repository and
# asset names are constants; there is deliberately no URL or command input.
REPO="https://github.com/popiposter/xkeen-control"
# release-build.sh replaces this line in the published installer asset with
# the exact semver that owns the asset. The source checkout remains an
# explicit operator-gated template and never falls back to a mutable URL.
STABLE_RELEASE_VERSION=""
DOWNLOAD_ROOT=""
CHANNEL="${XKEEN_CONTROL_CHANNEL:-stable}"
VERSION="${XKEEN_CONTROL_VERSION:-}"
TEST_MODE="${XKEEN_CONTROL_TEST_MODE:-0}"
ROOT_PREFIX="${XKEEN_CONTROL_TEST_ROOT:-}"

# The only legacy generation eligible for published-installer adoption is the
# historical manual C.1 panel. These fingerprints are intentionally fixed in
# the release-owned installer; they are not operator or environment inputs.
LEGACY_PANEL_BINARY_SHA256="e8d9d02d0093c62ac4eb330cd28f15ee5fd640db2be4c7788b958f64cefdd937"
LEGACY_PANEL_INIT_SHA256="6dc8d8fd7315acd746e0729e2291dd198511cb9bbd214be6d13400a654813085"

if [ "$TEST_MODE" = "1" ]; then
	case "$ROOT_PREFIX" in
		/tmp/*) ;;
		*) echo "ERROR: test root must be an absolute /tmp path" >&2; exit 1 ;;
	esac
else
	[ -z "$ROOT_PREFIX" ] || { echo "ERROR: test root is not available in production mode" >&2; exit 1; }
	ROOT_PREFIX=""
fi

OPT_ROOT="${ROOT_PREFIX}/opt"
BIN="${OPT_ROOT}/sbin/xkeen-control"
INIT="${OPT_ROOT}/etc/init.d/S99xkeen-control"
UPDATER="${OPT_ROOT}/libexec/xkeen-control-updater"
ROOT_DIR="${OPT_ROOT}/etc/xkeen-control"
AUTH_DIR="${ROOT_DIR}/auth"
STATE_DIR="${ROOT_DIR}/state"
BOOTSTRAP_TMP_ROOT="${ROOT_PREFIX}/tmp/xkeen-control/panel-bootstrap"
UPDATE_TMP_ROOT="${ROOT_PREFIX}/tmp/xkeen-control/panel-update"
TMP_ROOT="$BOOTSTRAP_TMP_ROOT"
LEGACY_ADOPTION=0

fail() { echo "ERROR: $1" >&2; exit 1; }
if [ "$TEST_MODE" != "1" ]; then
	[ "$(id -u)" = "0" ] || fail "root is required"
fi
[ -d "$OPT_ROOT" ] || fail "/opt is required"
command -v opkg >/dev/null 2>&1 || fail "Entware opkg is required"
[ -w "$OPT_ROOT" ] || fail "/opt is not writable"

case "$(uname -m)" in
	aarch64|arm64) ;;
	*) fail "unsupported architecture; only linux/arm64 is supported" ;;
esac

free_kb="$(df -Pk "$OPT_ROOT" | awk 'NR == 2 { print $4 }')"
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

legacy_layout() {
	[ -f "$BIN" ] && [ -x "$BIN" ] && [ ! -L "$BIN" ] || return 1
	[ -f "$INIT" ] && [ -x "$INIT" ] && [ ! -L "$INIT" ] || return 1
	[ ! -e "$STATE_DIR/installed-release.json" ] && [ ! -L "$STATE_DIR/installed-release.json" ] || return 1
	[ ! -e "$UPDATER" ] && [ ! -L "$UPDATER" ] || return 1
	[ "$(sha256sum "$BIN" | awk '{print $1}')" = "$LEGACY_PANEL_BINARY_SHA256" ] || return 1
	[ "$(sha256sum "$INIT" | awk '{print $1}')" = "$LEGACY_PANEL_INIT_SHA256" ] || return 1
}

if [ -e "$BIN" ] || [ -L "$BIN" ]; then
	if legacy_layout; then
		LEGACY_ADOPTION=1
		TMP_ROOT="$UPDATE_TMP_ROOT"
	else
		[ -f "$BIN" ] && [ -x "$BIN" ] && [ ! -L "$BIN" ] || fail "existing panel path is not an executable managed install"
		[ -f "$INIT" ] && [ -x "$INIT" ] && [ ! -L "$INIT" ] || fail "managed panel init path is not trusted"
		if [ -e "$STATE_DIR/installed-release.json" ] || [ -L "$STATE_DIR/installed-release.json" ]; then
			[ -f "$STATE_DIR/installed-release.json" ] && [ ! -L "$STATE_DIR/installed-release.json" ] || fail "installed release marker is not trusted"
			jq -e '.product == "xkeen-control" and (.version | type == "string") and (.sourceCommit | type == "string") and (.channel == "stable" or .channel == "beta")' "$STATE_DIR/installed-release.json" >/dev/null 2>&1 || fail "installed release marker is not trusted"
		fi
		[ -f "$UPDATER" ] && [ -x "$UPDATER" ] && [ ! -L "$UPDATER" ] || fail "managed updater helper is missing or invalid"
		metadata="$($BIN version --json 2>/dev/null || true)"
		echo "$metadata" | jq -e '.product == "xkeen-control" and (.version | type == "string") and (.sourceCommit | type == "string")' >/dev/null 2>&1 || fail "existing panel identity is not trusted"
		# Existing installs use the installed binary's pinned-signature path. The
		# bootstrap never replaces a valid install from bootstrap-only HTTPS checks.
		if [ "$CHANNEL" = "beta" ] && [ -z "$VERSION" ]; then fail "beta rerun requires an explicit version"; fi
		if [ "$CHANNEL" = "stable" ] && [ -n "$VERSION" ]; then fail "stable rerun does not accept an arbitrary version"; fi
		if [ -n "$VERSION" ]; then
			exec "$BIN" self-update --channel "$CHANNEL" --apply "$VERSION"
		fi
		exec "$BIN" self-update --channel "$CHANNEL" --apply
	fi
fi

if [ ! -e "$BIN" ] && [ ! -L "$BIN" ]; then
	[ ! -e "$INIT" ] && [ ! -L "$INIT" ] && \
	[ ! -e "$UPDATER" ] && [ ! -L "$UPDATER" ] && \
	[ ! -e "$STATE_DIR/installed-release.json" ] && [ ! -L "$STATE_DIR/installed-release.json" ] || \
		fail "partial managed install is not eligible for bootstrap"
fi

if [ "$LEGACY_ADOPTION" = "1" ]; then
	[ "$CHANNEL" = "stable" ] || fail "legacy adoption requires the stable channel"
	[ -z "$VERSION" ] || fail "legacy adoption does not accept an arbitrary version"
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
	destination="$2"
	effective="$(curl --fail --silent --show-error --location --max-redirs 3 \
		--connect-timeout 10 --max-time 120 --proto '=https' --proto-redir '=https' \
		--user-agent 'xkeen-control-installer/1' --write-out '%{url_effective}' \
		"${DOWNLOAD_ROOT}/${name}" -o "$destination")" || fail "release download failed"
	case "$effective" in
		https://github.com/*|https://objects.githubusercontent.com/*|https://release-assets.githubusercontent.com/*|https://github-releases.githubusercontent.com/*) ;;
		*) rm -f "$destination"; fail "release redirect host is not supported" ;;
	esac
}

fetch release-manifest.json "$TMP_ROOT/release-manifest.json"
fetch release-manifest.sig "$TMP_ROOT/release-manifest.sig"
fetch SHA256SUMS "$TMP_ROOT/SHA256SUMS"
for name in xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater install.sh; do
	fetch "$name" "$TMP_ROOT/assets/$name"
done

jq -e --arg channel "$CHANNEL" --arg version "$EXPECTED_VERSION" '.schemaVersion == 1 and .product == "xkeen-control" and .version == $version and .channel == $channel and (.sourceCommit | type == "string" and length == 40) and .os == "linux" and .architecture == "arm64" and ((.artifacts | map(.name) | sort) == ["S99xkeen-control", "install.sh", "xkeen-control-linux-arm64", "xkeen-control-updater"])' "$TMP_ROOT/release-manifest.json" >/dev/null || fail "release manifest identity does not match bootstrap policy"

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
done <<EOF_MANIFEST
$(jq -r '.artifacts[] | [.name, (.size | tostring), .sha256] | @tsv' "$TMP_ROOT/release-manifest.json")
EOF_MANIFEST

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

if [ "$LEGACY_ADOPTION" = "1" ]; then
	# The fixed updater owns stop/swap/health/rollback. The installer only
	# moves the already-consistency-checked release into its fixed candidate
	# boundary and invokes that staged updater; it never installs a helper first.
	for name in xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater; do
		cp "$TMP_ROOT/assets/$name" "$TMP_ROOT/$name"
		chmod 755 "$TMP_ROOT/$name"
	done
	jq -c '{product,version,sourceCommit,channel}' "$TMP_ROOT/release-manifest.json" > "$TMP_ROOT/.installed-release.json.new" || fail "release marker could not be prepared"
	chmod 600 "$TMP_ROOT/.installed-release.json.new"
	mv -f "$TMP_ROOT/.installed-release.json.new" "$TMP_ROOT/installed-release.json"
	exec "$TMP_ROOT/xkeen-control-updater" install
fi

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
	XKEEN_CONTROL_AUTH_HASH="${AUTH_DIR}/password.bcrypt" \
	XKEEN_CONTROL_BOOTSTRAP_MARKER="${AUTH_DIR}/bootstrap-required" \
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
