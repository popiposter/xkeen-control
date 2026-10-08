#!/bin/sh
set -eu
set -f

INSTALL_MODE=panel
case "${1:-}" in
	'') [ "$#" -eq 0 ] || exit 2 ;;
	--setup) [ "$#" -eq 1 ] || exit 2; INSTALL_MODE=setup ;;
	--setup-panel) [ "$#" -eq 1 ] || exit 2; INSTALL_MODE=setup-panel ;;
	*) echo 'usage: install.sh [--setup]' >&2; exit 2 ;;
esac

PATH="/opt/bin:/opt/sbin:${PATH:-/usr/sbin:/usr/bin:/sbin:/bin}"
export PATH

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
LEGACY_PANEL_BINARY_SHA256="89f7ef1a75b1f928e998fdfa7a8b6ea86e4d544534625c9b483104a31b5d4826"
LEGACY_PANEL_INIT_SHA256="2976e5ddaa076031e416d77da5ae865a921a7b00692c8000bf8d71a16927475d"

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
if [ "$INSTALL_MODE" = setup ]; then TMP_ROOT="${ROOT_PREFIX}/tmp/xkeen-control/guided-bootstrap"; fi
LEGACY_ADOPTION=0

fail() { echo "ERROR: $1" >&2; exit 1; }
# The internal deferred branch must have an inherited owner descriptor before
# dependency installation or destination creation. The verified binary repeats
# inode/root/receipt validation before placement; this is only an early fence.
if [ "$INSTALL_MODE" = setup-panel ]; then
	[ "$(readlink /proc/self/fd/3 2>/dev/null || :)" = /opt/var/lock/xkeen-control/initial-setup.lock ] || fail "setup owner descriptor required"
	[ -f /opt/etc/xkeen-control/state/initial-setup.json ] && [ ! -L /opt/etc/xkeen-control/state/initial-setup.json ] || fail "setup receipt required"
fi
if [ "$TEST_MODE" != "1" ]; then
	[ "$(id -u)" = "0" ] || fail "root is required"
fi
[ -d "$OPT_ROOT" ] || fail "/opt is required"
command -v opkg >/dev/null 2>&1 || fail "Entware opkg is required"
[ -w "$OPT_ROOT" ] || fail "/opt is not writable"

case "$(uname -m)" in
	aarch64|arm64) ARCHITECTURE=arm64 ;;
	mips|mipsel|mipsle)
		# uname does not establish MIPS byte order/ABI. Admit only the observed
		# Entware soft-float little-endian target, never guess from its spelling.
		entware_architectures="$(opkg print-architecture)" || fail "unable to determine MIPS Entware target"
		printf '%s\n' "$entware_architectures" | grep -Eq '^arch mipsel-3\.4(_kn)? [0-9]+$' || fail "unsupported MIPS Entware target"
		ARCHITECTURE=mipsle ;;
	*) fail "unsupported architecture" ;;
esac
BINARY_ASSET="xkeen-control-linux-$ARCHITECTURE"
MANIFEST_ASSET=release-manifest.json
SIGNATURE_ASSET=release-manifest.sig
if [ "$ARCHITECTURE" = mipsle ]; then
	[ "$INSTALL_MODE" = panel ] || fail "guided setup is ARM64-only"
	MANIFEST_ASSET=release-manifest-mipsle.json
	SIGNATURE_ASSET=release-manifest-mipsle.sig
fi

