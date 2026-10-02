// Actual pinned generated-hook prefix/terminals with real foreground admission.
// Native lifecycle/core/kernel observations are synthetic; no full upstream runs.
import assert from 'node:assert/strict'
import { readFileSync, writeFileSync, mkdtempSync, mkdirSync, chmodSync, existsSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const candidate = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) }).init.toString()
function extract(start, end) {
  const a = candidate.indexOf(start), b = candidate.indexOf(end, a + start.length)
  assert.ok(a >= 0 && b > a)
  return candidate.slice(a + start.length, b)
}
const prefix = extract('cat > "$file_netfilter_hook" <<\'EOL\'\n', '\nEOL')
const tails = [
  ['    if [ -n "$_xkeen_cur_wan" ]', '\n    if _xkeen_rules_intact; then'],
  ['    if _xkeen_rules_intact; then', '\n    # Кэш готовых'],
  ['    if _xkeen_cache_valid; then', '\n    if [ -n "$port_donor" ]'],
  ['\n    _xkeen_apply || exit 1\n', '\nelse\n    # mkdir-lock'],
].map(([start, end]) => start + extract(start, end))

function fixture({ terminal = 0, direct = false, ready = true, syncFail = false, verifyFail = false } = {}) {
  const root = mkdtempSync(join(tmpdir(), 'foreground-hook-gate-'))
  const code = mkdtempSync('/opt/foreground-hook-code-')
  chmodSync(root, 0o700); chmodSync(code, 0o700)
  const paths = { entry: join(code, 'entry'), gate: join(code, 'gate'), init: join(code, 'init'), hook: join(code, 'hook'), verify: join(code, 'verify'), native: join(root, 'native'), evidence: join(code, 'effects') }
  const rewrite = s => s.replaceAll('/tmp/.xkeen-admission', root).replaceAll('/tmp/.xkeen', paths.native)
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', paths.entry).replaceAll('/opt/lib/xkeen/native-operation-gate.sh', paths.gate)
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', paths.verify).replaceAll('/opt/etc/init.d/S05xkeen', paths.init)
    .replaceAll('/opt/etc/ndm/netfilter.d/proxy.sh', paths.hook).replaceAll('/opt/bin/sh', '/bin/sh')
  const put = (file, s) => writeFileSync(file, s, { mode: 0o600 })
  put(paths.entry, rewrite(readFileSync('scripts/native-admission-entry.sh', 'utf8')))
  put(paths.gate, readFileSync('scripts/native-operation-gate.sh', 'utf8'))
  put(paths.verify, `#!/bin/sh\n${verifyFail ? '[ "$1:$2" != post:hook ]' : 'exit 0'}\n`)
  put(paths.init, rewrite(`#!/bin/sh
. /opt/lib/xkeen/native-admission-entry.sh
native_admission_enter init start forced || exit $?
[ "$_na_body" = 1 ] || exit 0
/bin/sh /opt/etc/ndm/netfilter.d/proxy.sh || exit $?
native_admission_finish 0
exit $?
`))
  put(paths.hook, rewrite(`${prefix}
iptables_supported=false; ip6tables_supported=false
_xkeen_cur_wan=192.0.2.1; _xkeen_prev_wan=192.0.2.1; _xkeen_wan_state="$_xkeen_rundir/wan"
_xkeen_rules_intact() { return 0; }; _xkeen_cache_valid() { return 0; }
_xkeen_ensure_ipsets() { :; }; _xkeen_cache_load() { :; }; _xkeen_apply() { :; }
_xkeen_release_nf_lock() { :; }; _xkeen_flush_udp_conntrack() { :; }
_xkeen_sync_deny_mac_ipset() { echo SYNC >> '${paths.evidence}'; return ${syncFail ? 1 : 0}; }
_xkeen_cache_save() { echo CACHE >> '${paths.evidence}'; }
${tails[terminal]}
`))
  try {
    if (!direct && ready) { mkdirSync(paths.native, { mode: 0o700 }); put(join(paths.native, 'ready'), '') }
    const r = spawnSync('/bin/sh', [direct ? paths.hook : paths.init], { encoding: 'utf8', timeout: 4000 })
    assert.ifError(r.error)
    return { ...r, held: existsSync(join(root, 'operation.lock.d')), nativeCreated: existsSync(paths.native), effects: existsSync(paths.evidence) ? readFileSync(paths.evidence, 'utf8') : '' }
  } finally { rmSync(root, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

for (const terminal of [0, 1, 2, 3]) test(`actual native terminal ${terminal} completes borrowed hook only after writers and readback`, () => {
  const r = fixture({ terminal })
  assert.equal(r.status, 0, r.stderr); assert.equal(r.held, false)
  assert.equal(r.effects, terminal === 3 ? 'SYNC\nCACHE\n' : 'SYNC\n')
  const failed = fixture({ terminal, syncFail: true })
  assert.notEqual(failed.status, 0); assert.equal(failed.held, true); assert.equal(failed.effects, 'SYNC\n')
})
test('unowned generated hook refuses before native runtime directory repair', () => {
  const r = fixture({ direct: true })
  assert.notEqual(r.status, 0); assert.equal(r.held, false); assert.equal(r.nativeCreated, false); assert.equal(r.effects, '')
})
test('missing native ready or failed hook postcondition retains parent admission', () => {
  for (const options of [{ ready: false }, { verifyFail: true }]) {
    const r = fixture(options)
    assert.notEqual(r.status, 0); assert.equal(r.held, true)
  }
})
