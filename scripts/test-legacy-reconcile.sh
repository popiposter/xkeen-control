#!/bin/sh
set -eu
set -f

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
command -v jq >/dev/null 2>&1 || { echo "jq is required for legacy reconcile fixture" >&2; exit 1; }

tmp="$(mktemp -d /tmp/xkeen-legacy-reconcile-test.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
fakebin="$tmp/fakebin"
mkdir -p "$fakebin"

cat > "$fakebin/curl" <<'EOF_CURL'
#!/bin/sh
exit 0
EOF_CURL
chmod 755 "$fakebin/curl"

make_candidate_binary() {
	path="$1"
	cat > "$path" <<'EOF_BIN'
#!/bin/sh
set -eu
if [ "${1:-} ${2:-}" = "version --json" ]; then
	printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}'
	exit 0
fi
if [ "${1:-} ${2:-}" = "nodes validate" ]; then
	jq -e '.schemaVersion == 1 and (.generation | type == "string" and length > 0)' "${XKEEN_NODES_PATH:?}" >/dev/null
	exit 0
fi
if [ "${1:-} ${2:-}" = "nodes render" ]; then
	[ "${3:-}" = "--output" ] && [ -n "${4:-}" ] || exit 1
	generation="$(jq -r '.generation' "${XKEEN_NODES_PATH:?}")"
	printf '{"schemaVersion":1,"generation":"%s"}\n' "$generation" > "$4"
	exit 0
fi
if [ "${1:-} ${2:-}" = "nodes reconcile-runtime" ]; then
	root="${XKEEN_CONTROL_TEST_ROOT:?}"
	registry_generation="$(jq -r '.generation' "${XKEEN_NODES_PATH:?}")"
	outbound_generation="$(jq -r '.generation' "${XKEEN_ACTIVE_OUTBOUNDS:?}")"
	[ "$registry_generation" = "$outbound_generation" ] || exit 1
	printf '%s\n' "$registry_generation" >> "$root/reconcile-calls"
	if [ "${XKEEN_TEST_RECONCILE_FAIL_GENERATION:-}" = "$registry_generation" ]; then
		exit 1
	fi
	printf '%s\n' "$registry_generation" > "$root/runtime-generation"
	exit 0
fi
exit 2
EOF_BIN
	chmod 755 "$path"
}

make_legacy_binary() {
	path="$1"
	cat > "$path" <<'EOF_LEGACY'
#!/bin/sh
exit 0
EOF_LEGACY
	chmod 755 "$path"
}

make_legacy_init() {
	path="$1"
	cat > "$path" <<'EOF_INIT'
#!/bin/sh
set -eu
root="${XKEEN_CONTROL_TEST_ROOT:?}"
case "${1:-}" in
	stop)
		generation="${XKEEN_TEST_STOP_GENERATION:-applied}"
		printf '{"schemaVersion":1,"generation":"%s"}\n' "$generation" > "$root/opt/etc/xkeen-control/secrets/nodes.json.new"
		mv -f "$root/opt/etc/xkeen-control/secrets/nodes.json.new" "$root/opt/etc/xkeen-control/secrets/nodes.json"
		printf '{"schemaVersion":1,"generation":"%s"}\n' "$generation" > "$root/opt/etc/xray/configs/04_outbounds.json.new"
		mv -f "$root/opt/etc/xray/configs/04_outbounds.json.new" "$root/opt/etc/xray/configs/04_outbounds.json"
		;;
	start|status|restart) ;;
	*) exit 2 ;;
esac
EOF_INIT
	chmod 755 "$path"
}

make_candidate_init() {
	path="$1"
	cat > "$path" <<'EOF_INIT'
#!/bin/sh
case "${1:-}" in start|stop|status|restart) exit 0 ;; *) exit 2 ;; esac
EOF_INIT
	chmod 755 "$path"
}

setup_root() {
	root="$1"
	mkdir -p "$root/opt/sbin" "$root/opt/etc/init.d" "$root/opt/libexec" \
		"$root/opt/etc/xkeen-control/state" "$root/opt/etc/xkeen-control/previous" \
		"$root/opt/etc/xkeen-control/secrets" "$root/opt/etc/xray/configs" \
		"$root/tmp/xkeen-control/panel-update"
	make_legacy_binary "$root/opt/sbin/xkeen-control"
	make_legacy_init "$root/opt/etc/init.d/S99xkeen-control"
	printf '%s\n' '127.0.0.1:8787' > "$root/opt/etc/xkeen-control/listen-address"
	printf '%s\n' '{"schemaVersion":1,"generation":"legacy"}' > "$root/opt/etc/xkeen-control/secrets/nodes.json"
	printf '%s\n' '{"schemaVersion":1,"generation":"legacy"}' > "$root/opt/etc/xray/configs/04_outbounds.json"
	printf '%s\n' legacy > "$root/runtime-generation"

	candidate="$root/tmp/xkeen-control/panel-update"
	make_candidate_binary "$candidate/xkeen-control-linux-arm64"
	make_candidate_init "$candidate/S99xkeen-control"
	cp "$ROOT/scripts/xkeen-control-updater" "$candidate/xkeen-control-updater"
	chmod 755 "$candidate/xkeen-control-updater"
	printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$candidate/installed-release.json"
	: > "$candidate/.legacy-adoption"
	chmod 600 "$candidate/.legacy-adoption"
}

commit_root="$tmp/commit-root"
setup_root "$commit_root"
PATH="$fakebin:$PATH" \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$commit_root" \
XKEEN_TEST_STOP_GENERATION=applied \
sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ "$(jq -r '.generation' "$commit_root/opt/etc/xkeen-control/secrets/nodes.json")" = applied ]
[ "$(jq -r '.generation' "$commit_root/opt/etc/xray/configs/04_outbounds.json")" = applied ]
[ "$(cat "$commit_root/runtime-generation")" = applied ]
[ "$(cat "$commit_root/reconcile-calls")" = applied ]

restore_root="$tmp/restore-root"
setup_root "$restore_root"
PATH="$fakebin:$PATH" \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$restore_root" \
XKEEN_TEST_STOP_GENERATION=applied \
XKEEN_TEST_RECONCILE_FAIL_GENERATION=applied \
sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ "$(jq -r '.generation' "$restore_root/opt/etc/xkeen-control/secrets/nodes.json")" = legacy ]
[ "$(jq -r '.generation' "$restore_root/opt/etc/xray/configs/04_outbounds.json")" = legacy ]
[ "$(cat "$restore_root/runtime-generation")" = legacy ]
[ "$(sed -n '1p' "$restore_root/reconcile-calls")" = applied ]
[ "$(sed -n '2p' "$restore_root/reconcile-calls")" = legacy ]
[ "$(wc -l < "$restore_root/reconcile-calls" | tr -d '[:space:]')" -eq 2 ]

echo "legacy reconcile fixture passed"
