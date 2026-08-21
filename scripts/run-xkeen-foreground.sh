#!/bin/sh
set -eu

ACTION="${1:-}"
XKEEN_BIN="${XKEEN_BIN:-xkeen}"
LIFECYCLE_TIMEOUT="${XKEEN_RESTART_TIMEOUT:-60}"

case "$ACTION" in
  -restart|-start|-stop) ;;
  *) echo "ERROR: expected -restart, -start, or -stop" >&2; exit 2 ;;
esac
case "$LIFECYCLE_TIMEOUT" in
  ''|*[!0-9]*) echo "ERROR: XKeen lifecycle timeout is invalid" >&2; exit 2 ;;
esac
[ "$LIFECYCLE_TIMEOUT" -ge 1 ] && [ "$LIFECYCLE_TIMEOUT" -le 120 ] || {
  echo "ERROR: XKeen lifecycle timeout is outside 1..120 seconds" >&2
  exit 2
}
export XKEEN_FOREGROUND=1
"$XKEEN_BIN" "$ACTION" &
LAUNCHER_PID=$!
trap 'kill "$LAUNCHER_PID" 2>/dev/null || true; exit 130' HUP INT TERM

ELAPSED=0
while kill -0 "$LAUNCHER_PID" 2>/dev/null; do
  if [ "$ELAPSED" -ge "$LIFECYCLE_TIMEOUT" ]; then
    kill "$LAUNCHER_PID" 2>/dev/null || true
    sleep 1
    kill -9 "$LAUNCHER_PID" 2>/dev/null || true
    wait "$LAUNCHER_PID" 2>/dev/null || true
    trap - HUP INT TERM
    echo "ERROR: XKeen lifecycle command timed out" >&2
    exit 124
  fi
  sleep 1
  ELAPSED=$((ELAPSED + 1))
done

set +e
wait "$LAUNCHER_PID"
RESULT=$?
set -e
trap - HUP INT TERM
exit "$RESULT"
