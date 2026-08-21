#!/bin/sh
set -eu
CRON="${CRON:-/opt/var/spool/cron/crontabs/root}"
WATCHDOG_PATH="${WATCHDOG_PATH:-/opt/etc/xkeen/speed_failover_watchdog.sh}"
mkdir -p "$(dirname "$CRON")"
touch "$CRON"
grep -v -e 'speed_failover_watchdog.sh' -e 'xkeen-control-watchdog' "$CRON" > "$CRON.tmp" 2>/dev/null || true
mv "$CRON.tmp" "$CRON"
[ -x /opt/etc/init.d/S05crond ] && /opt/etc/init.d/S05crond restart || true
rm -f "$WATCHDOG_PATH"
echo "Legacy XKeen watchdog disabled; xkeen-control owns liveness"
