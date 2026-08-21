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
