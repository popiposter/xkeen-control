#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
export XKEEN_CONTROL_BIN="$TMP/panel"
export XKEEN_CONTROL_PIDFILE="$TMP/panel.pid"
export XKEEN_CONTROL_LOG="$TMP/panel.log"
export PANEL_PATH_MARKER="$TMP/environment"
trap 'sh "$ROOT/packaging/S99xkeen-control" stop >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT
cat > "$TMP/panel.c" <<'EOF'
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
int main(int argc, char **argv) {
 if (argc == 3 && !strcmp(argv[1], "setup") && !strcmp(argv[2], "guard"))
  return getenv("PANEL_GUARD_DENY") && strcmp(getenv("PANEL_GUARD_DENY"), "0");
 if (argc > 1) { for (;;) sleep(1); }
 FILE *f = fopen(getenv("PANEL_PATH_MARKER"), "w");
 if (!f) return 2;
 fprintf(f, "%s\n%s\n", getenv("PATH"), getenv("XKEEN_CONTROL_TELEGRAM_SOCKS_ADDR"));
 fclose(f);
 for (;;) sleep(1);
}
EOF
cc -o "$XKEEN_CONTROL_BIN" "$TMP/panel.c"
chmod 700 "$XKEEN_CONTROL_BIN"
wait_spawn() {
 i=0
 while [ ! -s "$PANEL_PATH_MARKER" ]; do
  i=$((i + 1)); [ "$i" -lt 20 ] || return 1
  sleep .1
 done
}
export XKEEN_CONTROL_TELEGRAM_SOCKS_ADDR=127.0.0.1:5310
# Reproduce Entware's inherited firmware-first ordering.
PATH="/opt/usr/bin:/usr/bin:/bin" sh "$ROOT/packaging/S99xkeen-control" start >/dev/null
wait_spawn
case "$(head -n 1 "$PANEL_PATH_MARKER")" in
    /opt/bin:/opt/sbin:*) ;;
    *) echo 'panel init selected firmware tools before Entware' >&2; exit 1 ;;
esac
[ "$(tail -n 1 "$PANEL_PATH_MARKER")" = 127.0.0.1:5310 ]
# Real procps must see the detached child without a controlling terminal.
sh "$ROOT/packaging/S99xkeen-control" status >/dev/null
sh "$ROOT/packaging/S99xkeen-control" stop >/dev/null
[ ! -e "$XKEEN_CONTROL_PIDFILE" ]
rm -f "$PANEL_PATH_MARKER"
if PANEL_GUARD_DENY=1 sh "$ROOT/packaging/S99xkeen-control" start >/dev/null 2>&1; then
 echo 'panel init ignored setup guard rejection' >&2; exit 1
fi
[ ! -e "$XKEEN_CONTROL_PIDFILE" ] && [ ! -e "$PANEL_PATH_MARKER" ]

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
wait_spawn
sh "$ROOT/packaging/S99xkeen-control" status >/dev/null

# A CLI sharing the executable is not the daemon, even with a stale PID file.
sh "$ROOT/packaging/S99xkeen-control" stop >/dev/null
"$XKEEN_CONTROL_BIN" version --json &
cli_pid=$!
printf '%s\n' "$cli_pid" > "$XKEEN_CONTROL_PIDFILE"
if sh "$ROOT/packaging/S99xkeen-control" status >/dev/null; then
 kill "$cli_pid"; wait "$cli_pid" || true
 echo 'version CLI mistaken for daemon' >&2; exit 1
fi
sh "$ROOT/packaging/S99xkeen-control" stop >/dev/null
kill -0 "$cli_pid"
kill "$cli_pid"; wait "$cli_pid" || true