if [ "$INSTALL_MODE" = setup ]; then
	command -v flock >/dev/null 2>&1 && command -v stat >/dev/null 2>&1 || fail "setup requires flock and stat before bootstrap"
	# Root-only fixed parents prevent an untrusted rename between checks/open.
	# The Go owner repeats descriptor/inode validation and adopts FD3. Never
	# unlink this inode or close it across the launcher -> setup handoff.
	for dir in "$OPT_ROOT" "$OPT_ROOT/var" "$OPT_ROOT/var/lock" "$OPT_ROOT/var/lock/xkeen-control"; do
		if [ ! -e "$dir" ] && [ ! -L "$dir" ]; then (umask 077; mkdir "$dir") || fail "setup lock directory unavailable"; fi
		[ -d "$dir" ] && [ ! -L "$dir" ] && [ "$(stat -c %u "$dir")" = 0 ] || fail "unsafe setup lock directory"
		mode=$(stat -c %a "$dir")
		case "$mode" in ''|*[!0-7]*) fail "unsafe setup directory mode";; esac
		[ "$((0$mode & 0022))" -eq 0 ] || fail "writable setup lock directory"
	done
	LOCK_PATH="$OPT_ROOT/var/lock/xkeen-control/initial-setup.lock"
	[ "$(stat -c %a "$OPT_ROOT/var/lock/xkeen-control")" = 700 ] || fail "setup lock directory must be private"
	if [ -e "$LOCK_PATH" ] || [ -L "$LOCK_PATH" ]; then
		[ -f "$LOCK_PATH" ] && [ ! -L "$LOCK_PATH" ] && [ "$(stat -c '%u:%a:%h' "$LOCK_PATH")" = 0:600:1 ] || fail "unsafe setup lock file"
	fi
	umask 077
	exec 3>>"$LOCK_PATH"
	flock -n -x 3 || fail "setup or panel is already running"
elif [ "$INSTALL_MODE" = panel ] && { [ -e "$STATE_DIR/initial-setup.json" ] || [ -L "$STATE_DIR/initial-setup.json" ]; }; then
	[ -x "$BIN" ] || fail "incomplete initial setup; use setup inspect"
	"$BIN" setup guard || fail "initial setup blocks ordinary installation"
fi

free_kb="$(df -Pk "$OPT_ROOT" | awk 'NR == 2 { print $4 }')"
case "$free_kb" in ''|*[!0-9]*) fail "unable to determine free space" ;; esac
[ "$free_kb" -ge 131072 ] || fail "at least 128 MiB free space is required"

