#!/bin/sh
set -eu
set -f

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
command -v jq >/dev/null 2>&1 || { echo "jq is required for updater fixture" >&2; exit 1; }

tmp="$(mktemp -d /tmp/xkeen-updater-test.XXXXXX)"
background_pid=""
cleanup() {
	if [ -n "$background_pid" ]; then
		kill "$background_pid" 2>/dev/null || true
		wait "$background_pid" 2>/dev/null || true
	fi
	rm -rf "$tmp"
}
trap cleanup EXIT
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
if [ "\${1:-}" = "nodes" ] && [ "\${2:-}" = "validate" ]; then
	jq -e '.schemaVersion == 1 and (.generation | type == "string" and length > 0)' "\${XKEEN_NODES_PATH:?}" >/dev/null
	exit 0
fi
if [ "\${1:-}" = "nodes" ] && [ "\${2:-}" = "render" ]; then
	[ "\${3:-}" = "--output" ] && [ -n "\${4:-}" ] || exit 1
	generation="\$(jq -r '.generation' "\${XKEEN_NODES_PATH:?}")"
	printf '{"schemaVersion":1,"generation":"%s"}\\n' "\$generation" > "\$4"
	exit 0
fi
if [ "\${1:-}" = "nodes" ] && [ "\${2:-}" = "reconcile-runtime" ]; then
	root="\${XKEEN_CONTROL_TEST_ROOT:?}"
	registry_generation="\$(jq -r '.generation' "\${XKEEN_NODES_PATH:?}")"
	outbound_generation="\$(jq -r '.generation' "\${XKEEN_ACTIVE_OUTBOUNDS:?}")"
	[ "\$registry_generation" = "\$outbound_generation" ] || exit 1
	printf '%s\\n' "\$registry_generation" >> "\$root/reconcile-calls"
	if [ "\${XKEEN_TEST_RECONCILE_FAIL_GENERATION:-}" = "\$registry_generation" ]; then
		exit 1
	fi
	printf '%s\\n' "\$registry_generation" > "\$root/runtime-generation"
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

make_legacy_apply_binary() {
	path="$1"
	cat > "$path" <<'EOF_LEGACY_APPLY'
#!/bin/sh
set -eu
root="${XKEEN_CONTROL_TEST_ROOT:?}"
nodes="$root/opt/etc/xkeen-control/secrets/nodes.json"
outbounds="$root/opt/etc/xray/configs/04_outbounds.json"
case "${1:-}" in
	hold)
		printf '%s\n' ready > "${XKEEN_LEGACY_APPLY_READY:?}"
		IFS= read -r _ < "${XKEEN_LEGACY_APPLY_CONTINUE:?}"
		;;
	apply)
		printf '%s\n' '{"schemaVersion":1,"generation":"in-flight"}' > "$nodes.new"
		mv -f "$nodes.new" "$nodes"
		printf '%s\n' ready > "${XKEEN_LEGACY_APPLY_READY:?}"
		IFS= read -r _ < "${XKEEN_LEGACY_APPLY_CONTINUE:?}"
		printf '%s\n' '{"schemaVersion":1,"generation":"applied"}' > "$nodes.new"
		mv -f "$nodes.new" "$nodes"
		printf '%s\n' '{"schemaVersion":1,"generation":"applied"}' > "$outbounds.new"
		mv -f "$outbounds.new" "$outbounds"
		printf '%s\n' complete > "$root/apply-complete"
		;;
	*)
		exit 2
		;;
esac
EOF_LEGACY_APPLY
	chmod 755 "$path"
}

make_recording_init() {
	path="$1"
	cat > "$path" <<'EOF_RECORDING_INIT'
#!/bin/sh
set -eu
root="${XKEEN_CONTROL_TEST_ROOT:?}"
case "${1:-}" in
	stop)
		: > "$root/legacy-stop-called"
		;;
	start|status|restart)
		;;
	*)
		exit 2
		;;
esac
EOF_RECORDING_INIT
	chmod 755 "$path"
}

