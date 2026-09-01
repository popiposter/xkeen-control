#!/bin/sh
set -eu
set -f

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
command -v jq >/dev/null 2>&1 || { echo "jq is required for bootstrap fixture" >&2; exit 1; }

tmp="$(mktemp -d /tmp/xkeen-bootstrap-test.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
fixture="$tmp/release"
fakebin="$tmp/fakebin"
testroot="$tmp/root"
mkdir -p "$fixture" "$fakebin" "$testroot/opt" "$testroot/tmp"

cat > "$tmp/legacy-panel" <<'EOF_LEGACY_BIN'
#!/bin/sh
set -eu
echo "legacy panel must not be executed by the adoption gate" >&2
exit 1
EOF_LEGACY_BIN
chmod 755 "$tmp/legacy-panel"

cat > "$tmp/legacy-init" <<'EOF_LEGACY_INIT'
#!/bin/sh
case "${1:-}" in start|stop|status|restart) exit 0 ;; *) exit 2 ;; esac
EOF_LEGACY_INIT
printf '%s\n' '# legacy generation' >> "$tmp/legacy-init"
chmod 755 "$tmp/legacy-init"
legacy_binary_hash="$(sha256sum "$tmp/legacy-panel" | awk '{print $1}')"
legacy_init_hash="$(sha256sum "$tmp/legacy-init" | awk '{print $1}')"

installer="$fixture/install.sh"
sed \
	-e 's/^STABLE_RELEASE_VERSION=""$/STABLE_RELEASE_VERSION="1.2.3"/' \
	-e "s/^LEGACY_PANEL_BINARY_SHA256=.*/LEGACY_PANEL_BINARY_SHA256=\"$legacy_binary_hash\"/" \
	-e "s/^LEGACY_PANEL_INIT_SHA256=.*/LEGACY_PANEL_INIT_SHA256=\"$legacy_init_hash\"/" \
	"$ROOT/scripts/install.sh" > "$installer"
chmod 755 "$installer"

cat > "$fixture/xkeen-control-linux-arm64" <<'EOF_BIN'
#!/bin/sh
set -eu
root="${XKEEN_CONTROL_TEST_ROOT:?}"
case "${1:-} ${2:-}" in
	"version --json")
		printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}'
		;;
	"password bootstrap")
		mkdir -p "$root/opt/etc/xkeen-control/auth"
		printf '%s\n' synthetic-hash > "$root/opt/etc/xkeen-control/auth/password.bcrypt"
		printf '%s\n' bootstrap-required > "$root/opt/etc/xkeen-control/auth/bootstrap-required"
		printf '%s\n' bootstrap >> "$root/bootstrap-calls"
		;;
	"nodes validate")
		jq -e '.schemaVersion == 1 and (.generation | type == "string" and length > 0)' "${XKEEN_NODES_PATH:?}" >/dev/null
		;;
	"nodes render")
		[ "${3:-}" = "--output" ] && [ -n "${4:-}" ] || exit 1
		generation="$(jq -r '.generation' "${XKEEN_NODES_PATH:?}")"
		printf '{"schemaVersion":1,"generation":"%s"}\n' "$generation" > "$4"
		;;
	"self-update --channel")
		printf '%s\n' "$*" >> "$root/self-update-calls"
		;;
	*)
		echo "unexpected synthetic binary invocation: $*" >&2
		exit 1
		;;
esac
EOF_BIN
chmod 755 "$fixture/xkeen-control-linux-arm64"

cat > "$fixture/S99xkeen-control" <<'EOF_INIT'
#!/bin/sh
case "${1:-}" in start|stop|status|restart) exit 0 ;; *) exit 2 ;; esac
EOF_INIT
chmod 755 "$fixture/S99xkeen-control"

cp "$ROOT/scripts/xkeen-control-updater" "$fixture/xkeen-control-updater"
chmod 755 "$fixture/xkeen-control-updater"

