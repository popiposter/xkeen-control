// Real extracted pinned native helpers; synthetic kernel/API commands only.
import assert from 'node:assert/strict'
import { readFileSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const source = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) }).init.toString()
function extract(start, end) {
  const a = source.indexOf(start), b = source.indexOf(end, a + start.length)
  assert.ok(a > 0 && b > a)
  return source.slice(a, b)
}
const deny = extract('    _xkeen_sync_deny_mac_ipset() {\n', '\n    command -v ipset')
const route = extract('    configure_route() {\n', '\n    # Добавление')
const ensure = extract('    _xkeen_ensure_ipsets() {\n', '\n    _xkeen_cache_valid()')
const refill = extract('    _xkeen_refill_geo_if_empty() {\n', '\n    # Текущий WAN')
function run(code) {
  const dir = mkdtempSync(join(tmpdir(), 'native-errors-'))
  try { return spawnSync('/bin/sh', ['-c', `_xkeen_rundir='${dir}'; ${code}`], { encoding: 'utf8', timeout: 3000 }) }
  finally { rmSync(dir, { recursive: true, force: true }) }
}
const denySetup = `name_ipset_deny_mac=xkeen_deny_mac
url_server=local; url_hotspot=hotspot
logger() { :; }
curl_api() { printf '{"host":[]}'; }
ipset() { echo "$*"; [ "$1:$2" != "$FAIL" ]; }
`
for (const fail of ['create:xkeen_deny_mac', 'create:xkeen_deny_mac_tmp', 'flush:xkeen_deny_mac_tmp', 'restore:-exist', 'swap:xkeen_deny_mac_tmp', 'destroy:xkeen_deny_mac_tmp']) {
  test(`deny MAC failure ${fail} never returns successful synchronization`, () => {
    const r = run(`${deny}\n${denySetup}\nFAIL='${fail}'; _xkeen_sync_deny_mac_ipset`)
    assert.equal(r.status, 1, r.stderr)
  })
}
test('deny MAC failed curl or malformed JSON cannot swap the existing live set', () => {
  for (const api of ['return 7', "printf 'not-json'", "printf '[]'; return 7"]) {
    const r = run(`${deny}\n${denySetup}\ncurl_api() { ${api}; }; _xkeen_sync_deny_mac_ipset`)
    assert.equal(r.status, 1, r.stderr)
    assert.ok(!r.stdout.includes('swap '), r.stdout)
  }
})
test('a valid empty deny list is a successful intentional atomic replacement', () => {
  const r = run(`${deny}\n${denySetup}\n_xkeen_sync_deny_mac_ipset`)
  assert.equal(r.status, 0, r.stderr)
  assert.ok(r.stdout.includes('swap xkeen_deny_mac_tmp xkeen_deny_mac'))
})

test('wrong-shaped or multiple hotspot documents cannot clear the live deny list', () => {
  for (const payload of ['null', '{"error":"unavailable"}', '{"host":null}', '{"host":{"error":"unavailable"}}', '{}', '[]\n[]', '{"host":[{"mac":"AA:BB:CC:DD:EE:FF","access":false}]}']) {
    const r = run(`${deny}\n${denySetup}\ncurl_api() { printf '%s' '${payload}'; }; _xkeen_sync_deny_mac_ipset`)
    assert.notEqual(r.status, 0, payload)
    assert.ok(!r.stdout.includes('swap '), payload)
  }
  for (const payload of ['{"host":[]}', '{"host":{"mac":"AA:BB:CC:DD:EE:FF","access":"allow"}}']) {
    const r = run(`${deny}\n${denySetup}\ncurl_api() { printf '%s' '${payload}'; }; _xkeen_sync_deny_mac_ipset`)
    assert.equal(r.status, 0, r.stderr)
    assert.ok(r.stdout.includes('swap '))
  }
})
test('denied MAC survives parsing, malformed denied MAC refuses instead of clearing it', () => {
  const body = `curl_api() { printf '%s' '{"host":[{"access":"deny","mac":"aa:bb:cc:dd:ee:ff"}]}'; }
ipset() { if [ "$1" = restore ]; then cat; else echo "$*"; fi; }
_xkeen_sync_deny_mac_ipset`
  const valid = run(`${deny}\n${denySetup}\n${body}`)
  assert.equal(valid.status, 0, valid.stderr)
  assert.ok(valid.stdout.includes('add xkeen_deny_mac_tmp AA:BB:CC:DD:EE:FF -exist'), valid.stdout)
  const invalid = run(`${deny}\n${denySetup}\n${body.replace('aa:bb:cc:dd:ee:ff', 'malformed')}`)
  assert.equal(invalid.status, 1)
  assert.ok(!invalid.stdout.includes('swap '))
})
test('oversized API reply refuses before shell capture, restore or swap', () => {
  const r = run(`${deny}\n${denySetup}\ncurl_api() { head -c 131072 /dev/zero; }; _xkeen_sync_deny_mac_ipset`)
  assert.notEqual(r.status, 0)
  assert.ok(!r.stdout.includes('swap '))
})
const routeSetup = `policy_mark=; table_id=111; table_mark=0x111
sleep() { :; }
ip() {
  case "$*" in
    '-4 route show default') echo 'default via 192.0.2.1';;
    '-4 route show table main') [ "$FAIL" != source ] || return 1; echo '192.0.2.0/24 dev eth0';;
    '-4 route show table 111') [ "$FAIL" != read ] || return 1; [ "$CURRENT" != populated ] || echo 'local default dev lo';;
    '-4 rule show') [ "$FAIL" != rules ] || return 1; [ "$RULE" != present ] || echo '100: from all fwmark 0x111 lookup 111';;
    *) echo "$*"; [ "$*" != "$FAIL" ] || return 1;;
  esac
  return 0
}
`
for (const fail of ['source', 'read', 'rules', '-4 rule del fwmark 0x111 lookup 111', '-4 route flush table 111', '-4 route add local default dev lo table 111', '-4 rule add fwmark 0x111 lookup 111', '-4 route add table 111 192.0.2.0/24 dev eth0']) {
  test(`native route failure ${fail} reaches caller`, () => {
    const r = run(`${route}\n${routeSetup}\nCURRENT=populated; RULE=present; FAIL='${fail}'; configure_route 4`)
    assert.equal(r.status, 1, r.stderr)
  })
}
test('absent native rule is an idempotent state, not a failed deletion', () => {
  const r = run(`${route}\n${routeSetup}\nFAIL='-4 rule del fwmark 0x111 lookup 111'; configure_route 4`)
  assert.equal(r.status, 0, r.stderr)
  assert.ok(!r.stdout.includes('rule del'), r.stdout)
})