need_tool() {
	tool="$1"
	package="$2"
	if command -v "$tool" >/dev/null 2>&1; then return 0; fi
	[ "$INSTALL_MODE" != setup ] || fail "setup download requires Entware $tool before entering the installer"
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

validate_buildinfo_json() {
	jq -e -s '
		def identifier_valid($value; $prerelease):
			if ($value | type) != "string" or $value == "" then false
			elif (($value | test("^[A-Za-z0-9-]+$")) | not) then false
			elif $prerelease and (($value | test("^[0-9]+$")) == true) and ($value | length) > 1 and (($value | startswith("0")) == true) then false
			else true
			end;
		def identifiers_valid($value; $prerelease):
			if ($value | type) != "string" or $value == "" then false
			else ($value | split(".") | map(identifier_valid(.; $prerelease)) | all)
			end;
		def numeric_valid($value):
			if ($value | type) != "string" or $value == "" or (($value | test("^[0-9]+$")) | not) then false
			elif ($value | length) > 1 and (($value | startswith("0")) == true) then false
			else true
			end;
		def core_valid($value):
			if ($value | type) != "string" then false
			else ($value | split(".")) as $parts
				| (($parts | length) == 3 and ($parts | map(numeric_valid(.)) | all))
			end;
		def semver_valid($input):
			if ($input | type) != "string" then false
			else
				(if ($input | startswith("v")) then $input[1:] else $input end) as $value
				| if $value == "" or (($value | test("[ /\\\\]")) == true) then false
				  else ($value | split("+")) as $parts
					| if ($parts | length) > 2 or $parts[0] == "" then false
					  elif ($parts | length) == 2 and ((identifiers_valid($parts[1]; false)) | not) then false
					  else $parts[0] as $core_and_prerelease
						| if ($core_and_prerelease | contains("-")) then
							($core_and_prerelease | index("-")) as $separator
							| ($core_and_prerelease[0:$separator]) as $core
							| ($core_and_prerelease[($separator + 1):]) as $prerelease
							| (core_valid($core) and identifiers_valid($prerelease; true))
						  else core_valid($core_and_prerelease)
						  end
					  end
				  end
			end;
		def commit_valid($value):
			if ($value | type) != "string" then false
			else (($value | test("^[0-9a-f]{40}$")) == true)
			end;
		def info_valid:
			if type != "object" or .product != "xkeen-control" then false
			elif (.version | type) != "string" or (.sourceCommit | type) != "string" then false
			elif (.channel | type) != "string" then false
			elif .channel == "development" then ((.sourceCommit == "dev" or commit_valid(.sourceCommit)) and (.version == "dev" or (.version | test("^[0-9a-f]{7,40}$")) or semver_valid(.version)))
			elif .channel == "stable" then (semver_valid(.version) and commit_valid(.sourceCommit) and ((.version | contains("-")) | not))
			elif .channel == "beta" then (semver_valid(.version) and commit_valid(.sourceCommit) and (.version | contains("-")))
			else false
			end;
		(length == 1) and (.[0] | info_valid)
	' >/dev/null 2>&1
}

legacy_layout() {
	[ "$ARCHITECTURE" = arm64 ] || return 1
	[ -f "$BIN" ] && [ -x "$BIN" ] && [ ! -L "$BIN" ] || return 1
	[ -f "$INIT" ] && [ -x "$INIT" ] && [ ! -L "$INIT" ] || return 1
	[ ! -e "$STATE_DIR/installed-release.json" ] && [ ! -L "$STATE_DIR/installed-release.json" ] || return 1
	[ ! -e "$UPDATER" ] && [ ! -L "$UPDATER" ] || return 1
	[ "$(sha256sum "$BIN" | awk '{print $1}')" = "$LEGACY_PANEL_BINARY_SHA256" ] || return 1
	[ "$(sha256sum "$INIT" | awk '{print $1}')" = "$LEGACY_PANEL_INIT_SHA256" ] || return 1
}

if [ -e "$BIN" ] || [ -L "$BIN" ]; then
	[ "$INSTALL_MODE" != setup ] || fail "guided setup requires a fresh router; existing installation is inspect-only"
	if legacy_layout; then
		LEGACY_ADOPTION=1
		TMP_ROOT="$UPDATE_TMP_ROOT"
	else
		[ -f "$BIN" ] && [ -x "$BIN" ] && [ ! -L "$BIN" ] || fail "existing panel path is not an executable managed install"
		[ -f "$INIT" ] && [ -x "$INIT" ] && [ ! -L "$INIT" ] || fail "managed panel init path is not trusted"
		if [ -e "$STATE_DIR/installed-release.json" ] || [ -L "$STATE_DIR/installed-release.json" ]; then
			[ -f "$STATE_DIR/installed-release.json" ] && [ ! -L "$STATE_DIR/installed-release.json" ] || fail "installed release marker is not trusted"
			validate_buildinfo_json < "$STATE_DIR/installed-release.json" || fail "installed release marker is not trusted"
		fi
		[ -f "$UPDATER" ] && [ -x "$UPDATER" ] && [ ! -L "$UPDATER" ] || fail "managed updater helper is missing or invalid"
		metadata="$($BIN version --json 2>/dev/null || true)"
		printf '%s\n' "$metadata" | validate_buildinfo_json || fail "existing panel identity is not trusted"
		if [ -e "$STATE_DIR/installed-release.json" ] || [ -L "$STATE_DIR/installed-release.json" ]; then
			binary_identity="$(printf '%s\n' "$metadata" | jq -c '{product,version,sourceCommit,channel}')"
			marker_identity="$(jq -c '{product,version,sourceCommit,channel}' "$STATE_DIR/installed-release.json")"
			[ "$binary_identity" = "$marker_identity" ] || fail "installed release marker does not match the panel identity"
		fi
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

TMP_PARENT="${ROOT_PREFIX}/tmp/xkeen-control"
if [ ! -e "$TMP_PARENT" ] && [ ! -L "$TMP_PARENT" ]; then
	(umask 077; mkdir "$TMP_PARENT") || fail "bootstrap temporary parent unavailable"
fi
[ -d "$TMP_PARENT" ] && [ ! -L "$TMP_PARENT" ] && [ "$(stat -c %u "$TMP_PARENT")" = 0 ] || fail "unsafe bootstrap temporary parent"
mode=$(stat -c %a "$TMP_PARENT")
case "$mode" in ''|*[!0-7]*) fail "unsafe bootstrap temporary mode";; esac
[ "$((0$mode & 0022))" -eq 0 ] || fail "writable bootstrap temporary parent"
if [ -e "$TMP_ROOT" ] || [ -L "$TMP_ROOT" ]; then
	[ -d "$TMP_ROOT" ] && [ ! -L "$TMP_ROOT" ] && [ "$(stat -c %u "$TMP_ROOT")" = 0 ] || fail "unsafe bootstrap temporary directory"
	mode=$(stat -c %a "$TMP_ROOT")
	case "$mode" in ''|*[!0-7]*) fail "unsafe bootstrap temporary mode";; esac
	[ "$((0$mode & 0022))" -eq 0 ] || fail "writable bootstrap temporary directory"
