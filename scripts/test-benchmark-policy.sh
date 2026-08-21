#!/usr/bin/env bash
set -euo pipefail

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

jq -e '.xkeen.xray.speed_balancer.enabled == false and
  .xkeen.xray.speed_balancer.max_nodes == 128 and
  .xkeen.xray.speed_balancer.max_time == 10' "$ROOT/config/xkeen/xkeen.json" >/dev/null

printf '%s\n' '{"xkeen":{"xray":{"speed_balancer":{"enabled":true}}}}' > "$TMP/xkeen.json"
XKEEN_CONFIG="$TMP/xkeen.json" "$ROOT/scripts/disable-legacy-speed-balancer.sh" >/dev/null
jq -e '.xkeen.xray.speed_balancer.enabled == false' "$TMP/xkeen.json" >/dev/null

if "$ROOT/scripts/run-bounded-speed-benchmark.sh" >/dev/null 2>&1; then
  echo "legacy benchmark guard unexpectedly ran" >&2
  exit 1
fi

printf '%s\n' '17 4 * * * /opt/etc/xkeen-control/run-bounded-speed-benchmark.sh' \
  '*/5 * * * * /opt/etc/xkeen/speed_failover_watchdog.sh' \
  '5 * * * * /opt/sbin/xkeen -sbt' > "$TMP/root"
CRON="$TMP/root" XKEEN_BIN=/opt/sbin/xkeen XKEEN_BENCHMARK_RUNNER=/opt/etc/xkeen-control/run-bounded-speed-benchmark.sh \
  "$ROOT/scripts/install-performance-schedule.sh" >/dev/null
CRON="$TMP/root" WATCHDOG_PATH="$TMP/speed_failover_watchdog.sh" "$ROOT/scripts/install-watchdog.sh" >/dev/null
if grep -E -q 'xkeen[^[:space:]]*[[:space:]]+-sbt|run-bounded-speed-benchmark\.sh|speed_failover_watchdog\.sh' "$TMP/root"; then
  echo "legacy benchmark/watchdog cron survived migration" >&2
  exit 1
fi