test('failed native policy-table discovery cannot fall back to main or write routes', () => {
  const r = run(`${route}\n${routeSetup}\npolicy_mark=policy123
ip() { if [ "$*" = 'rule show' ]; then return 7; fi; echo WRITE; }
configure_route 4`)
  assert.notEqual(r.status, 0)
  assert.equal(r.stdout, '')
})

test('IPv6 default-route read failure cannot be accepted as native absent default', () => {
  const r = run(`${route}\n${routeSetup}
ip() { if [ "$*" = '-6 route show default' ]; then return 7; fi; echo WRITE; }
configure_route 6`)
  assert.notEqual(r.status, 0)
  assert.equal(r.stdout, '')
})
test('enabled family ipset creation propagates first failed write; disabled family is skipped', () => {
  const fail = run(`${ensure}\nipset() { [ "$2" != ext_exclude ]; }; iptables_supported=true; ip6tables_supported=false; _xkeen_ensure_ipsets`)
  assert.equal(fail.status, 1)
  const ok = run(`${ensure}\nipset() { echo "$*"; }; iptables_supported=true; ip6tables_supported=false; _xkeen_ensure_ipsets`)
  assert.equal(ok.status, 0)
  assert.ok(!ok.stdout.includes('inet6'))
})
for (const fail of ['save:geo_exclude', 'flush:geo_exclude_renew_tmp', 'restore:-exist', 'swap:geo_exclude']) {
  test(`geo refill ${fail} cannot be masked by temporary-set destruction`, () => {
    const r = run(`${refill}\nprintf '192.0.2.0/24\\n' > "$_xkeen_rundir/list"
logger() { :; }; ipset() { [ "$1:$2" != '${fail}' ]; }
_xkeen_refill_geo_if_empty geo_exclude "$_xkeen_rundir/list" inet`)
    assert.equal(r.status, 1, r.stderr)
  })
}
test('geo refill failed list producer cannot swap an empty temporary set', () => {
  const r = run(`${refill}\nprintf '192.0.2.0/24\\n' > "$_xkeen_rundir/list"
logger() { :; }; sed() { return 9; }; ipset() { echo "$*"; }
_xkeen_refill_geo_if_empty geo_exclude "$_xkeen_rundir/list" inet`)
  assert.equal(r.status, 1)
  assert.ok(!r.stdout.includes('swap '), r.stdout)
})

test('geo membership scan failure cannot masquerade as an empty set', () => {
  const r = run(`${refill}\nprintf '192.0.2.0/24\\n' > "$_xkeen_rundir/list"
grep() { return 2; }; ipset() { echo "$*"; }
_xkeen_refill_geo_if_empty geo_exclude "$_xkeen_rundir/list" inet`)
  assert.notEqual(r.status, 0)
  assert.ok(!r.stdout.includes('swap '))
})
test('hook terminal paths propagate failed deny sync before publishing WAN or cache', () => {
  const paths = [
    extract('    if [ -n "$_xkeen_cur_wan" ]', '\n    if _xkeen_rules_intact; then'),
    extract('    if _xkeen_rules_intact; then', '\n    # Кэш готовых'),
    extract('    if _xkeen_cache_valid; then', '\n    if [ -n "$port_donor" ]'),
    extract('\n    _xkeen_apply || exit 1\n', '\nelse\n    # mkdir-lock'),
  ]
  for (const path of paths) {
    const r = run(`iptables_supported=false; ip6tables_supported=false
_xkeen_cur_wan=192.0.2.1; _xkeen_prev_wan=192.0.2.1; _xkeen_wan_state="$_xkeen_rundir/wan"
_xkeen_rules_intact() { return 0; }; _xkeen_cache_valid() { return 0; }
_xkeen_ensure_ipsets() { :; }; _xkeen_cache_load() { :; }; _xkeen_apply() { :; }
_xkeen_release_nf_lock() { :; }; _xkeen_flush_udp_conntrack() { :; }
_xkeen_sync_deny_mac_ipset() { return 1; }; _xkeen_cache_save() { echo CACHE; }
trap '[ ! -e "$_xkeen_wan_state" ] || echo WAN' EXIT
${path}`)
    assert.equal(r.status, 1, r.stderr)
    assert.equal(r.stdout, '')
  }
})
