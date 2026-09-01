#!/bin/sh
set -eu
set -f

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
command -v jq >/dev/null 2>&1 || { echo "jq is required for updater fixture" >&2; exit 1; }

tmp="$(mktemp -d /tmp/xkeen-updater-test.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
fakebin="$tmp/fakebin"
mkdir -p "$fakebin"

cat > "$fakebin/curl" <<'EOF_CURL'
#!/bin/sh
set -eu
url=""
for arg in "$@"; do
	case "$arg" in http://*) url="$arg" ;; esac
done
[ "$url" = "${EXPECTED_HEALTH_URL:?}" ] || { echo "unexpected health URL: $url" >&2; exit 1; }
exit 0
EOF_CURL
chmod 755 "$fakebin/curl"

make_binary() {
	path="$1"
	version="$2"
	commit="$3"
	channel="$4"
	cat > "$path" <<EOF_BIN
#!/bin/sh
if [ "\${1:-} \${2:-}" = "version --json" ]; then
	printf '%s\\n' '{"product":"xkeen-control","version":"$version","sourceCommit":"$commit","channel":"$channel"}'
	exit 0
fi
exit 2
EOF_BIN
	chmod 755 "$path"
}

make_init() {
	path="$1"
	cat > "$path" <<'EOF_INIT'
#!/bin/sh
case "${1:-}" in start|stop|status|restart) exit 0 ;; *) exit 2 ;; esac
EOF_INIT
	chmod 755 "$path"
}

setup_generation() {
	root="$1"
	listen="${2-192.168.10.2:8787}"
	mkdir -p "$root/opt/sbin" "$root/opt/etc/init.d" "$root/opt/libexec" \
		"$root/opt/etc/xkeen-control/state" "$root/opt/etc/xkeen-control/previous" \
		"$root/tmp/xkeen-control/panel-update"
	make_binary "$root/opt/sbin/xkeen-control" "1.0.0" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" stable
	make_init "$root/opt/etc/init.d/S99xkeen-control"
	cp "$ROOT/scripts/xkeen-control-updater" "$root/opt/libexec/xkeen-control-updater"
	chmod 755 "$root/opt/libexec/xkeen-control-updater"
	if [ -n "$listen" ]; then printf '%s\n' "$listen" > "$root/opt/etc/xkeen-control/listen-address"; fi
	printf '%s\n' '{"product":"xkeen-control","version":"1.0.0","sourceCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","channel":"stable"}' > "$root/opt/etc/xkeen-control/state/installed-release.json"

	candidate="$root/tmp/xkeen-control/panel-update"
	make_binary "$candidate/xkeen-control-linux-arm64" "1.2.3" "cccccccccccccccccccccccccccccccccccccccc" stable
	make_init "$candidate/S99xkeen-control"
	cp "$ROOT/scripts/xkeen-control-updater" "$candidate/xkeen-control-updater"
	chmod 755 "$candidate/xkeen-control-updater"
	printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$candidate/installed-release.json"
	printf '%s\n' '{"version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$candidate/release-manifest.json"
}

setup_legacy_generation() {
	root="$1"
	listen="${2-192.168.10.2:8787}"
	mkdir -p "$root/opt/sbin" "$root/opt/etc/init.d" "$root/opt/libexec" \
		"$root/opt/etc/xkeen-control/state" "$root/opt/etc/xkeen-control/previous" \
		"$root/tmp/xkeen-control/panel-update"
	make_binary "$root/opt/sbin/xkeen-control" "1.0.0" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" stable
	make_init "$root/opt/etc/init.d/S99xkeen-control"
	printf '%s\n' '# legacy generation' >> "$root/opt/etc/init.d/S99xkeen-control"
	if [ -n "$listen" ]; then printf '%s\n' "$listen" > "$root/opt/etc/xkeen-control/listen-address"; fi

	candidate="$root/tmp/xkeen-control/panel-update"
	make_binary "$candidate/xkeen-control-linux-arm64" "1.2.3" "cccccccccccccccccccccccccccccccccccccccc" stable
	make_init "$candidate/S99xkeen-control"
	cp "$ROOT/scripts/xkeen-control-updater" "$candidate/xkeen-control-updater"
	chmod 755 "$candidate/xkeen-control-updater"
	printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$candidate/installed-release.json"
}

root="$tmp/root"
setup_generation "$root"
PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$root" \
XKEEN_CONTROL_HANDOFF_DELAY=0 \
sh "$ROOT/scripts/xkeen-control-updater" install

grep -Fq '"version":"1.2.3"' "$root/opt/etc/xkeen-control/state/installed-release.json"
[ -f "$root/opt/etc/xkeen-control/previous/panel/xkeen-control-linux-arm64" ]
"$root/opt/sbin/xkeen-control" version --json | jq -e '.version == "1.2.3"' >/dev/null

PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$root" \
XKEEN_CONTROL_HANDOFF_DELAY=0 \
sh "$root/opt/libexec/xkeen-control-updater" rollback

grep -Fq '"version":"1.0.0"' "$root/opt/etc/xkeen-control/state/installed-release.json"
[ ! -e "$root/opt/etc/xkeen-control/previous/panel" ]
"$root/opt/sbin/xkeen-control" version --json | jq -e '.version == "1.0.0"' >/dev/null

