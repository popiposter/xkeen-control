#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp="$(mktemp -d /tmp/xkeen-appliance-test.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT

config="$tmp/xray"
xkeen="$tmp/xkeen/xkeen.json"
nodes="$tmp/secrets/nodes.json"
active="$config/04_outbounds.json"
appliance="$tmp/control/config/appliance.json"
candidate="$tmp/candidate"
fakebin="$tmp/bin"
mkdir -p "$config" "$(dirname "$xkeen")" "$(dirname "$nodes")" "$fakebin"

for name in 01_log.json 02_dns.json 03_inbounds.json 05_routing.json 06_policy.json 07_observatory.json 08_api.json; do
	cp "$ROOT/config/xray/$name" "$config/$name"
done
cp "$ROOT/config/xkeen/xkeen.json" "$xkeen"

source_key="sha256-$(printf '%s' 'node.example.com|443|reality|node.example.com|chrome|AAAAAAAAAAAAAAAA|0123456789abcdef||tcp||||' | sha256sum | awk '{print $1}')"
printf '%s\n' "{\"schemaVersion\":1,\"nodes\":[{\"id\":\"node-11111111\",\"outboundTag\":\"proxy-node-11111111\",\"name\":\"Fixture node\",\"enabled\":true,\"source\":{\"type\":\"manual\"},\"vless\":{\"uuid\":\"11111111-1111-1111-1111-111111111111\",\"host\":\"node.example.com\",\"port\":443,\"encryption\":\"none\",\"security\":\"reality\",\"serverName\":\"node.example.com\",\"fingerprint\":\"chrome\",\"publicKey\":\"AAAAAAAAAAAAAAAA\",\"shortId\":\"0123456789abcdef\",\"network\":\"tcp\"},\"sourceKey\":\"$source_key\"}],\"subscriptions\":[]}" > "$nodes"

go build -trimpath -buildvcs=false -o "$tmp/xkeen-control" ./cmd/xkeen-control

cat > "$fakebin/xray" <<'EOF_XRAY'
#!/bin/sh
set -eu
[ "${1:-}" = run ] && [ "${2:-}" = -test ] || exit 1
exit 0
EOF_XRAY
chmod 755 "$fakebin/xray"

export XKEEN_APPLIANCE_PATH="$appliance"
export XKEEN_XRAY_CONFIG_DIR="$config"
export XKEEN_CONFIG_PATH="$xkeen"
export XKEEN_NODES_PATH="$nodes"
export XKEEN_ACTIVE_OUTBOUNDS="$active"
export XKEEN_XRAY_BINARY="$fakebin/xray"

"$tmp/xkeen-control" nodes render --output "$active" >/dev/null
before="$(sha256sum "$config"/*.json "$xkeen" | sha256sum | awk '{print $1}')"
"$tmp/xkeen-control" appliance adopt >/dev/null
"$tmp/xkeen-control" appliance validate >/dev/null
"$tmp/xkeen-control" appliance verify >/dev/null
"$tmp/xkeen-control" appliance render --output "$candidate" >/dev/null

after="$(sha256sum "$config"/*.json "$xkeen" | sha256sum | awk '{print $1}')"
[ "$before" = "$after" ]
for name in 01_log.json 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json 06_policy.json 07_observatory.json 08_api.json; do
	[ -f "$candidate/xray/$name" ]
done
[ -f "$candidate/xkeen/xkeen.json" ]

printf '%s\n' '{"routing":{}}' > "$config/05_routing.json"
if "$tmp/xkeen-control" appliance verify >/dev/null 2>&1; then
	echo "appliance verify accepted active policy drift" >&2
	exit 1
fi

echo "appliance fixtures passed"