fi
rm -rf "$TMP_ROOT"
mkdir -p "$TMP_ROOT/assets"
chmod 700 "$TMP_ROOT" "$TMP_ROOT/assets"
if [ "$INSTALL_MODE" != setup ]; then
	mkdir -p "$AUTH_DIR" "$STATE_DIR"
	chmod 700 "$ROOT_DIR" "$AUTH_DIR" "$STATE_DIR"
fi

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

fetch "$MANIFEST_ASSET" "$TMP_ROOT/release-manifest.json"
fetch "$SIGNATURE_ASSET" "$TMP_ROOT/release-manifest.sig"
fetch SHA256SUMS "$TMP_ROOT/SHA256SUMS"
for name in "$BINARY_ASSET" S99xkeen-control xkeen-control-updater install.sh; do
	fetch "$name" "$TMP_ROOT/assets/$name"
done

jq -e --arg channel "$CHANNEL" --arg version "$EXPECTED_VERSION" --arg architecture "$ARCHITECTURE" --arg binary "$BINARY_ASSET" '.schemaVersion == 1 and .product == "xkeen-control" and .version == $version and .channel == $channel and (.sourceCommit | type == "string" and length == 40) and .os == "linux" and .architecture == $architecture and ((.artifacts | map(.name) | sort) == (["S99xkeen-control", "install.sh", $binary, "xkeen-control-updater"] | sort))' "$TMP_ROOT/release-manifest.json" >/dev/null || fail "release manifest identity does not match bootstrap policy"

# First-install trust starts at GitHub HTTPS. Internal manifest, size and hash
# consistency is checked before any asset is installed; the downloaded panel
# is not executed to verify its own release signature.
while IFS="$(printf '\t')" read -r name size hash; do
	case "$name" in
		"$BINARY_ASSET"|S99xkeen-control|xkeen-control-updater|install.sh) ;;
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
		"$MANIFEST_ASSET") file="$TMP_ROOT/release-manifest.json" ;;
		"$SIGNATURE_ASSET") file="$TMP_ROOT/release-manifest.sig" ;;
		"$BINARY_ASSET"|S99xkeen-control|xkeen-control-updater|install.sh) file="$TMP_ROOT/assets/$name" ;;
		# The global list also covers the other fixed platform. Do not download
		# or execute it; its signed manifest is verified by release publication.
		xkeen-control-linux-arm64|xkeen-control-linux-mipsle|release-manifest.json|release-manifest.sig|release-manifest-mipsle.json|release-manifest-mipsle.sig) file="" ;;
		*) fail "checksum list contains an unexpected file" ;;
	esac
	case " $sum_names " in
		*" $name "*) fail "checksum list contains a duplicate file" ;;
	esac
	printf '%s' "$expected" | grep -Eq '^[0-9a-f]{64}$' || fail "invalid checksum"
	if [ -n "$file" ]; then
		actual_hash="$(sha256sum "$file" | awk '{print $1}')"
		[ "$actual_hash" = "$expected" ] || fail "SHA256SUMS verification failed"
	fi
	sum_count=$((sum_count + 1))
	sum_names="$sum_names $name"
done < "$TMP_ROOT/SHA256SUMS"
# Legacy ARM64 releases have six entries; dual-platform releases have nine.
expected_sums='S99xkeen-control install.sh release-manifest.json release-manifest.sig xkeen-control-linux-arm64 xkeen-control-updater'
if [ "$sum_count" -eq 9 ]; then
	expected_sums="$expected_sums release-manifest-mipsle.json release-manifest-mipsle.sig xkeen-control-linux-mipsle"
