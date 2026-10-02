// Extracted pinned hook functions; synthetic external commands, no router scripts.
import assert from 'node:assert/strict'
import { readFileSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs'
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
const apply = extract('    _xkeen_apply_table() {\n', '\n    # Пока NDM')
function run(text) { return spawnSync('/bin/sh', ['-c', text], { encoding: 'utf8' }) }

test('real table function exhausts three restore attempts with failure', () => {
  const dir = mkdtempSync(join(tmpdir(), 'hook-restore-'))
  try {
  writeFileSync(join(dir, 'iptables-save'), '#!/bin/sh\nexit 0\n', { mode: 0o700 })
  writeFileSync(join(dir, 'iptables-restore'), '#!/bin/sh\ncat >/dev/null\necho ATTEMPT >> "$ATTEMPTS"\nexit 1\n', { mode: 0o700 })
  const r = run(`${apply}
PATH='${dir}':$PATH; ATTEMPTS='${dir}/attempts'; export PATH ATTEMPTS
iptables_supported=true; name_chain=xkeen; rules='-A xkeen -j RETURN'
logger() { :; }; usleep() { :; }
_xkeen_apply_table iptables nat rules
`)
  assert.equal(r.status, 1)
  assert.equal(readFileSync(join(dir, 'attempts'), 'utf8'), 'ATTEMPT\n'.repeat(3))
  assert.equal(r.signal, null)
  } finally { rmSync(dir, { recursive: true, force: true }) }
})

for (const failed of ['iptables:nat', 'iptables:mangle', 'ip6tables:nat', 'ip6tables:mangle']) test(`Hybrid aggregator propagates ${failed} restore failure`, () => {
  const r = run(`${apply}
iptables_supported=true; ip6tables_supported=true
_xkeen_apply_table() { printf '%s:%s\\n' "$1" "$2"; [ "$1:$2" != '${failed}' ]; }
_xkeen_apply
`)
  assert.equal(r.status, 1)
  assert.ok(r.stdout.includes(failed))
})

test('unsupported family is skipped without turning successful application into failure', () => {
  const r = run(`${apply}
iptables_supported=true; ip6tables_supported=false
_xkeen_apply_table() { printf '%s:%s\\n' "$1" "$2"; }
_xkeen_apply
`)
  assert.equal(r.status, 0)
  assert.equal(r.stdout, 'iptables:nat\niptables:mangle\n')
})

test('cached hook failure stops before state publication and success cleanup', () => {
  const cached = extract('    if _xkeen_cache_valid; then\n', '\n    if [ -n "$port_donor" ]')
  const r = run(`
_xkeen_cache_valid() { return 0; }; _xkeen_ensure_ipsets() { :; }; _xkeen_cache_load() { :; }
iptables_supported=false; ip6tables_supported=false
_xkeen_apply() { echo APPLY; return 1; }
_xkeen_release_nf_lock() { echo RELEASE; }; _xkeen_flush_udp_conntrack() { echo FLUSH; }; _xkeen_sync_deny_mac_ipset() { echo SYNC; }
${cached}
`)
  assert.equal(r.status, 1)
  assert.equal(r.stdout, 'APPLY\n')
})

test('full rebuild failure stops before caching or success cleanup', () => {
  const start = source.indexOf('\n    _xkeen_apply || exit 1\n') >= 0
    ? '\n    _xkeen_apply || exit 1\n' : '\n    _xkeen_apply\n'
  const tail = extract(start, '\nelse\n    # mkdir-lock')
  const r = run(`
_xkeen_apply() { echo APPLY; return 1; }; _xkeen_cache_save() { echo CACHE; }
_xkeen_release_nf_lock() { echo RELEASE; }; _xkeen_flush_udp_conntrack() { echo FLUSH; }; _xkeen_sync_deny_mac_ipset() { echo SYNC; }
${tail}
`)
  assert.equal(r.status, 1)
  assert.equal(r.stdout, 'APPLY\n')
})

test('same WAN and intact rules still refresh schedule-driven deny MAC state', () => {
  const fast = extract('    if [ -n "$_xkeen_cur_wan" ]', '\n    if _xkeen_rules_intact; then')
  const r = run(`
_xkeen_cur_wan=192.0.2.1; _xkeen_prev_wan=192.0.2.1
iptables_supported=false; ip6tables_supported=false
_xkeen_rules_intact() { return 0; }; _xkeen_release_nf_lock() { echo RELEASE; }; _xkeen_sync_deny_mac_ipset() { echo SYNC; }
${fast}
`)
  assert.equal(r.status, 0)
  assert.equal(r.stdout, 'RELEASE\nSYNC\n')
})
