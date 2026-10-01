#!/bin/sh
# xkeen-control-hybrid v1; source-owned; fixed LAN-only TCP redirect + UDP TProxy
set -eu

ensure_jump() {
  family="$1"
  table="$2"
  shift 2
  if ! "$family" -t "$table" -C "$@" >/dev/null 2>&1; then
    "$family" -t "$table" -A "$@"
  fi
}

iptables -t nat -L XKEEN_CONTROL_HYBRID >/dev/null 2>&1 || iptables -t nat -N XKEEN_CONTROL_HYBRID
iptables -t mangle -L XKEEN_CONTROL_HYBRID >/dev/null 2>&1 || iptables -t mangle -N XKEEN_CONTROL_HYBRID
iptables -t nat -F XKEEN_CONTROL_HYBRID
iptables -t mangle -F XKEEN_CONTROL_HYBRID
iptables -t nat -A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p tcp -m comment --comment xkeen-control-hybrid -j REDIRECT --to-ports 61219
iptables -t mangle -A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p udp -m socket --transparent -m comment --comment xkeen-control-hybrid -j MARK --set-mark 0x111/0xfff
iptables -t mangle -A XKEEN_CONTROL_HYBRID -i br0 -m addrtype ! --dst-type LOCAL -p udp -m comment --comment xkeen-control-hybrid -j TPROXY --on-ip 0.0.0.0 --on-port 61219 --tproxy-mark 0x111/0xfff
ensure_jump iptables nat PREROUTING -i br0 -m addrtype ! --dst-type LOCAL -p tcp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID
ensure_jump iptables mangle PREROUTING -i br0 -m addrtype ! --dst-type LOCAL -p udp -m comment --comment xkeen-control-hybrid -j XKEEN_CONTROL_HYBRID

ip -4 link show dev br0 >/dev/null
if ! ip -4 rule show | grep -F "fwmark 0x111/0xfff lookup 111" >/dev/null 2>&1; then
  ip -4 rule add fwmark 0x111/0xfff table 111 pref 111
fi
if ! ip -4 route show table 111 | grep -F "local 0.0.0.0/0 dev lo" >/dev/null 2>&1; then
  ip -4 route add local 0.0.0.0/0 dev lo table 111
fi