else
	[ "$sum_count" -eq 6 ] && [ "$ARCHITECTURE" = arm64 ] || fail "checksum list is incomplete"
fi
for name in $expected_sums; do
	case " $sum_names " in *" $name "*) ;; *) fail "checksum list is incomplete" ;; esac
done

if [ "$INSTALL_MODE" = setup ]; then
	[ "$LEGACY_ADOPTION" = 0 ] || fail "setup cannot adopt an existing panel"
	chmod 755 "$TMP_ROOT/assets/xkeen-control-linux-arm64"
	exec "$TMP_ROOT/assets/xkeen-control-linux-arm64" setup run
fi
if [ "$INSTALL_MODE" = setup-panel ]; then
	chmod 755 "$TMP_ROOT/assets/xkeen-control-linux-arm64"
	"$TMP_ROOT/assets/xkeen-control-linux-arm64" setup panel-guard || fail "panel installation requires the live initial setup owner"
fi

if [ "$LEGACY_ADOPTION" = "1" ]; then
	# The fixed updater owns the legacy quiescence/recovery handoff,
	# stop/swap/health/rollback. The installer only moves the already-
	# consistency-checked release into its fixed candidate boundary and invokes
	# the explicit adoption action; it never installs a helper first.
	for name in xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater; do
		cp "$TMP_ROOT/assets/$name" "$TMP_ROOT/$name"
		chmod 755 "$TMP_ROOT/$name"
	done
	: > "$TMP_ROOT/.legacy-adoption"
	chmod 600 "$TMP_ROOT/.legacy-adoption"
	jq -c '{product,version,sourceCommit,channel}' "$TMP_ROOT/release-manifest.json" > "$TMP_ROOT/.installed-release.json.new" || fail "release marker could not be prepared"
	chmod 600 "$TMP_ROOT/.installed-release.json.new"
	mv -f "$TMP_ROOT/.installed-release.json.new" "$TMP_ROOT/installed-release.json"
	exec "$TMP_ROOT/xkeen-control-updater" adopt
fi

mkdir -p "$(dirname "$BIN")" "$(dirname "$INIT")" "$(dirname "$UPDATER")"
cp "$TMP_ROOT/assets/$BINARY_ASSET" "$BIN.new"
cp "$TMP_ROOT/assets/S99xkeen-control" "$INIT.new"
cp "$TMP_ROOT/assets/xkeen-control-updater" "$UPDATER.new"
chmod 755 "$BIN.new" "$INIT.new" "$UPDATER.new"
mv -f "$BIN.new" "$BIN"
mv -f "$INIT.new" "$INIT"
mv -f "$UPDATER.new" "$UPDATER"
rm -rf "$TMP_ROOT"

if [ "$INSTALL_MODE" = setup-panel ]; then
	echo "Panel installed with daemon startup deferred to verified setup completion."
	exit 0
fi

if [ ! -e "${AUTH_DIR}/password.bcrypt" ]; then
	XKEEN_CONTROL_AUTH_HASH="${AUTH_DIR}/password.bcrypt" \
	XKEEN_CONTROL_BOOTSTRAP_MARKER="${AUTH_DIR}/bootstrap-required" \
	"$BIN" password bootstrap
fi

"$INIT" start >/dev/null
health="$(curl --fail --silent --show-error --max-time 10 http://127.0.0.1:8787/healthz)" || fail "panel health check failed"
[ "$health" = "ok" ] || fail "panel health response was not generic ok"
metadata="$($BIN version --json)" || fail "installed build metadata is unavailable"
printf '%s\n' "$metadata" | validate_buildinfo_json || fail "installed build provenance is invalid"
echo "xkeen-control installed: $metadata"
echo "Management listener remains loopback by default; use an SSH tunnel unless an exact private address is configured explicitly."
echo "Install stock XKeen using its official procedure. The panel invokes its supported commands and edits native configuration; it does not install or patch XKeen."
