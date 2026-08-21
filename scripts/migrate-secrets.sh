#!/bin/sh
set -eu

SRC="${XKEEN_ACTIVE_OUTBOUNDS:-/opt/etc/xray/configs/04_outbounds.json}"
SECRET_DIR="${XKEEN_SECRET_DIR:-/opt/etc/xkeen-control/secrets}"
DST="$SECRET_DIR/04_outbounds.json"
NODES="$SECRET_DIR/nodes.json"
CONTROL_BIN="${XKEEN_CONTROL_BIN:-/opt/sbin/xkeen-control}"
FORCE=0
[ "${1:-}" = "--force" ] && FORCE=1

command -v jq >/dev/null 2>&1 || { echo "ERROR: jq not found" >&2; exit 1; }
[ -f "$SRC" ] || { echo "ERROR: active outbound file not found: $SRC" >&2; exit 1; }
jq -e . "$SRC" >/dev/null || { echo "ERROR: active outbound file is not valid JSON" >&2; exit 1; }

if [ -e "$NODES" ]; then
  echo "ERROR: nodes.json already exists; refusing legacy migration." >&2
  exit 1
fi

if [ -e "$DST" ] && [ "$FORCE" -ne 1 ]; then
  echo "ERROR: $DST already exists; refusing to overwrite. Use --force only after verifying the source." >&2
  exit 1
fi

umask 077
mkdir -p "$SECRET_DIR"
TMP="$DST.tmp.$$"
trap 'rm -f "$TMP"' EXIT HUP INT TERM
cp "$SRC" "$TMP"
chmod 600 "$TMP"
mv -f "$TMP" "$DST"
trap - EXIT HUP INT TERM

# Validate the copied file without printing its contents.
jq -e . "$DST" >/dev/null
printf 'Local secret outbound store created: %s\n' "$DST"
printf 'Mode forced to 0600. Back this file up separately from Git.\n'

if [ -x "$CONTROL_BIN" ]; then
  "$CONTROL_BIN" nodes migrate-legacy
  printf 'Versioned nodes.json migration applied through the transactional control-plane path.\n'
else
  printf 'Install xkeen-control, then run: xkeen-control nodes migrate-legacy\n'
fi