make_racy_init() {
	path="$1"
	cat > "$path" <<'EOF_RACY_INIT'
#!/bin/sh
set -eu
root="${XKEEN_CONTROL_TEST_ROOT:?}"
nodes="$root/opt/etc/xkeen-control/secrets/nodes.json"
case "${1:-}" in
	stop)
		printf '%s\n' '{"schemaVersion":1,"generation":"interrupted"}' > "$nodes.new"
		mv -f "$nodes.new" "$nodes"
		: > "$root/legacy-stop-called"
		;;
	start|status|restart)
		;;
	*)
		exit 2
		;;
esac
EOF_RACY_INIT
	chmod 755 "$path"
}

make_activation_racy_init() {
	path="$1"
	cat > "$path" <<'EOF_ACTIVATION_RACY_INIT'
#!/bin/sh
set -eu
root="${XKEEN_CONTROL_TEST_ROOT:?}"
nodes="$root/opt/etc/xkeen-control/secrets/nodes.json"
outbounds="$root/opt/etc/xray/configs/04_outbounds.json"
case "${1:-}" in
	stop)
		# Model the legacy transaction window after both persistent writes but
		# before Xray activation/readiness/inventory has committed.
		printf '%s\n' '{"schemaVersion":1,"generation":"applied"}' > "$nodes.new"
		mv -f "$nodes.new" "$nodes"
		printf '%s\n' '{"schemaVersion":1,"generation":"applied"}' > "$outbounds.new"
		mv -f "$outbounds.new" "$outbounds"
		: > "$root/legacy-stop-called"
		;;
	start|status|restart)
		;;
	*)
		exit 2
		;;
esac
EOF_ACTIVATION_RACY_INIT
	chmod 755 "$path"
}

