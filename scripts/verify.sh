#!/bin/sh
set -u

fail=0
check() { printf '%-36s' "$1"; shift; if "$@" >/dev/null 2>&1; then echo OK; else echo FAIL; fail=1; fi; }
CRON="${CRON:-/opt/var/spool/cron/crontabs/root}"
XKEEN_CONFIG="${XKEEN_CONFIG:-/opt/etc/xkeen/xkeen.json}"
CONTROL_BIN="${CONTROL_BIN:-/opt/sbin/xkeen-control}"
CONTROL_INIT="${CONTROL_INIT:-/opt/etc/init.d/S99xkeen-control}"

legacy_cron_absent() {
  ! grep -E -q 'xkeen[^[:space:]]*[[:space:]]+-sbt|run-bounded-speed-benchmark\.sh|speed_failover_watchdog\.sh' "$CRON" 2>/dev/null
}

legacy_speed_balancer_disabled() {
  jq -e '.xkeen.xray.speed_balancer.enabled == false' "$XKEEN_CONFIG" >/dev/null
}

state_dir_safe() {
  [ ! -e /opt/etc/xkeen-control/state ] || [ -d /opt/etc/xkeen-control/state ]
  [ ! -e /opt/etc/xkeen-control/runtime/benchmark-outbounds.json ]
}

check "Xray process" pidof xray
check "Xray config" xkeen -xtest
check "RoutingService 10085" xray api lsrules -s 127.0.0.1:10085
printf '%-36s' "HTTP probe 10808"
if netstat -lnt 2>/dev/null | grep -q '127.0.0.1:10808' || ss -lnt 2>/dev/null | grep -q '127.0.0.1:10808'; then echo OK; else echo FAIL; fail=1; fi
printf '%-36s' "Unified proxy balancer"
if xray api bi -s 127.0.0.1:10085 bal-proxy >/tmp/xkeen-proxy.$$ 2>/dev/null; then echo OK; cat /tmp/xkeen-proxy.$$; else echo FAIL; fail=1; fi
rm -f /tmp/xkeen-proxy.$$
check "xkeen-control binary" test -x "$CONTROL_BIN"
check "xkeen-control init" test -x "$CONTROL_INIT"
check "Legacy benchmark/watchdog absent" legacy_cron_absent
check "Legacy Speed Balancer disabled" legacy_speed_balancer_disabled
check "C.1 state boundary" state_dir_safe
echo
echo "Routing order:"
jq -r '.routing.rules[].ruleTag' /opt/etc/xray/configs/05_routing.json 2>/dev/null || fail=1
echo
echo "DNS policy:"
jq -r '.dns.servers[] | if type=="object" then [.tag,.address,((.domains|length)|tostring)]|@tsv else ["fallback",.,"-"]|@tsv end' /opt/etc/xray/configs/02_dns.json 2>/dev/null || fail=1
exit "$fail"