manifest="$fixture/release-manifest.json"
write_release_metadata() {
	artifact_json=""
	for name in S99xkeen-control install.sh xkeen-control-linux-arm64 xkeen-control-updater; do
		size="$(wc -c < "$fixture/$name" | tr -d '[:space:]')"
		hash="$(sha256sum "$fixture/$name" | awk '{print $1}')"
		entry="$(jq -cn --arg name "$name" --argjson size "$size" --arg hash "$hash" '{name:$name,size:$size,sha256:$hash}')"
		if [ -z "$artifact_json" ]; then artifact_json="$entry"; else artifact_json="$artifact_json,$entry"; fi
	done
	printf '{"schemaVersion":1,"product":"xkeen-control","version":"1.2.3","channel":"stable","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","sourceDateEpoch":1750000000,"os":"linux","architecture":"arm64","artifacts":[%s],"compatibility":{"stateSchemaMin":0,"stateSchemaMax":0,"updaterGeneration":1,"manualMigrationRequired":false,"rollbackCompatible":true}}\n' "$artifact_json" > "$manifest"
	printf '%s\n' synthetic-signature > "$fixture/release-manifest.sig"
	(
		cd "$fixture"
		sha256sum xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater install.sh release-manifest.json release-manifest.sig > SHA256SUMS
	)
}
write_release_metadata

cat > "$fakebin/opkg" <<'EOF_OPKG'
#!/bin/sh
exit 0
EOF_OPKG
cat > "$fakebin/uname" <<'EOF_UNAME'
#!/bin/sh
printf '%s\n' aarch64
EOF_UNAME
cat > "$fakebin/df" <<'EOF_DF'
#!/bin/sh
printf '%s\n' 'Filesystem 1024-blocks Used Available Capacity Mounted on'
printf '%s\n' 'synthetic 1048576 1 1048575 1% /opt'
EOF_DF
cat > "$fakebin/curl" <<'EOF_CURL'
#!/bin/sh
set -eu
destination=""
url=""
writeout=0
while [ "$#" -gt 0 ]; do
	case "$1" in
		-o) destination="$2"; shift 2 ;;
		--write-out) writeout=1; shift 2 ;;
		https://*|http://*) url="$1"; shift ;;
		*) shift ;;
	esac
done
printf '%s\n' "$url" >> "${XKEEN_CONTROL_TEST_ROOT:?}/curl-calls"
case "$url" in
	http://127.0.0.1:8787/healthz|http://192.168.10.2:8787/healthz)
		printf '%s' ok
		exit 0
		;;
	https://github.com/popiposter/xkeen-control/releases/download/v1.2.3/*)
		name="${url##*/}"
		cp "${XKEEN_CONTROL_FIXTURE_DIR:?}/$name" "$destination"
		[ "$writeout" -eq 0 ] || printf '%s' "$url"
		;;
	*)
		echo "unexpected synthetic curl URL: $url" >&2
		exit 1
		;;
esac
EOF_CURL
chmod 755 "$fakebin/opkg" "$fakebin/uname" "$fakebin/df" "$fakebin/curl"

run_installer() {
	root="$1"
	PATH="$fakebin:$PATH" \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$root" \
	XKEEN_CONTROL_FIXTURE_DIR="$fixture" \
	sh "$installer"
}

setup_legacy_root() {
	root="$1"
	mkdir -p "$root/opt/sbin" "$root/opt/etc/init.d" "$root/opt/libexec" \
		"$root/opt/etc/xkeen-control/auth" "$root/opt/etc/xkeen-control/state" \
		"$root/opt/etc/xkeen-control/secrets" "$root/opt/etc/xray/configs" "$root/tmp"
	cp "$tmp/legacy-panel" "$root/opt/sbin/xkeen-control"
	cp "$tmp/legacy-init" "$root/opt/etc/init.d/S99xkeen-control"
	chmod 755 "$root/opt/sbin/xkeen-control" "$root/opt/etc/init.d/S99xkeen-control"
	printf '%s\n' existing-auth-hash > "$root/opt/etc/xkeen-control/auth/password.bcrypt"
	printf '%s\n' 192.168.10.2:8787 > "$root/opt/etc/xkeen-control/listen-address"
	printf '%s\n' existing-state-fixture > "$root/opt/etc/xkeen-control/state/protected-state"
	printf '%s\n' existing-xray-fixture > "$root/opt/etc/xray/configs/protected-config"
	printf '%s\n' '{"schemaVersion":1,"generation":"legacy"}' > "$root/opt/etc/xkeen-control/secrets/nodes.json"
	printf '%s\n' '{"schemaVersion":1,"generation":"legacy"}' > "$root/opt/etc/xray/configs/04_outbounds.json"
}