setup_generation() {
	root="$1"
	listen="${2-192.168.10.2:8787}"
	mkdir -p "$root/opt/sbin" "$root/opt/etc/init.d" "$root/opt/libexec" \
		"$root/opt/etc/xkeen-control/state" "$root/opt/etc/xkeen-control/previous" \
		"$root/opt/etc/xkeen-control/secrets" "$root/opt/etc/xray/configs" \
		"$root/tmp/xkeen-control/panel-update"
	make_binary "$root/opt/sbin/xkeen-control" "1.0.0" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" stable
	make_init "$root/opt/etc/init.d/S99xkeen-control"
	cp "$ROOT/scripts/xkeen-control-updater" "$root/opt/libexec/xkeen-control-updater"
	chmod 755 "$root/opt/libexec/xkeen-control-updater"
	if [ -n "$listen" ]; then printf '%s\n' "$listen" > "$root/opt/etc/xkeen-control/listen-address"; fi
	printf '%s\n' '{"product":"xkeen-control","version":"1.0.0","sourceCommit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","channel":"stable"}' > "$root/opt/etc/xkeen-control/state/installed-release.json"
	printf '%s\n' '{"schemaVersion":1,"generation":"legacy"}' > "$root/opt/etc/xkeen-control/secrets/nodes.json"
	printf '%s\n' '{"schemaVersion":1,"generation":"legacy"}' > "$root/opt/etc/xray/configs/04_outbounds.json"
	printf '%s\n' legacy > "$root/runtime-generation"

	candidate="$root/tmp/xkeen-control/panel-update"
	make_binary "$candidate/xkeen-control-linux-arm64" "1.2.3" "cccccccccccccccccccccccccccccccccccccccccb"7F&ÆP –Ö¶Uö–æ—B"F6æF–FFRõ3“—†¶VVâÖ6öçG&öÂ  –7"E$ôõB÷67&—G2÷†¶VVâÖ6öçG&öÂ×WFFW"""F6æF–FFR÷†¶VVâÖ6öçG&öÂ×WFFW"  –6†ÖöBsSR"F6æF–FFR÷†¶VVâÖ6öçG&öÂ×WFFW"  —&–çFbrW5Æârw²'&öGV7B#¢'†¶VVâÖ6öçG&öÂ"Â'fW'6–öâ#¢#ã"ã2"Â'6÷W&6T6öÖÖ—B#¢&6666666666666666666666666666666666666662"Â&6†ææVÂ#¢'7F&ÆR'Òrâ"F6æF–FFRö–ç7FÆÆVB×&VÆV6Ræ§6öâ  —&–çFbrW5Æârw²'fW'6–öâ#¢#ã"ã2"Â'6÷W&6T6öÖÖ—B#¢&6666666666666666666666666666666666666662"Â&6†ææVÂ#¢'7F&ÆR'Òrâ"F6æF–FFR÷&VÆV6RÖÖæ–fW7Bæ§6öâ §Ð §6WGWöÆVv7•övVæW&F–öâ‚’° —&ö÷CÒ"C  –Æ—7FVãÒ"G³"Ó“"ãc‚ãã#£ƒsƒwÒ  –Ö¶F—"×"G&ö÷Bö÷B÷6&–â""G&ö÷Bö÷BöWF2ö–æ—BæB""G&ö÷Bö÷BöÆ–&W†V2"À ’"G&ö÷Bö÷BöWF2÷†¶VVâÖ6öçG&öÂ÷7FFR""G&ö÷Bö÷BöWF2÷†¶VVâÖ6öçG&öÂ÷&Wf–÷W2"À ’"G&ö÷Bö÷BöWF2÷†¶VVâÖ6öçG&öÂ÷6V7&WG2""G&ö÷Bö÷BöWF2÷‡&’ö6öæf–w2"À ’"G&ö÷B÷F×÷†¶VVâÖ6öçG&öÂ÷æVÂ×WFFR  –Ö¶Uö&–æ'’"G&ö÷Bö÷B÷6&–â÷†¶VVâÖ6öçG&öÂ"#ãã"&"7F&ÆP –Ö¶Uö–æ—B"G&ö÷Bö÷BöWF2ö–æ—BæBõ3“—†¶VVâÖ6öçG&öÂ  —&–çFbrW5Æârr2ÆVv7’vVæW&F–öârãâ"G&ö÷Bö÷BöWF2ö–æ—BæBõ3“—†¶VVâÖ6öçG&öÂ  ––b²Öâ"FÆ—7FVâ"Ó²F†Vâ&–çFbrW5Æâr"FÆ—7FVâ"â"G&ö÷Bö÷BöWF2÷†¶VVâÖ6öçG&öÂöÆ—7FVâÖFG&W72#²f —&–çFbrW5Æârw²'66†VÖfW'6–öâ#£Â&vVæW&F–öâ#¢&ÆVv7’'Òrâ"G&ö÷Bö÷BöWF2÷†¶VVâÖ6öçG&öÂ÷6V7&WG2öæöFW2æ§6öâ  —&–çFbrW5Æârw²'66†VÖfW'6–öâ#£Â&vVæW&F–öâ#¢&ÆVv7’'Òrâ"G&ö÷Bö÷BöWF2÷‡&’ö6öæf–w2óEö÷WF&÷VæG2æ§6öâ  —&–çFbrW5ÆârÆVv7’â"G&ö÷B÷'VçF–ÖRÖvVæW&F–öâ   –6æF–FFSÒ"G&ö÷B÷F×÷†¶VVâÖ6öçG&öÂ÷æVÂ×WFFR  –Ö¶Uö&–æ'’"F6æF–FFR÷†¶VVâÖ6öçG&öÂÖÆ–çW‚Ö&ÓcB"#ã"ã2"&66666666666666666666666666666666666666666" stable
	make_init "$candidate/S99xkeen-control"
	cp "$ROOT/scripts/xkeen-control-updater" "$candidate/xkeen-control-updater"
	chmod 755 "$candidate/xkeen-control-updater"
	printf '%s\n' '{"product":"xkeen-control","version":"1.2.3","sourceCommit":"cccccccccccccccccccccccccccccccccccccccc","channel":"stable"}' > "$candidate/installed-release.json"
	: > "$candidate/.legacy-adoption"
	chmod 600 "$candidate/.legacy-adoption"
}

root="$tmp/root"
setup_generation "$root"
PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$root" \
sh "$ROOT/scripts/xkeen-control-updater" install

grep -Fq '"version":"1.2.3"' "$root/opt/etc/xkeen-control/state/installed-release.json"
[ -f "$root/opt/etc/xkeen-control/previous/panel/xkeen-control-linux-arm64" ]
"$root/opt/sbin/xkeen-control" version --json | jq -e '.version == "1.2.3"' >/dev/null

PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$root" \
sh "$root/opt/libexec/xkeen-control-updater" rollback

grep -Fq '"version":"1.0.0"' "$root/opt/etc/xkeen-control/state/installed-release.json
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
sh "$ROOT/scripts/xkeen-control-updater" adopt

[ "$(sha256sum "$legacy_root/opt/sbin/xkeen-control" | awk '{print $1}')" != "$legacy_binary_before" ]
[ "$(sha256sum "$legacy_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" != "$legacy_init_before" ]
[ -x "$legacy_root/opt/libexec/xkeen-control-updater" ]
[ -f "$legacy_root/opt/etc/xkeen-control/previous/panel/.helper-absent" ]
[ ! -e "$legacy_root/opt/etc/xkeen-control/previous/panel/xkeen-control-updater" ]

PATH="$fakebin:$PATH" \
EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$legacy_root" \
sh "$legacy_root/opt/libexec/xkeen-control-updater" rollback

[ "$(sha256sum "$legacy_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$legacy_binary_before" ]
[ "$(sha256sum "$legacy_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')" = "$legacy_init_before" ]
[ ! -e "$legacy_root/opt/libexec/xkeen-control-updater" ]
[ ! -e "$legacy_root/opt/etc/xkeen-control/state/installed-release.json" ]
[ ! -e "$legacy_root/opt/etc/xkeen-control/previous/panel" ]

mkdir -p "$legacy_root/tmp/xkeen-control/panel-update"
make_binary "$legacy_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" "1.2.3" "cccccccccccccccccccccccccccccccccccccccc""7F&ÆP¦Ö¶Uö–æ—B"FÆVv7•÷&ö÷B÷F×÷†¶VVâÖ6öçG&öÂ÷æVÂ×WFFRõ3“—†¶VVâÖ6öçG&öÂ ¦7"E$ôõB÷67&—G2÷†¶VVâÖ6öçG&öÂ×WFFW"""FÆVv7•÷&ö÷B÷F×÷†¶VVâÖ6öçG&öÂ÷æVÂ×WFFR÷†¶VVâÖ6öçG&öÂ×WFFW" ¦6†ÖöBsSR"FÆVv7•÷&ö÷B÷F×÷†¶VVâÖ6öçG&öÂ÷æVÂ×WFFR÷†¶VVâÖ6öçG&öÂ×WFFW" §&–çFbrW5Æârw²'&öGV7B#¢'†¶VVâÖ6öçG&öÂ"Â'fW'6–öâ#¢#ã"ã2"Â'6÷W&6T6öÖÖ—B#¢&6666666666666666666666666666666666666662","channel":"stable"}' > "$legacy_root/tmp/xkeen-control/panel-update/installed-release.json"
: > "$legcy_root/tmp/xkeen-control/panel-update/.legacy-adoption"
chmod 600 "$legcy_root/tmp/xkeen-control/panel-update/.legacy-adoption"
PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$legcy_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt

[ -x "$legacy_root/opt/libexec/xkeen-control-updater" ]

jq -e '.version == "1.2.3"' "$legcy_root/opt/etc/xkeen-control/state/installed-release.json" >/dev/null

concurrent_root="$tmp/concurrent-root"
setup_legacy_generation "$concurrent_root"
make_legacy_apply_binary "$concurrent_root/opt/sbin/xkeen-control"
make_recording_init "$concurrent_root/opt/etc/init.d/S99xkeen-control"
concurrent_ready="$concurrent_root/apply-ready"
concurrent_continue="$concurrent_root/apply-continue"
mkfifo "$concurrent_ready" "$concurrent_continue"
concurrent_binary_before="$(sha256sum "$concurrent_root/opt/sbin/xkeen-control" | awk '{print $1}')"
XKEEN_CONTROL_TEST_ROOT="$concurrent_root" \
XKEEN_LEGACY_APPLY_READY="$concurrent_ready" \
XKEEN_LEGACY_APPLY_CONTINUE="$concurrent_continue" \
"$concurrent_root/opt/sbin/xkeen-control" apply &
background_pid="$!"
IFS= read -r _ < "$concurrent_ready"

if PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$concurrent_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null 2>&1; then
	echo "adoption unexpectedly crossed a blocked legacy Apply" >&2
	exit 1
fi
[ ! -e "$concurrent_root/legacy-stop-called" ]
[ "$(sha256sum "$concurrent_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$concurrent_binary_before" ]
[ ! -e "$concurrent_root/opt/libexec/xkeen-control-updater" ]
[ "$(jq -r '.generation' "$concurrent_root/opt/etc/xkeen-control/secrets/nodes.json")" = "in-flight" ]
printf '%s\n' continue > "$concurrent_continue"
wait "$background_pid"
background_pid=""
[ -f "$concurrent_root/apply-complete" ]
[ "$(jq -r '.generation' "$concurrent_root/opt/etc/xkeen-control/secrets/nodes.json")" = "applied" ]
[ "$(jq -r '.generation' "$concurrent_root/opt/etc/xray/configs/04_outbounds.json")" = "applied" ]

concurrent_rendered="$concurrent_root/rendered.json"
XKEEN_NODES_PATH="$concurrent_root/opt/etc/xkeen-control/secrets/nodes.json" \
"$concurrent_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" nodes render --output "$concurrent_rendered"
[ "$(sha256sum "$concurrent_rendered" | awk '{print $1}')" = "$(sha256sum "$concurrent_root/opt/etc/xray/configs/04_outbounds.json" | awk '{print $1}')" ]
rm -f "$concurrent_rendered"

PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$concurrent_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ -x "$concurrent_root/opt/libexec/xkeen-control-updater" ]

running_root="$tmp/running-root"
setup_legacy_generation "$running_root"
make_legacy_apply_binary "$running_root/opt/sbin/xkeen-control"
make_recording_init "$running_root/opt/etc/init.d/S99xkeen-control"
running_ready="$running_root/hold-ready"
running_continue="$running_root/hold-continue"
mkfifo "$running_ready" "$running_continue"
running_binary_before="$(sha256sum "$running_root/opt/sbin/xkeen-control" | awk '{print $1}')"
XKEEN_CONTROL_TEST_ROOT="$running_root" \
XKEEN_LEGACY_APPLY_READY="$running_ready" \
XKEEN_LEGACY_APPLY_CONTINUE="$running_continue" \
"$running_root/opt/sbin/xkeen-control" hold &
background_pid="$!"
IFS= read -r _ < "$running_ready"
if PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$running_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null 2>&1; then
	echo "adoption unexpectedly crossed a running legacy panel" >&2
	exit 1
fi
[ -f "$running_root/legacy-stop-called" ]
[ "$(sha256sum "$running_root/opt/sbin/xkeen-control" | awk '{print $1}')" = "$running_binary_before" ]
[ ! -e "$running_root/opt/libexec/xkeen-control-updater" ]
[ "$(jq -r '.generation' "$running_root/opt/etc/xkeen-control/secrets/nodes.json")" = "legacy" ]
printf '%s\n' continue > "$running_continue"
wait "$background_pid"
background_pid=""
PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$running_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ -x "$running_root/opt/libexec/xkeen-control-updater" ]

