#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
export XKEEN_CONTROL_BIN="$TMP/panel"
export XKEEN_CONTROL_PIDFILE="$TMP/panel.pid"
export XKEEN_CONTROL_LOG="$TMP/panel.log"
export PANEL_PATH_MARKER="$TMP/environment"
trap 'sh "$ROOT/packaging/S99xkeen-control" stop >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT
cat > "$XKEEN_CONTROL_BIN" <<'EOF'
#!/bin/sh
printf '%s\n' "$PATH" "${XKEEN_CONTROL_TELEGRAM_SOCKS_ADDR:-}" > "$PANEL_PATH_MARKER"
while :; do sleep 1; done
EOF
chmod 700 "$XKEEN_CONTROL_BIN"
export XKEEN_CONTROL_TELEGRAM_SOCKS_ADDR=127.0.0.1:5310
# Reproduce Entware's inherited firmware-first ordering.
PATH="/opt/usr/bin:/usr/bin:/bin" sh "$ROOT/packaging/S99xkeen-control" start >/dev/null
case "$(head -n 1 "$PANEL_PATH_MARKER")" in
    /opt/bin:/opt/sbin:*) ;;
    *) echo 'panel init selected firmware tools before Entware' >&2; exit 1 ;;
esac
[ "$(tail -n 1 "$PANEL_PATH_MARKER")" = 127.0.0.1:5310 ]
# Real procps must see the detached child without a controlling terminal.
sh "$ROOT/packaging/S99xkeen-control" status >/dev/null
sh "$ROOT/packaging/S99xkeen-control" stop >/dev/null
[ ! -e "$XKEEN_CONTROL_PIDFILE" ]

# Model the actual router's minimal BusyBox interface: only w is supported.
mkdir "$TMP/tools"
cat > "$TMP/tools/ps" <<'EOF'
#!/bin/sh
[ "$#" -eq 1 ] && [ "$1" = w ] || exit 1
exec /bin/ps -eo pid,args
EOF
chmod 700 "$TMP/tools/ps"
export PATH="$TMP/tools:/usr/bin:/bin"
sh "$ROOT/packaging/S99xkeen-control" start >/dev/null
sh "$ROOT/packaging/S99xkeen-control" status >/dev/null
