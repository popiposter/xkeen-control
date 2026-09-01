#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
CANDIDATE="${1:-}"
XRAY_DST="${XRAY_DST:-/opt/etc/xray/configs}"
XKEEN_DST="${XKEEN_DST:-/opt/etc/xkeen}"
SECRET_DIR="${XKEEN_SECRET_DIR:-/opt/etc/xkeen-control/secrets}"
NODES_REGISTRY="$SECRET_DIR/nodes.json"
SECRET_OUTBOUNDS="$SECRET_DIR/04_outbounds.json"
CONTROL_BIN="${XKEEN_CONTROL_BIN:-/opt/sbin/xkeen-control}"
CONTROL_DIR="${XKEEN_CONTROL_DIR:-/opt/etc/xkeen-control}"
APPLIANCE_PATH="${XKEEN_APPLIANCE_PATH:-$CONTROL_DIR/config/appliance.json}"

case "$CANDIDATE" in
  /tmp/xkeen-control/candidate-*) ;;
  *)
    echo "ERROR: deploy candidate must be a fresh /tmp/xkeen-control/candidate-* path" >&2
    exit 1
    ;;
esac

[ ! -e "$CANDIDATE" ] && [ ! -L "$CANDIDATE" ] || {
  echo "ERROR: deploy candidate path already exists" >&2
  exit 1
}

[ -f "$NODES_REGISTRY" ] || [ -f "$SECRET_OUTBOUNDS" ] || {
  echo "ERROR: local nodes.json not found: $NODES_REGISTRY" >&2
  echo "Existing router: run scripts/migrate-secrets.sh before deploy." >&2
  echo "Fresh router: restore nodes.json separately from Git/backup." >&2
  exit 1
}

if [ -f "$NODES_REGISTRY" ]; then
  [ -x "$CONTROL_BIN" ] || { echo "ERROR: xkeen-control is required to render nodes.json" >&2; exit 1; }
  chmod 600 "$NODES_REGISTRY" 2>/dev/null || true
  "$CONTROL_BIN" nodes validate >/dev/null
else
  command -v jq >/dev/null 2>&1 || { echo "ERROR: jq not found" >&2; exit 1; }
  echo "WARNING: using legacy 04_outbounds.json fallback; migrate before retiring this file." >&2
  jq -e . "$SECRET_OUTBOUNDS" >/dev/null || {
    echo "ERROR: local legacy outbound file is not valid JSON" >&2
    exit 1
  }
  chmod 600 "$SECRET_OUTBOUNDS" 2>/dev/null || true
fi

if [ -e "$APPLIANCE_PATH" ] || [ -L "$APPLIANCE_PATH" ]; then
  [ -f "$NODES_REGISTRY" ] || {
    echo "ERROR: appliance authority requires authoritative nodes.json" >&2
    exit 1
  }
  [ -x "$CONTROL_BIN" ] || { echo "ERROR: xkeen-control is required to render appliance.json" >&2; exit 1; }
  XKEEN_APPLIANCE_PATH="$APPLIANCE_PATH" \
  XKEEN_NODES_PATH="$NODES_REGISTRY" \
  XKEEN_ACTIVE_OUTBOUNDS="$XRAY_DST/04_outbounds.json" \
  XKEEN_XRAY_CONFIG_DIR="$XRAY_DST" \
  XKEEN_CONFIG_PATH="$XKEEN_DST/xkeen.json" \
  "$CONTROL_BIN" appliance validate >/dev/null
  XKEEN_APPLIANCE_PATH="$APPLIANCE_PATH" \
  XKEEN_NODES_PATH="$NODES_REGISTRY" \
  XKEEN_ACTIVE_OUTBOUNDS="$XRAY_DST/04_outbounds.json" \
  XKEEN_XRAY_CONFIG_DIR="$XRAY_DST" \
  XKEEN_CONFIG_PATH="$XKEEN_DST/xkeen.json" \
  "$CONTROL_BIN" appliance render --output "$CANDIDATE" >/dev/null
else
  mkdir -p "$CANDIDATE/xray" "$CANDIDATE/xkeen"
  chmod 700 "$CANDIDATE" "$CANDIDATE/xray" "$CANDIDATE/xkeen" 2>/dev/null || true
  for f in "$ROOT"/config/xray/*.json; do
    [ -f "$f" ] || continue
    [ "$(basename "$f")" = "04_outbounds.json" ] && {
      echo "ERROR: tracked config/xray/04_outbounds.json must not exist in a secretless checkout" >&2
      exit 1
    }
    cp "$f" "$CANDIDATE/xray/$(basename "$f")"
  done
  if [ -f "$NODES_REGISTRY" ]; then
    "$CONTROL_BIN" nodes render --output "$CANDIDATE/xray/04_outbounds.json"
  else
    cp "$SECRET_OUTBOUNDS" "$CANDIDATE/xray/04_outbounds.json"
  fi
  cp "$ROOT/config/xkeen/xkeen.json" "$CANDIDATE/xkeen/xkeen.json"
fi

chmod 600 "$CANDIDATE/xray/04_outbounds.json" 2>/dev/null || true