setup_managed_root() {
	root="$1"
	mkdir -p "$root/opt/sbin" "$root/opt/etc/init.d" "$root/opt/libexec" \
		"$root/opt/etc/xkeen-control/auth" "$root/opt/etc/xkeen-control/state" \
		"$root/tmp"
	cp "$fixture/xkeen-control-linux-arm64" "$root/opt/sbin/xkeen-control"
	cp "$fixture/S99xkeen-control" "$root/opt/etc/init.d/S99xkeen-control"
	cp "$fixture/xkeen-control-updater" "$root/opt/libexec/xkeen-control-updater"
	chmod 755 "$root/opt/sbin/xkeen-control" "$root/opt/etc/init.d/S99xkeen-control" "$root/opt/libexec/xkeen-control-updater"
	printf '%s\n' existing-auth-hash > "$root/opt/etc/xkeen-control/auth/password.bcrypt"
}

run_installer "$testroot" >/dev/null

[ -x "$testroot/opt/sbin/xkeen-control" ]
[ -x "$testroot/opt/etc/init.d/S99xkeen-control" ]
[ -x "$testroot/opt/libexec/xkeen-control-updater" ]
[ -f "$testroot/opt/etc/xkeen-control/auth/password.bcrypt" ]
[ "$(wc -l < "$testroot/bootstrap-calls" | tr -d '[:space:]')" -eq 1 ]
before_hash="$(sha256sum "$testroot/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')"

run_installer "$testroot" >/dev/null

after_hash="$(sha256sum "$testroot/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')"
[ "$before_hash" = "$after_hash" ]
[ "$(wc -l < "$testroot/bootstrap-calls" | tr -d '[:space:]')" -eq 1 ]
grep -Fq 'self-update --channel stable --apply' "$testroot/self-update-calls"

malformed_marker_root="$tmp/malformed-marker-root"
setup_managed_root "$malformed_marker_root"
printf '%s\n' '{"product":"xkeen-control","version":"","sourceCommit":"","channel":"stable"}' > "$malformed_marker_root/opt/etc/xkeen-control/state/installed-release.json"
malformed_marker_before="$(sha256sum "$malformed_marker_root/opt/etc/xkeen-control/state/installed-release.json" | awk '{print $1}')"
malformed_binary_before="$(sha256sum "$malformed_marker_root/opt/sbin/xkeen-control" | awk '{print $1}')"
if run_installer "$malformed_marker_root" >/dev/null 2>&1; then
	echo "malformed managed marker unexpectedly delegated" >&2
	exit 1
