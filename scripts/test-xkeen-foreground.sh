#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/xkeen" <<'EOF'
#!/bin/sh
[ "${XKEEN_FOREGROUND:-}" = "1" ] || exit 91
printf started > "$XKEEN_FAKE_MARKER"
sleep "${XKEEN_FAKE_DELAY:-0}"
printf '%s\n' completed >> "$XKEEN_FAKE_MARKER"
EOF
chmod 700 "$TMP/xkeen"

started="$(date +%s)"
XKEEN_BIN="$TMP/xkeen" XKEEN_FAKE_MARKER="$TMP/marker" XKEEN_FAKE_DELAY=1 XKEEN_RESTART_TIMEOUT=3 \
  "$ROOT/scripts/run-xkeen-foreground.sh" -restart
elapsed="$(( $(date +%s) - started ))"
[ "$elapsed" -ge 1 ]
grep -q '^startedcompleted$' "$TMP/marker"

started="$(date +%s)"
if XKEEN_BIN="$TMP/xkeen" XKEEN_FAKE_MARKER="$TMP/timeout-marker" XKEEN_FAKE_DELAY=3 XKEEN_RESTART_TIMEOUT=1 \
  "$ROOT/scripts/run-xkeen-foreground.sh" -restart 2>/dev/null; then
  echo "ERROR: lifecycle timeout unexpectedly succeeded" >&2
  exit 1
fi
elapsed="$(( $(date +%s) - started ))"
[ "$elapsed" -le 3 ]

if "$ROOT/scripts/run-xkeen-foreground.sh" -benchmark >/dev/null 2>&1; then
  echo "ERROR: unsupported lifecycle action was accepted" >&2
  exit 1
fi
