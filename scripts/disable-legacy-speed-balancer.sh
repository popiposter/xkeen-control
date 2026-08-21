#!/bin/sh
set -eu

CONFIG="${XKEEN_CONFIG:-/opt/etc/xkeen/xkeen.json}"
[ -f "$CONFIG" ] || { echo "ERROR: XKeen config not found: $CONFIG" >&2; exit 1; }

jq -e '.xkeen.xray.speed_balancer | type == "object"' "$CONFIG" >/dev/null
DIR="$(dirname -- "$CONFIG")"
TMP="$(mktemp "$DIR/.xkeen-config.XXXXXX")"
trap 'rm -f "$TMP"' EXIT
jq '.xkeen.xray.speed_balancer.enabled = false' "$CONFIG" > "$TMP"
chmod 600 "$TMP"
mv -f "$TMP" "$CONFIG"
trap - EXIT
echo "Legacy XKeen Speed Balancer disabled: $CONFIG"