fi
[ "$(sha256sum "$malformed_marker_root/opt/etc/xkeen-control/state/installed-release.json" | awk '{print $1}')" = "$malformed_marker_before" ]
[ "$(sha256sum "$malformed_marker_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$malformed_binary_before" ]
[ ! -e "$malformed_marker_root/curl-calls" ]
[ ! -e "$malformed_marker_root/self-update-calls" ]

mismatched_marker_root="$tmp/mismatched-marker-root"
setup_managed_root "$mismatched_marker_root"
printf '%s\n' '{"product":"xkeen-control","version":"1.2.4","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$mismatched_marker_root/opt/etc/xkeen-control/state/installed-release.json"
mismatched_marker_before="$(sha256sum "$mismatched_marker_root/opt/etc/xkeen-control/state/installed-release.json" | awk '{print $1}')"
mismatched_binary_before="$(sha256sum "$mismatched_marker_root/opt/sbin/xkeen-control" | awk '{print $1}')"
if run_installer "$mismatched_marker_root" >/dev/null 2>&1; then
	echo "mismatched managed marker unexpectedly delegated" >&2
	exit 1
fi
[ "$(sha256sum "$mismatched_marker_root/opt/etc/xkeen-control/state/installed-release.json" | awk '{print $1}')" = "$mismatched_marker_before" ]
[ "$(sha256sum "$mismatched_marker_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$mismatched_binary_before" ]
[ ! -e "$mismatched_marker_root/curl-calls" ]
[ ! -e "$mismatched_marker_root/self-update-calls" ]

invalid_binary_root="$tmp/invalid-binary-root"
setup_managed_root "$invalid_binary_root"
sed 's/cccccccccccccccccccccccccccccccccccccccc/CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC/' "$fixture/xkeen-control-linux-arm64" > "$invalid_binary_root/opt/sbin/xkeen-control"
chmod 755 "$invalid_binary_root/opt/sbin/xkeen-control"
if run_installer "$invalid_binary_root" >/dev/null 2>&1; then
	echo "invalid managed binary unexpectedly delegated" >&2
	exit 1
fi
[ ! -e "$invalid_binary_root/curl-calls" ]
[ ! -e "$invalid_binary_root/self-update-calls" ]

candidate_binary_hash="$(sha256sum "$fixture/xkeen-control-linux-arm64" | awk '{print $1}')"
candidate_init_hash="$(sha256sum "$fixture/S99xkeen-control" | awk '{print $1}')"
good_release="$tmp/good-release"
mkdir -p "$good_release"
for name in SHA256SUMS release-manifest.json release-manifest.sig install.sh xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater; do
	cp "$fixture/$name" "$good_release/$name"
done

adoption_root="$tmp/adoption-root"
setup_legacy_root "$adoption_root"
adoption_auth_before="$(sha256sum "$adoption_root/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')"
adoption_listen_before="$(sha256sum "$adoption_root/opt/etc/xkeen-control/listen-address" | awk '{print $1}')"
adoption_state_before="$(sha256sum "$adoption_root/opt/etc/xkeen-control/state/protected-state" | awk '{print $1}')"
adoption_xray_before="$(sha256sum "$adoption_root/opt/etc/xray/configs/protected-config" | awk '{print $1}')"
adoption_legacy_binary_before="$(sha256sum "$adoption_root/opt/sbin/xkeen-control" | awk '{print $1}')"
adoption_legacy_init_before="$(sha256sum "$adoption_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')"

run_installer "$adoption_root" >/dev/null
[ "$(sha256sum "$adoption_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$candidate_binary_hash" ]
[ "$(sha256sum "$adoption_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" = "$candidate_init_hash" ]
[ -x "$adoption_root/opt/libexec/xkeen-control-updater" ]
jq -e '.version == "1.2.3" and .channel == "stable"' "$adoption_root/opt/etc/xkeen-control/state/installed-release.json" >/dev/null
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')" = "$adoption_auth_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/listen-address" | awk '{print $1}')" = "$adoption_listen_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/state/protected-state" | awk '{print $1}')" = "$adoption_state_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xray/configs/protected-config" | awk '{print $1}')" = "$adoption_xray_before" ]
[ ! -e "$adoption_root/bootstrap-calls" ]
[ ! -e "$adoption_root/self-update-calls" ]
[ -f "$adoption_root/opt/etc/xkeen-control/previous/panel/.helper-absent" ]
[ ! -e "$adoption_root/opt/etc/xkeen-control/previous/panel/xkeen-control-updater" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/previous/panel/xkeen-control-linux-arm64" | awk '{print $1}')" = "$adoption_legacy_binary_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/previous/panel/S99xkeen-control" | awk '{print $1}')" = "$adoption_legacy_init_before" ]
[ ! -e "$adoption_root/tmp/xkeen-control/panel-update" ]

failure_root="$tmp/failure-root"
setup_legacy_root "$failure_root"
failure_auth_before="$(sha256sum "$failure_root/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')"
failure_listen_before="$(sha256sum "$failure_root/opt/etc/xkeen-control/listen-address" | awk '{print $1}')"
failure_state_before="$(sha256sum "$failure_root/opt/etc/xkeen-control/state/protected-state" | awk '{print $1}')"
failure_xray_before="$(sha256sum "$failure_root/opt/etc/xray/configs/protected-config" | awk '{print $1}')"
failure_legacy_binary_before="$(sha256sum "$failure_root/opt/sbin/xkeen-control" | awk '{print $1}')"
failure_legacy_init_before="$(sha256sum "$failure_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')"
sed 's/"version":"1.2.3"/"version":"9.9.9"/' "$good_release/xkeen-control-linux-arm64" > "$fixture/xkeen-control-linux-arm64"
chmod 755 "$fixture/xkeen-control-linux-arm64"
write_release_metadata
if run_installer "$failure_root" >/dev/null 2>&1; then
	echo "invalid adoption candidate unexpectedly committed" >&2
	exit 1
fi
[ "$(sha256sum "$failure_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$failure_legacy_binary_before" ]
[ "$(sha256sum "$failure_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" = "$failure_legacy_init_before" ]
[ ! -e "$failure_root/opt/libexec/xkeen-control-updater" ]
[ ! -e "$failure_root/opt/etc/xkeen-control/state/installed-release.json" ]
[ -f "$failure_root/opt/etc/xkeen-control/previous/panel/.helper-absent" ]
[ ! -e "$failure_root/tmp/xkeen-control/panel-update" ]
[ "$(sha256sum "$failure_root/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')" = "$failure_auth_before" ]
[ "$(sha256sum "$failure_root/opt/etc/xkeen-control/listen-address" | awk '{print $1}')" = "$failure_listen_before" ]
[ "$(sha256sum "$failure_root/opt/etc/xkeen-control/state/protected-state" | awk '{print $1}')" = "$failure_state_before" ]
[ "$(sha256sum "$failure_root/opt/etc/xray/configs/protected-config" | awk '{print $1}')" = "$failure_xray_before" ]

for name in SHA256SUMS release-manifest.json release-manifest.sig install.sh xkeen-control-linux-arm64 S99xkeen-control xkeen-control-updater; do
	cp "$good_release/$name" "$fixture/$name"
done

PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$adoption_root" \
sh "$adoption_root/opt/libexec/xkeen-control-updater" rollback >/dev/null

[ "$(sha256sum "$adoption_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$adoption_legacy_binary_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" = "$adoption_legacy_init_before" ]
[ ! -e "$adoption_root/opt/libexec/xkeen-control-updater" ]
[ ! -e "$adoption_root/opt/etc/xkeen-control/state/installed-release.json" ]
[ ! -e "$adoption_root/opt/etc/xkeen-control/previous/panel" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')" = "$adoption_auth_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/listen-address" | awk '{print $1}')" = "$adoption_listen_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/state/protected-state" | awk '{print $1}')" = "$adoption_state_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xray/configs/protected-config" | awk '{print $1}')" = "$adoption_xray_before" ]
grep -Fq 'http://192.168.10.2:8787/healthz' "$adoption_root/curl-calls"

run_installer "$adoption_root" >/dev/null
[ -x "$adoption_root/opt/libexec/xkeen-control-updater" ]
jq -e '.version == "1.2.3" and .channel == "stable"' "$adoption_root/opt/etc/xkeen-control/state/installed-release.json" >/dev/null
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')" = "$adoption_auth_before" ]
[ "$(sha256sum "$adoption_root/opt/etc/xkeen-control/listen-address" | awk '{print $1}')" = "$adoption_listen_before" ]

partial_root="$tmp/partial-root"
setup_legacy_root "$partial_root"
printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$partial_root/opt/etc/xkeen-control/state/installed-release.json"
partial_binary_before="$(sha256sum "$partial_root/opt/sbin/xkeen-control" | awk '{print $1}')"
partial_marker_before="$(sha256sum "$partial_root/opt/etc/xkeen-control/state/installed-release.json" | awk '{print $1}')"
if run_installer "$partial_root" >/dev/null 2>&1; then
	echo "partial managed install unexpectedly entered legacy adoption" >&2
	exit 1
fi
[ "$(sha256sum "$partial_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$partial_binary_before" ]
[ "$(sha256sum "$partial_root/opt/etc/xkeen-control/state/installed-release.json" | awk '{print $1}')" = "$partial_marker_before" ]
[ ! -e "$partial_root/opt/libexec/xkeen-control-updater" ]
[ ! -e "$partial_root/curl-calls" ]
[ ! -e "$partial_root/tmp/xkeen-control/panel-update" ]