recovery_root="$tmp/recovery-root"
setup_legacy_generation "$recovery_root"
make_racy_init "$recovery_root/opt/etc/init.d/S99xkeen-control"
PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$recovery_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ -f "$recovery_root/legacy-stop-called" ]
[ "$(jq -r '.generation' "$recovery_root/opt/etc/xkeen-control/secrets/nodes.json")" = "legacy" ]
[ "$(jq -r '.generation' "$recovery_root/opt/etc/xray/configs/04_outbounds.json")" = "legacy" ]
[ "$(cat "$recovery_root/runtime-generation")" = "legacy" ]
[ ! -e "$recovery_root/opt/etc/xkeen-control/state/panel-adoption-recovery" ]

activation_commit_root="$tmp/activation-commit-root"
setup_legacy_generation "$activation_commit_root"
make_activation_racy_init "$activation_commit_root/opt/etc/init.d/S99xkeen-control"
PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$activation_commit_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ "$(jq -r '.generation' "$activation_commit_root/opt/etc/xkeen-control/secrets/nodes.json")" = "applied" ]
[ "$(jq -r '.generation' "$activation_commit_root/opt/etc/xray/configs/04_outbounds.json")" = "applied" ]
[ "$(cat "$activation_commit_root/runtime-generation")" = "applied" ]
[ "$(cat "$activation_commit_root/reconcile-calls")" = "applied" ]
[ ! -e "$activation_commit_root/opt/etc/xkeen-control/state/panel-adoption-recovery" ]

