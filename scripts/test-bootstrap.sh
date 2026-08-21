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

installer="$fixture/install.sh"
sed 's/^STABLE_RELEASE_VERSION=""$/STABLE_RELEASE_VERSION="1.2.3"/' "$ROOT/scripts/install.sh" > "$installer"
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

cat > "$fixture/xkeen-control-updater" <<'EOF_UPDATER'
#!/bin/sh
exit 0
EOF_UPDATER
chmod 755 "$fixture/xkeen-control-updater"

manifest="$fixture/release-manifest.json"
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
case "$url" in
	http://127.0.0.1:8787/healthz)
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

PATH="$fakebin:$PATH" \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$testroot" \
XKEEN_CONTROL_FIXTURE_DIR="$fixture" \
sh "$installer" >/dev/null

[ -x "$testroot/opt/sbin/xkeen-control" ]
[ -x "$testroot/opt/etc/init.d/S99xkeen-control" ]
[ -x "$testroot/opt/libexec/xkeen-control-updater" ]
[ -f "$testroot/opt/etc/xkeen-control/auth/password.bcrypt" ]
[ "$(wc -l < "$testroot/bootstrap-calls" | tr -d '[:space:]')" -eq 1 ]
before_hash="$(sha256sum "$testroot/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')"

PATH="$fakebin:$PATH" \
XKEEN_CONTROL_TEST_MODE=1 \
XKEEN_CONTROL_TEST_ROOT="$testroot" \
XKEEN_CONTROL_FIXTURE_DIR="$fixture" \
sh "$installer" >/dev/null

after_hash="$(sha256sum "$testroot/opt/etc/xkeen-control/auth/password.bcrypt" | awk '{print $1}')"
[ "$before_hash" = "$after_hash" ]
[ "$(wc -l < "$testroot/bootstrap-calls" | tr -d '[:space:]')" -eq 1 ]
grep -Fq 'self-update --channel stable --apply' "$testroot/self-update-calls"
