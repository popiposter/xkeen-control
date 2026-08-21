#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BIN_SRC="${1:-$ROOT/dist/xkeen-control-linux-arm64}"
BIN_DST="${CONTROL_BIN:-/opt/sbin/xkeen-control}"
INIT_DST="${CONTROL_INIT:-/opt/etc/init.d/S99xkeen-control}"

[ -f "$BIN_SRC" ] || { echo "ERROR: arm64 binary not found: $BIN_SRC" >&2; exit 1; }
mkdir -p "$(dirname -- "$BIN_DST")" "$(dirname -- "$INIT_DST")" /opt/etc/xkeen-control /opt/var/run
cp "$BIN_SRC" "$BIN_DST"
chmod 755 "$BIN_DST"
cp "$ROOT/packaging/S99xkeen-control" "$INIT_DST"
chmod 755 "$INIT_DST"
chmod 700 /opt/etc/xkeen-control

echo "Installed $BIN_DST"
echo "Installed $INIT_DST"
echo "Initialize authentication interactively with: $BIN_DST password init"