activation_restore_root="$tmp/activation-restore-root"
setup_legacy_generation "$activation_restore_root"
make_activation_racy_init "$activation_restore_root/opt/etc/init.d/S99xkeen-control"
PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$activation_restore_root" \
	XKEEN_TEST_RECONCILE_FAIL_GENERATION=applied \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null
[ "$(jq -r '.generation' "$activation_restore_root/opt/etc/xkeen-control/secrets/nodes.json")" = "legacy" ]
[ "$(jq -r '.generation' "$activation_restore_root/opt/etc/xray/configs/04_outbounds.json")" = "legacy" ]
[ "$(cat "$activation_restore_root/runtime-generation")" = "legacy" ]
[ "$(sed -n '1p' "$activation_restore_root/reconcile-calls")" = "applied" ]
[ "$(sed -n '2p' "$activation_restore_root/reconcile-calls")" = "legacy" ]
[ "$(wc -l < "$activation_restore_root/reconcile-calls" | tr -d '[:space:]')" -eq 2 ]
[ ! -e "$activation_restore_root/opt/etc/xkeen-control/state/panel-adoption-recovery" ]

legacy_failure_root="$tmp/legacy-failure-root"
setup_legacy_generation "$legacy_failure_root"
legacy_failure_binary_before="$(sha256sum "$legacy_failure_root/opt/sbin/xkeen-control" | awk '{print $1}')"
legcy_failure_init_before="$(sha256sum "$legacy_failure_root/opt/etc/init.d/S99xkeen-control" | awk '{print $1}')"
make_binary "$legacy_failure_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" "9.9.9" "dddddddddddddddddddddddddddddddddddddddd" stable
if PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$legacy_failure_root" \
	sh "$ROOT/scripts/xkeen-control-updater" adopt >/dev/null 2>&1; then
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
sh "$ROOT/scripts/xkeen-control-updater" install
grep -Fq '"version":"1.2.3"' "$loopback_root/opt/etc/xkeen-control/state/installed-release.json"

failure_root="$tmp/failure-root"
setup_generation "$failure_root"
make_binary "$failure_root/tmp/xkeen-control/panel-update/xkeen-control-linux-arm64" "9.9.9" "ddddddddddddddddddddddddddddddddddddddddddd" stable
if PATH="$fakebin:$PATH" \
	EXPECTED_HEALTH_URL='http://192.168.10.2:8787/healthz' \
	XKEEN_CONTROL_TEST_MODE=1 \
	XKEEN_CONTROL_TEST_ROOT="$failure_root" \
	sh "$ROOT/scripts/xkeen-control-updater" install >/dev/null 2>&1; then
	echo "mismatched candidate unexpectedly committed" >&2
	exit 1
fi
grep -Fq '"version":"1.0.0"' "$failure_root/opt/etc/xkeen-control/state/installed-release.json"
"$failure_root/opt/sbin/xkeen-control" version --json | jq -e '.version == "1.0.0"' >/dev/null