legacy_root="$tmp/legacy-root"
setup_legacy_generation "$legacy_root"
legacy_binary_before="$(sha256sum "$legacy_root/opt/sbin/xkeen-control" | awk '{print $1}')"
legacy_init_before="$(sha256sum "$legacy_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')"
PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$legacy_root" \
XKEEN_CONTROL_HANDOFF_DELAY=0 \
sh "$ROOT/scripts/xkeen-control-updater" install

[ "$(sha256sum "$legacy_root/opt/sbin/xkeen-control" | awk '{print $1}')" != "$legacy_binary_before" ]
[ "$(sha256sum "$legacy_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" != "$legacy_init_before" ]
[ -x "$legacy_root/opt/libexec/xkeen-control-updater" ]
[ -f "$legacy_root/opt/etc/xkeen-control/previous/panel/.helper-absent" ]
[ ! -e "$legacy_root/opt/etc/xkeen-control/previous/panel/xkeen-control-updater" ]

PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$legacy_root" \
XKEEN_CONTROL_HANDOFF_DELAY=0 \
sh "$legacy_root/opt/libexec/xkeen-control-updater" rollback

[ "$(sha256sum "$legacy_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$legacy_binary_before" ]
[ "$(sha256sum "$legacy_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" = "$legacy_init_before" ]
[ ! -e "$legacy_root/opt/libexec/xkeen-control-updater" ]
[ ! -e "$legacy_root/opt/etc/xkeen-control/state/installed-release.json" ]
[ ! -e "$legacy_root/opt/etc/xkeen-control/previous/panel" ]

mkdir -p "$legacy_root/tmp/xkeen-control/panel-update"
make_binary "$legacy_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" "1.2.3" "cccccccccccccccccccccccccccccccccccccccc" stable
make_init "$legacy_root/tmp/xkeen-control/panel-update/S99xkeen-control"
cp "$ROOT/scripts/xkeen-control-updater" "$legacy_root/tmp/xkeen-control/panel-update/xkeen-control-updater"
chmod 755 "$legacy_root/tmp/xkeen-control/panel-update/xkeen-control-updater"
printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$legacy_root/tmp/xkeen-control/panel-update/installed-release.json"
PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$legacy_root" \
XKEEN_CONTROL_HANDOFF_DELAY=0 \
sh "$ROOT/scripts/xkeen-control-updater" install
[ -x "$legacy_root/opt/libexec/xkeen-control-updater" ]
jq -e '.version == "1.2.3"' "$legacy_root/opt/etc/xkeen-control/state/installed-release.json" >/dev/null

legacy_failure_root="$tmp/legacy-failure-root"
setup_legacy_generation "$legacy_failure_root"
legacy_failure_binary_before="$(sha256sum "$legacy_failure_root/opt/sbin/xkeen-control" | awk '{print $1}')"
legacy_failure_init_before="$(sha256sum "$legacy_failure_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')"
make_binary "$legacy_failure_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" "9.9.9" "dddddddddddddddddddddddddddddddddddddddd" stable
if PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$legacy_failure_root" \
	XKEEN_CONTROL_HANDOFF_DELAY=0 \
	sh "$ROOT/scripts/xkeen-control-updater" install >/dev/null 2>&1; then
	echo "legacy failure candidate unexpectedly committed" >&2
	exit 1
fi
[ "$(sha256sum "$legacy_failure_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$legacy_failure_binary_before" ]
[ "$(sha256sum "$legacy_failure_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" = "$legacy_failure_init_before" ]
[ ! -e "$legacy_failure_root/opt/libexec/xkeen-control-updater" ]
[ ! -e "$legacy_failure_root/opt/etc/xkeen-control/state/installed-release.json" ]
[ -f "$legacy_failure_root/opt/etc/xkeen-control/previous/panel/.helper-absent" ]
[ ! -e "$legacy_failure_root/tmp/xkeen-control/panel-update" ]

loopback_root="$tmp/loopback-root"
setup_generation "$loopback_root" ""
PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://127.0.0.1:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$loopback_root" \
XKEEN_CONTROL_HANDOFF_DELAY=0 \
sh "$ROOT/scripts/xkeen-control-updater" install
grep -Fq '"version":"1.2.3"' "$loopback_root/opt/etc/xkeen-control/state/installed-release.json"

failure_root="$tmp/failure-root"
setup_generation "$failure_root"
make_binary "$failure_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" "9.9.9" "dddddddddddddddddddddddddddddddddddddddd" stable
if PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$failure_root" \
	XKEEN_CONTROL_HANDOFF_DELAY=0 \
	sh "$ROOT/scripts/xkeen-control-updater" install >/dev/null 2>&1; then
	echo "mismatched candidate unexpectedly committed" >&2
	exit 1
fi
grep -Fq '"version":"1.0.0"' "$failure_root/opt/etc/xkeen-control/state/installed-release.json"
"$failure_root/opt/sbin/xkeen-control" version --json | jq -e '.version == "1.0.0"' >/dev/null
