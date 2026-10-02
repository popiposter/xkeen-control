import assert from 'node:assert/strict'
import { readFileSync, writeFileSync, mkdtempSync, rmSync, chmodSync, mkdirSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

// Realistic synthetic iptables-save views, using the pinned Hybrid/force rules.
function rules(family, table) {
  const ip = family === 4 ? '127.0.0.1' : '::1'
  const proto = table === 'nat' ? 'tcp' : 'udp'
  const target = (port) => table === 'nat' ? `REDIRECT --to-ports ${port}` : `TPROXY --on-port ${port} --on-ip ${ip} --tproxy-mark 0x111/0xffffffff`
  const ports = table === 'nat' ? [12345, 12347] : [12346, 12348]
  return `*${table}
:PREROUTING ACCEPT [0:0]
:OUTPUT ACCEPT [0:0]
:xkeen - [0:0]
:xkeen_force - [0:0]
-A PREROUTING -m set --match-set xkeen_deny_mac src -m comment --comment xkeen_rule -j RETURN
-A PREROUTING -p ${proto} -m conntrack ! --ctstate INVALID -m dscp --dscp 0x3d -m comment --comment xkeen_rule -j xkeen_force
-A PREROUTING -p ${proto} -m conntrack ! --ctstate INVALID -m comment --comment xkeen_rule -j xkeen
${table === 'nat' ? `-A xkeen -p ${proto} -m comment --comment xkeen_rule -j ${target(ports[0])}
-A xkeen_force -p ${proto} -m comment --comment xkeen_rule -j ${target(ports[1])}` : ''}
${table === 'mangle' ? ['xkeen', 'xkeen_force'].map(c => `-A ${c}${c === 'xkeen' ? ' -p udp' : ''} -m conntrack --ctstate RELATED,ESTABLISHED -m comment --comment xkeen_rule -j CONNMARK --restore-mark --nfmask 0xffffffff --ctmask 0xffffffff
-A ${c} -p udp -m socket --transparent -m comment --comment xkeen_rule -j MARK --set-xmark 0x111/0xffffffff
-A ${c} -p udp -m mark ! --mark 0x0 -m comment --comment xkeen_rule -j CONNMARK --save-mark --nfmask 0xffffffff --ctmask 0xffffffff
-A ${c} -p udp -m comment --comment xkeen_rule -j ${target(ports[c === 'xkeen' ? 0 : 1])}`).join('\n') : ''}
COMMIT
`
}
function makeViews() {
    const views = {}
    for (const family of [4, 6]) for (const table of ['nat', 'mangle']) views[`${family}-${table}`] = rules(family, table)
    views.ipsets = 'create xkeen_deny_mac hash:mac\n' + [4, 6].flatMap(f => ['ext_exclude', 'user_exclude', 'geo_exclude', 'geo_override'].map(n => `create ${n}${f === 6 ? '6' : ''} ${n === 'ext_exclude' ? 'hash:ip' : 'hash:net'} family ${f === 6 ? 'inet6' : 'inet'}`)).join('\n') + '\n'
    for (const f of [4, 6]) {
      views[`${f}-rules`] = '0: from all lookup local\n100: from all fwmark 0x111 lookup 111\n32766: from all lookup main\n'
      views[`${f}-source`] = f === 4 ? '192.0.2.0/24 dev br0 scope link\n' : '2001:db8::/64 dev br0 metric 256\n'
      views[`${f}-routes`] = (f === 4 ? 'local default dev lo scope host\n' : 'local default dev lo metric 1024 pref medium\n') + views[`${f}-source`]
    }
    return views
}
function fixture(mutate = () => {}) {
  const dir = mkdtempSync(join(tmpdir(), 'native-hook-proof-'))
  try {
    const views = makeViews()
    mutate(views)
    for (const [key, value] of Object.entries(views)) writeFileSync(join(dir, key), value)
    const functions = readFileSync('scripts/native-admission-hook-verify.sh', 'utf8').split('\n_hv_main "$@"')[0]
    const r = spawnSync('/bin/sh', ['-c', `${functions}
_hv_chain=xkeen; _hv_tag=xkeen_rule; _hv_mark=0x111; _hv_table=111
_hv_deny=xkeen_deny_mac; _hv_redirect=12345; _hv_tproxy=12346
_hv_force_redirect=12347; _hv_force_tproxy=12348; _hv_force_dscp=61
_hv_ip4=127.0.0.1; _hv_ip6=::1; _hv_v4=true; _hv_v6=true
_hv_router=off; _hv_full=; _hv_force_redirect_net=tcp; _hv_force_tproxy_net=udp
_hv_policy=
_hv_read_table() { cat '${dir}/'"$1-$2"; }
_hv_read_ipsets() { cat '${dir}/ipsets'; }
_hv_read_rules() { cat '${dir}/'"$1-rules"; }
_hv_read_routes() { if [ "$2" = 111 ]; then cat '${dir}/'"$1-routes"; else cat '${dir}/'"$1-source"; fi; }
_hv_running
`], { encoding: 'utf8' })
    return r
  } finally { rmSync(dir, { recursive: true, force: true }) }
}
test('forced Hybrid BOTH families required kernel anchors pass', () => assert.equal(fixture().status, 0))
for (const [name, from, to] of [
  ['redirect port', '--to-ports 12345', '--to-ports 9999'],
  ['forced redirect port', '--to-ports 12347', '--to-ports 9999'],
  ['TPROXY address', '--on-ip ::1', '--on-ip ::2'],
  ['TPROXY mark', '--tproxy-mark 0x111/0xffffffff', '--tproxy-mark 0x112/0xffffffff'],
  ['forced TPROXY port', '--on-port 12348', '--on-port 9999'],
  ['deny MAC set', '--match-set xkeen_deny_mac src', '--match-set wrong_set src'],
  ['force DSCP selector', '--dscp 0x3d', '--dscp 0x3c'],
  ['socket mark', '--set-xmark 0x111/0xffffffff', '--set-xmark 0x112/0xffffffff'],
  ['connmark restore', '--restore-mark', '--wrong-restore'],
]) test(`wrong ${name} refuses`, () => assert.notEqual(fixture(v => {
  for (const key of Object.keys(v)) v[key] = v[key].replaceAll(from, to)
}).status, 0))
test('missing IPv6 table refuses', () => assert.notEqual(fixture(v => { v['6-mangle'] = '' }).status, 0))
test('duplicate terminal capture refuses', () => assert.notEqual(fixture(v => {
  v['4-nat'] += '-A xkeen -p tcp -m comment --comment xkeen_rule -j REDIRECT --to-ports 12345\n'
}).status, 0))
test('deny MAC bypass after capture jumps refuses', () => assert.notEqual(fixture(v => {
  v['4-nat'] = v['4-nat'].replace(/(-A PREROUTING -m set[^\n]+)\n/, '') + '-A PREROUTING -m set --match-set xkeen_deny_mac src -m comment --comment xkeen_rule -j RETURN\n'
}).status, 0))
test('wrong IPv6 ipset family refuses', () => assert.notEqual(fixture(v => { v.ipsets = v.ipsets.replace('geo_override6 hash:net family inet6', 'geo_override6 hash:net family inet') }).status, 0))
test('missing deny MAC set refuses', () => assert.notEqual(fixture(v => { v.ipsets = v.ipsets.replace(/^create xkeen_deny_mac[^\n]+\n/, '') }).status, 0))
test('wrong policy route lookup refuses', () => assert.notEqual(fixture(v => { v['6-rules'] = v['6-rules'].replace('lookup 111', 'lookup 112') }).status, 0))
test('missing local route refuses', () => assert.notEqual(fixture(v => { v['4-routes'] = v['4-source'] }).status, 0))
test('missing copied source route refuses', () => assert.notEqual(fixture(v => { v['6-routes'] = 'local default dev lo scope host\n' }).status, 0))
test('wrong explicit protocol in sole normal capture jump refuses', () => assert.notEqual(fixture(v => {
  v['4-nat'] = v['4-nat'].replace('-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -m comment --comment xkeen_rule -j xkeen\n', '-A PREROUTING -p udp -m conntrack ! --ctstate INVALID -m comment --comment xkeen_rule -j xkeen\n')
}).status, 0))
test('native policy-all normal jump without protocol remains supported', () => assert.equal(fixture(v => {
  v['4-nat'] = v['4-nat'].replace('-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -m comment --comment xkeen_rule -j xkeen\n', '-A PREROUTING -m conntrack ! --ctstate INVALID -m comment --comment xkeen_rule -j xkeen\n')
}).status, 0))
for (const [name, from, to] of [
  ['zero nfmask', '--nfmask 0xffffffff', '--nfmask 0x0'],
  ['zero ctmask', '--ctmask 0xffffffff', '--ctmask 0x0'],
  ['wrong restore state', '--ctstate RELATED,ESTABLISHED', '--ctstate NEW'],
  ['non-negated save mark', '! --mark 0x0', '--mark 0x0'],
  ['inverted force selector', '--dscp 0x3d', '! --dscp 0x3d'],
  ['inverted deny MAC set', '--match-set xkeen_deny_mac src', '! --match-set xkeen_deny_mac src'],
  ['inverted restore state', '--ctstate RELATED,ESTABLISHED', '! --ctstate RELATED,ESTABLISHED'],
]) test(`${name} refuses`, () => assert.notEqual(fixture(v => { v['4-mangle'] = v['4-mangle'].replaceAll(from, to) }).status, 0))
for (const [name, rule] of [
  ['source restricted', '100: from 192.0.2.0/24 fwmark 0x111 lookup 111'],
  ['inverted source', '100: not from all fwmark 0x111 lookup 111'],
  ['partial mark mask', '100: from all fwmark 0x111/0xff lookup 111'],
  ['different mark owned table', '100: from all fwmark 0x222 lookup 111'],
]) test(`${name} policy rule refuses`, () => assert.notEqual(fixture(v => {
  v['4-rules'] = v['4-rules'].replace('100: from all fwmark 0x111 lookup 111', rule)
}).status, 0))
test('extra conflicting mark rule refuses', () => assert.notEqual(fixture(v => { v['4-rules'] += '101: from all fwmark 0x111/0xff lookup 112\n' }).status, 0))
test('TPROXY before mark restoration refuses', () => assert.notEqual(fixture(v => {
  const lines = v['4-mangle'].split('\n'), i = lines.findIndex(l => l.startsWith('-A xkeen -p udp') && l.includes('-j TPROXY'))
  const [capture] = lines.splice(i, 1); lines.splice(lines.findIndex(l => l.startsWith('-A xkeen ') && l.includes('--restore-mark')), 0, capture)
  v['4-mangle'] = lines.join('\n')
}).status, 0))

function admitted({ expectation = 'running', change = '', alterHook = h => h, badCapability = false, startsStopped = false } = {}) {
  const code = mkdtempSync('/opt/native-hook-proof-'), root = mkdtempSync(join(tmpdir(), 'hook-proof-gate-'))
  chmodSync(code, 0o700); chmodSync(root, 0o700)
  try {
    const paths = { init: join(code, 'init'), hook: join(code, 'hook'), schedule: join(code, 'schedule'), configs: join(code, 'configs'), gate: join(code, 'gate'), entry: join(code, 'entry') }
    mkdirSync(paths.configs, { mode: 0o700 })
    const action = expectation === 'stopped' ? 'stop' : expectation === 'unchanged' ? 'start' : 'restart'
    const mode = expectation === 'unchanged' ? 'automatic' : 'forced'
    const settings = { name_chain: 'xkeen', comment_tag: 'xkeen_rule', table_mark: '0x111', table_id: '111', name_ipset_deny_mac: 'xkeen_deny_mac', name_client: 'xray', proxy_router: 'off', aghfix: 'off', start_auto: expectation === 'unchanged' ? 'off' : 'on', dscp_force_proxy_tag: 'force-proxy' }
    writeFileSync(paths.init, Object.entries(settings).map(([k, v]) => `${k}="${v}"`).join('\n') + '\n', { mode: 0o600 })
    const hook = { ...settings, mode_proxy: 'Hybrid', table_redirect: 'nat', table_tproxy: 'mangle', iptables_supported: 'true', ip6tables_supported: 'true', policy_mark_full: '', port_redirect: '12345', port_tproxy: '12346', port_dscp_force_proxy_redirect: '12347', port_dscp_force_proxy_tproxy: '12348', dscp_force_proxy: '61', policy_mark: '', ipv4_proxy: '127.0.0.1', ipv6_proxy: '::1', network_dscp_force_proxy_redirect: 'tcp', network_dscp_force_proxy_tproxy: 'udp' }
    writeFileSync(paths.hook, alterHook(Object.entries(hook).map(([k, v]) => `${k}='${v}'`).join('\n') + '\n'), { mode: 0o600 })
    writeFileSync(join(code, 'hook-next'), readFileSync(paths.hook), { mode: 0o600 })
    const inbounds = [12345, 12346, 12347, 12348].map((port, i) => ({ protocol: 'dokodemo-door', port, tag: ['main-redir', 'main-tproxy', 'force-proxy-redirect', 'force-proxy-tproxy'][i], settings: { followRedirect: true, network: i % 2 ? 'udp' : 'tcp' }, streamSettings: { sockopt: { tproxy: i % 2 ? 'tproxy' : 'redirect' } } }))
    writeFileSync(join(paths.configs, 'inbounds.json'), JSON.stringify({ inbounds }), { mode: 0o600 })
    const rewrite = s => s.replaceAll('/tmp/.xkeen-admission', root).replaceAll('/opt/lib/xkeen/native-operation-gate.sh', paths.gate).replaceAll('/opt/lib/xkeen/native-admission-entry.sh', paths.entry).replaceAll('/opt/etc/init.d/S05xkeen', paths.init).replaceAll('/opt/etc/ndm/netfilter.d/proxy.sh', paths.hook).replaceAll('/opt/etc/ndm/schedule.d/00-xkeen-hotspot-sync.sh', paths.schedule).replaceAll('/opt/etc/xray/configs', paths.configs)
    for (const [key, file] of [['gate', 'native-operation-gate.sh'], ['entry', 'native-admission-entry.sh']]) writeFileSync(paths[key], rewrite(readFileSync(`scripts/${file}`, 'utf8')), { mode: 0o600 })
    const views = makeViews()
    for (const [key, value] of Object.entries(views)) {
      writeFileSync(join(code, key), value, { mode: 0o600 }); writeFileSync(join(code, `${key}-next`), value, { mode: 0o600 })
    }
    const functions = rewrite(readFileSync('scripts/native-admission-hook-verify.sh', 'utf8').split('\n_hv_main "$@"')[0])
    writeFileSync(join(code, 'proof'), `${functions}
_hv_read_table() { [ ! -f '${code}/read-failure' ] || return 3; ${badCapability ? 'return 1' : `cat '${code}/'"$1-$2"`}; }
_hv_read_ipsets() { cat '${code}/ipsets'; }
_hv_read_rules() { cat '${code}/'"$1-rules"; }
_hv_read_routes() { if [ "$2" = 111 ]; then cat '${code}/'"$1-routes"; else cat '${code}/'"$1-source"; fi; }
_hv_main "$@"
exit $?
`, { mode: 0o600 })
    const stop = `
for family in 4 6; do
 for table in nat mangle; do printf '*%s\\n:PREROUTING ACCEPT [0:0]\\n:OUTPUT ACCEPT [0:0]\\nCOMMIT\\n' "$table" > '${code}/'"$family-$table"; done
 : > '${code}/'"$family-routes"; printf '0: from all lookup local\\n' > '${code}/'"$family-rules"
done
: > '${code}/ipsets'; : > '${paths.hook}'
`
    const r = spawnSync('/bin/sh', ['-c', `
. '${paths.gate}'
native_gate_acquire '${root}' ${action} || exit $?
mkdir -m 700 '${root}/operation.lock.d/call.init'
_native_gate_self || exit $?
XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef; XKEEN_ADMISSION_ROLE=init; XKEEN_ADMISSION_ACTION=${action}
export XKEEN_ADMISSION_CALL XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION
umask 077
printf 'v1 %s %s %s init ${action} ${mode} %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '${root}/operation.lock.d/call.init/context'
${startsStopped ? stop : ''}
/bin/sh '${code}/proof' pre init ${action} ${mode} ${expectation} || exit $?
echo PREFLIGHT
${startsStopped ? `for key in ${Object.keys(views).join(' ')} hook; do cp '${code}/'"$key-next" '${code}/'"$key"; done` : ''}
${expectation === 'stopped' ? stop : ''}
${change.replaceAll('@CODE@', code)}
/bin/sh '${code}/proof' post init ${action} ${mode} ${expectation}
`], { encoding: 'utf8' })
    return { ...r, baseline: existsSync(join(root, 'operation.lock.d/call.init/hook-preflight')) }
  } finally { rmSync(code, { recursive: true, force: true }); rmSync(root, { recursive: true, force: true }) }
}
test('real admission and protected forced Hybrid inputs settle readback baseline', () => {
  const r = admitted(); assert.equal(r.status, 0, r.stderr); assert.equal(r.baseline, false)
})
test('pre capability failure refuses before baseline/body', () => {
  const r = admitted({ badCapability: true }); assert.notEqual(r.status, 0); assert.equal(r.stdout, ''); assert.equal(r.baseline, false)
})
test('pre permits stopped kernel and empty hook before native Start creates them', () => {
  const r = admitted({ startsStopped: true }); assert.equal(r.status, 0, r.stderr); assert.equal(r.baseline, false)
})
test('Stop accepts native empty hook and absence of owned kernel state', () => {
  const r = admitted({ expectation: 'stopped' }); assert.equal(r.status, 0, r.stderr); assert.equal(r.baseline, false)
})
test('Stop refuses a surviving capture chain and retains baseline', () => {
  const r = admitted({ expectation: 'stopped', change: "printf ':xkeen - [0:0]\\n' >> @CODE@/6-mangle" }); assert.notEqual(r.status, 0); assert.equal(r.baseline, true)
})
test('Stop readback command failure cannot masquerade as absence', () => {
  const r = admitted({ expectation: 'stopped', change: ': > @CODE@/read-failure' }); assert.notEqual(r.status, 0); assert.equal(r.baseline, true)
})
test('automatic no-op compares bounded baseline and removes only after match', () => {
  const r = admitted({ expectation: 'unchanged' }); assert.equal(r.status, 0, r.stderr); assert.equal(r.baseline, false)
  const changed = admitted({ expectation: 'unchanged', change: "printf 'changed\\n' >> @CODE@/4-routes" }); assert.notEqual(changed.status, 0); assert.equal(changed.baseline, true)
})
test('generated hook is parsed as data, never executed', () => {
  const r = admitted({ alterHook: h => h + 'exit 42\n' }); assert.equal(r.status, 0, r.stderr)
})
test('duplicate generated identity refuses', () => {
  const r = admitted({ alterHook: h => h + "name_chain='xkeen'\n" }); assert.notEqual(r.status, 0); assert.equal(r.baseline, false); assert.equal(r.stdout, '')
})
test('unsupported conditional profile refuses before body', () => {
  const r = admitted({ alterHook: h => h.replace("aghfix='off'", "aghfix='on'") }); assert.notEqual(r.status, 0); assert.equal(r.stdout, '')
})
test('automatic unchanged ignores counters and unrelated set/rule metadata', () => {
  const r = admitted({ expectation: 'unchanged', change: "sed -i 's/\\[0:0\\]/[100:200]/g' @CODE@/4-nat; printf 'create unrelated hash:ip family inet\\n' >> @CODE@/ipsets; printf '200: from all fwmark 0x999 lookup 999\\n' >> @CODE@/4-rules" }); assert.equal(r.status, 0, r.stderr)
})
test('generated port differing from validated native inbound refuses', () => {
  const r = admitted({ alterHook: h => h.replace("port_redirect='12345'", "port_redirect='9999'") }); assert.notEqual(r.status, 0); assert.equal(r.baseline, true)
})

function queryFixture(body) {
  const dir = mkdtempSync(join(tmpdir(), 'bounded-hook-query-'))
  chmodSync(dir, 0o700)
  try {
    writeFileSync(join(dir, 'ipset'), `#!/bin/sh\n[ "$*" = 'list -terse' ] || exit 99\n${body}\n`, { mode: 0o700 })
    const functions = readFileSync('scripts/native-admission-hook-verify.sh', 'utf8').split('\n_hv_main "$@"')[0]
    const r = spawnSync('/bin/sh', ['-c', `
. ./scripts/native-operation-gate.sh
${functions}
PATH='${dir}':$PATH; export PATH
_na_call_dir='${dir}'
_na_descendant_ok() { return 0; }
_hv_read_ipsets
`], { encoding: 'utf8' })
    return { ...r, queryLeft: existsSync(join(dir, 'hook-query')) }
  } finally { rmSync(dir, { recursive: true, force: true }) }
}
test('finite metadata query uses terse output without set memberships', () => {
  const r = queryFixture("printf 'Name: xkeen_deny_mac\\nType: hash:mac\\nRevision: 1\\nHeader: hashsize 1024 maxelem 65536\\nSize in memory: 100\\nReferences: 2\\nNumber of entries: 0\\n'")
  assert.equal(r.status, 0, r.stderr); assert.equal(r.stdout, 'create xkeen_deny_mac hash:mac\n'); assert.equal(r.queryLeft, false)
})
test('failed producer cannot masquerade as absent ipsets', () => {
  const r = queryFixture('exit 3'); assert.notEqual(r.status, 0); assert.equal(r.stdout, ''); assert.equal(r.queryLeft, false)
})
test('producer failure after plausible metadata still refuses', () => {
  const r = queryFixture("printf 'Name: xkeen_deny_mac\\nType: hash:mac\\nHeader: hashsize 1024\\n'; exit 3")
  assert.notEqual(r.status, 0); assert.equal(r.stdout, '')
})
test('producer output is hard bounded before shell capture', () => {
  const r = queryFixture('exec head -c 1048576 /dev/zero'); assert.notEqual(r.status, 0); assert.equal(r.stdout, ''); assert.equal(r.queryLeft, false)
})
test('unknown metadata grammar cannot masquerade as absence', () => {
  assert.notEqual(queryFixture("printf 'unexpected output\\n'").status, 0)
})
