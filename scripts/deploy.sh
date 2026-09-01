#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
XRAY_DST="${XRAY_DST:-/opt/etc/xray/configs}"
XKEEN_DST="${XKEEN_DST:-/opt/etc/xkeen}"
XRAY_ASSET_DIR="${XRAY_LOCATION_ASSET:-/opt/etc/xray/dat}"
SECRET_DIR="${XKEEN_SECRET_DIR:-/opt/etc/xkeen-control/secrets}"
NODES_REGISTRY="$SECRET_DIR/nodes.json"
SECRET_OUTBOUNDS="$SECRET_DIR/04_outbounds.json"
CONTROL_BIN="${XKEEN_CONTROL_BIN:-/opt/sbin/xkeen-control}"
CONTROL_DIR="${XKEEN_CONTROL_DIR:-/opt/etc/xkeen-control}"
APPLIANCE_PATH="${XKEEN_APPLIANCE_PATH:-$CONTROL_DIR/config/appliance.json}"
XKEEN_LIFECYCLE="$ROOT/scripts/run-xkeen-foreground.sh"
STAMP="$(date +%Y%m%d-%H%M%S)"
BACKUP="/opt/backups/xkeen-repo-$STAMP"
CANDIDATE="/tmp/xkeen-candidate.$$"
XRAY_STAGE="${XRAY_DST}.new.$$"
XRAY_PREV="${XRAY_DST}.prev.$$"
XKEEN_STAGE="${XKEEN_DST}/xkeen.json.new.$$"
ACTIVATED=0

restart_xray() {
  if pidof xray >/dev/null 2>&1; then
    "$XKEEN_LIFECYCLE" -restart
  else
    "$XKEEN_LIFECYCLE" -start
  fi
}

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: $1 not found" >&2
    exit 1
  }
}

rollback() {
  echo "Activation failed; rolling back previous generation..." >&2
  if [ -d "$XRAY_PREV" ]; then
    rm -rf "$XRAY_DST" 2>/dev/null || true
    mv "$XRAY_PREV" "$XRAY_DST"
  elif [ -d "$BACKUP/xray" ]; then
    mkdir -p "$XRAY_DST"
    rm -f "$XRAY_DST"/*.json 2>/dev/null || true
    cp "$BACKUP/xray"/*.json "$XRAY_DST/" 2>/dev/null || true
  fi
  if [ -f "$BACKUP/xkeen/xkeen.json" ]; then
    cp "$BACKUP/xkeen/xkeen.json" "$XKEEN_DST/xkeen.json"
  fi
  restart_xray >/dev/null 2>&1 || echo "WARNING: bounded rollback restart failed" >&2
}

cleanup() {
  rc=$?
  trap - EXIT
  if [ "$ACTIVATED" -eq 1 ]; then
    rollback
    ACTIVATED=0
  fi
  rm -rf "$CANDIDATE" "$XRAY_STAGE" 2>/dev/null || true
  rm -f "$XKEEN_STAGE" 2>/dev/null || true
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

wait_api() {
  i=0
  while ! xray api lsrules -s 127.0.0.1:10085 >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -ge 30 ] && return 1
    sleep 1
  done
}

need xkeen
need xray
need jq
need curl
[ -x "$XKEEN_LIFECYCLE" ] || { echo "ERROR: foreground XKeen lifecycle wrapper is unavailable" >&2; exit 1; }

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
  echo "WARNING: using legacy 04_outbounds.json fallback; migrate before retiring this file." >&2
  jq -e . "$SECRET_OUTBOUNDS" >/dev/null || {
    echo "ERROR: local legacy outbound file is not valid JSON" >&2
    exit 1
  }
  chmod 600 "$SECRET_OUTBOUNDS" 2>/dev/null || true
fi

rm -rf "$CANDIDATE" "$XRAY_STAGE" "$XRAY_PREV"
mkdir -p "$CANDIDATE/xray" "$CANDIDATE/xkeen" "$XRAY_STAGE" "$XKEEN_DST" "$BACKUP/xray" "$BACKUP/xkeen"

if [ -e "$APPLIANCE_PATH" ] || [ -L "$APPLIANCE_PATH" ]; then
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
chmod 600 "$CANDIDATE/xray/04_outbounds.json"

for f in "$CANDIDATE"/xray/*.json "$CANDIDATE"/xkeen/*.json; do
  jq -e . "$f" >/dev/null || {
    echo "ERROR: invalid JSON in candidate: $(basename "$f")" >&2
    exit 1
  }
done

"$ROOT/scripts/update-geodata.sh"

echo "Validating candidate Xray config..."
if ! XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" xray run -test -confdir "$CANDIDATE/xray" >/dev/null 2>&1; then
  echo "ERROR: candidate Xray validation failed; active generation was not changed" >&2
  exit 1
fi

mkdir -p "$XRAY_DST" "$XKEEN_DST"
cp "$XRAY_DST"/*.json "$BACKUP/xray/" 2>/dev/null || true
cp "$XKEEN_DST/xkeen.json" "$BACKUP/xkeen/" 2>/dev/null || true
chmod 700 "$BACKUP" "$BACKUP/xray" "$BACKUP/xkeen" 2>/dev/null || true
chmod 600 "$BACKUP/xray/04_outbounds.json" 2>/dev/null || true
echo "Backup: $BACKUP"

cp "$CANDIDATE/xray"/*.json "$XRAY_STAGE/"
chmod 600 "$XRAY_STAGE/04_outbounds.json" 2>/dev/null || true
cp "$CANDIDATE/xkeen/xkeen.json" "$XKEEN_STAGE"

mv "$XRAY_DST" "$XRAY_PREV"
ACTIVATED=1
mv "$XRAY_STAGE" "$XRAY_DST"
mv -f "$XKEEN_STAGE" "$XKEEN_DST/xkeen.json"

xkeen -dns on || true
xkeen -ipv6 off || true
restart_xray

echo "Waiting for Xray RoutingService..."
if ! wait_api; then
  rollback
  ACTIVATED=0
  echo "ERROR: Xray API not ready after 30s; previous generation restored" >&2
  exit 1
fi

# C.1 migration: remove the interim XKeen benchmark/watchdog writers. The
# running xkeen-control process owns the 04:17 schedule and liveness loop.
mkdir -p "$CONTROL_DIR"
XKEEN_CONFIG="$XKEEN_DST/xkeen.json" "$ROOT/scripts/disable-legacy-speed-balancer.sh"
"$ROOT/scripts/install-performance-schedule.sh"
"$ROOT/scripts/install-watchdog.sh"
rm -f "$CONTROL_DIR/run-bounded-speed-benchmark.sh" "$CONTROL_DIR/runtime/benchmark-outbounds.json"
rmdir "$CONTROL_DIR/runtime" 2>/dev/null || true

rm -rf "$XRAY_PREV"
ACTIVATED=0

echo
echo "Deployment complete."
echo "Backup: $BACKUP"
