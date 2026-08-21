#!/bin/sh
set -eu

CRON="${CRON:-/opt/var/spool/cron/crontabs/root}"
XKEEN_BIN="${XKEEN_BIN:-/opt/sbin/xkeen}"
RUNNER="${XKEEN_BENCHMARK_RUNNER:-/opt/etc/xkeen-control/run-bounded-speed-benchmark.sh}"

mkdir -p "$(dirname "$CRON")"
touch "$CRON"

grep -v -e "$XKEEN_BIN -sbt" -e "$RUNNER" "$CRON" > "$CRON.tmp" 2>/dev/null || true
mv "$CRON.tmp" "$CRON"
sed -i '/^$/d' "$CRON"

[ -x /opt/etc/init.d/S05crond ] && /opt/etc/init.d/S05crond restart >/dev/null 2>&1 || true
echo "Legacy XKeen speed benchmark schedule disabled; xkeen-control owns the 04:17 schedule"
