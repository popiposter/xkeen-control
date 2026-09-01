#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp="$(mktemp -d /tmp/xkeen-appliance-test.XXXXXX)"
candidate_root="/tmp/xkeen-control"
candidate="$candidate_root/candidate-appliance-test.$$"
pre_candidate="$candidate_root/candidate-deploy-pre.$$"
post_candidate="$candidate_root/candidate-deploy-post.$$"
trap 'rm -rf "$tmp" "$candidate" "$pre_candidate" "$post_candidate"' EXIT
mkdir -p "$candidate_root"

normalize_policy() {
	jq -S 'if .routing.rules then .routing.rules |= map(if has("type") then . else . + {type: "field"} end) else . end'
}

config="$tmp/xray"
xkeen="$tmp/xkeen/xkeen.json"
nodes="$tmp/secrets/nodes.json"
active="$config/04_outbounds.json"
appliance="$tmp/control/config/appliance.json"
fakebin="$tmp/bin"
fake_repo="$tmp/repo"
mkdir -p "$config" "$(dirname "$xkeen")" "$(dirname "$nodes")" "$fakebin" \
  "$fake_repo/scripts" "$fake_repo/config/xray" "$fake_repo/config/xkeen"

for name in 01_log.json 02_dns.json 03_inbounds.json 05_routing.json 06_policy.json 07_observatory.json 08_api.json; do
	cp "$ROOT/config/xray/$name" "$config/$name"
	cp "$ROOT/config/xray/$name" "$fake_repo/config/xray/$name"
done
cp "$ROOT/config/xkeen/xkeen.json" "$xkeen"
cp "$ROOT/config/xkeen/xkeen.json" "$fake_repo/config/xkeen/xkeen.json"
cp "$ROOT/scripts/prepare-deploy-candidate.sh" "$fake_repo/scripts/prepare-deploy-candidate.sh"

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
"$tmp/xkeen-control" nodes render --output "$tmp/expected-outbounds.json" >/dev/null

# Before adoption, the deploy candidate must come from repository policy while
# 04_outbounds remains generated from nodes.json.
XRAY_DST="$config" \
XKEEN_DST="$(dirname "$xkeen")" \
XKEEN_SECRET_DIR="$(dirname "$nodes")" \
XKEEN_CONTROL_BIN="$tmp/xkeen-control" \
XKEEN_CONTROL_DIR="$tmp/control" \
XKEEN_APPLIANCE_PATH="$appliance" \
sh "$fake_repo/scripts/prepare-deploy-candidate.sh" "$pre_candidate"
for name in 02_dns.json 05_routing.json 07_observatory.json; do
	if [ "$name" = "05_routing.json" ]; then
		diff -u <(normalize_policy < "$fake_repo/config/xray/$name") <(normalize_policy < "$pre_candidate/xray/$name")
	else
		diff -u <(jq -S . "$fake_repo/config/xray/$name") <(jq -S . "$pre_candidate/xray/$name")
	fi
done
cmp "$tmp/expected-outbounds.json" "$pre_candidate/xray/04_outbounds.json"

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

# After adoption, deliberately diverge the repository routing fixture. The
# deploy candidate must still come from appliance.json, not from Git defaults.
jq '.routing.domainMatcher = "linear"' "$fake_repo/config/xray/05_routing.json" > "$tmp/repo-routing.json"
mv "$tmp/repo-routing.json" "$fake_repo/config/xray/05_routing.json"
XRAY_DST="$config" \
XKEEN_DST="$(dirname "$xkeen")" \
XKEEN_SECRET_DIR="$(dirname "$nodes")" \
XKEEN_CONTROL_BIN="$tmp/xkeen-control" \
XKEEN_CONTROL_DIR="$tmp/control" \
XKEEN_APPLIANCE_PATH="$appliance" \
sh "$fake_repo/scripts/prepare-deploy-candidate.sh" "$post_candidate"
for name in 02_dns.json 05_routing.json 07_observatory.json; do
	if [ "$name" = "05_routing.json" ]; then
		diff -u <(normalize_policy < "$config/$name") <(normalize_policy < "$post_candidate/xray/$name")
	else
		diff -u <(jq -S . "$config/$name") <(jq -S . "$post_candidate/xray/$name")
	fi
done
if diff -q <(jq -S . "$fake_repo/config/xray/05_routing.json") <(jq -S . "$post_candidate/xray/05_routing.json") >/dev/null; then
	echo "post-adoption deploy candidate followed repository routing instead of appliance authority" >&2
	exit 1
fi
cmp "$tmp/expected-outbounds.json" "$post_candidate/xray/04_outbounds.json"
grep -F 'prepare-deploy-candidate.sh' "$ROOT/scripts/deploy.sh" >/dev/null

printf '%s\n' '{"routing":{}}' > "$config/05_routing.json"
if "$tmp/xkeen-control" appliance verify >/dev/null 2>&1; then
	echo "appliance verify accepted active policy drift" >&2
	exit 1
fi

echo "appliance fixtures passed"
