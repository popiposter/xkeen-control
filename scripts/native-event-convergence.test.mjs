import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

// These fixtures qualify the elected caller, not real native reconciliation.
// The synthetic fixed worker joins the actual shell gate and records ready state.
function fixture(body = '', worker = '') {
  const root = mkdtempSync('/tmp/native-convergence-')
  const code = mkdtempSync('/root/native-convergence-code-')
  chmodSync(root, 0o700)
  chmodSync(code, 0o700)
  const paths = {
    '/tmp/.xkeen-admission': root,
    '/opt/lib/xkeen/native-event-reconcile.sh': join(code, 'worker'),
    '/opt/libexec/timeout-coreutils': '/usr/bin/timeout',
    '/opt/bin/sh': '/bin/sh',
  }
  const rewrite = text => Object.entries(paths).reduce((s, [a, b]) => s.replaceAll(a, b), text).replaceAll('@ROOT@', root)
  for (const [name, source] of [['gate', 'native-operation-gate'], ['entry', 'native-admission-entry'], ['notification', 'native-event-notification'], ['caller', 'native-event-convergence']]) {
    writeFileSync(join(root, name), rewrite(readFileSync(`scripts/${source}.sh`, 'utf8')), { mode: 0o600 })
  }
  writeFileSync(join(code, 'worker'), rewrite(`. '@ROOT@/gate'
native_gate_join '@ROOT@' "$XKEEN_GATE_TOKEN" || exit 77
[ "$_ng_action" = reconcile ] || exit 77
${worker || 'cat "@ROOT@/ready" >> "@ROOT@/observed"'}
`), { mode: 0o600 })
  writeFileSync(join(root, 'ready'), 'running\n', { mode: 0o600 })
  const script = rewrite(`. '@ROOT@/gate'; . '@ROOT@/entry'; . '@ROOT@/notification'; . '@ROOT@/caller'
${(body || 'native_event_converge').replaceAll('@WORKER@', join(code, 'worker'))}
`)
  try {
    const r = spawnSync('/bin/sh', ['-c', script], { encoding: 'utf8', timeout: 4000 })
    assert.ifError(r.error)
    return { ...r, gate: existsSync(join(root, 'operation.lock.d')), leader: existsSync(join(root, 'events/leader')), dirty: existsSync(join(root, 'events/dirty')), observed: existsSync(join(root, 'observed')) ? readFileSync(join(root, 'observed'), 'utf8') : '' }
  } finally { rmSync(root, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('fixed worker joins reconcile admission, then successful pass retires cleanly', () => {
  const r = fixture()
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.observed, 'running\n')
  assert.equal(r.gate || r.leader || r.dirty, false)
})
test('foreground lifecycle hints refuse election instead of waiting on its own gate', () => {
  const r = fixture('XKEEN_GATE_ROOT=@ROOT@; native_event_converge')
  assert.equal(r.status, 76); assert.equal(r.leader || r.gate || r.dirty, false)
})
test('queued follower returns 75 without executing a second worker', () => {
  const r = fixture("native_event_notify || exit $?; native_event_converge")
  assert.equal(r.status, 75); assert.equal(r.observed, '')
  assert.equal(r.leader && r.dirty, true); assert.equal(r.gate, false)
})
test('missing fixed capability preserves explicit event before native effects', () => {
  const r = fixture("rm '@WORKER@'; native_event_converge")
  assert.equal(r.status, 76); assert.equal(r.leader && r.dirty, true); assert.equal(r.gate, false)
})
test('busy admission deadline retains event without consuming or starting worker', () => {
  const r = fixture(`native_gate_acquire() { return 75; }
_nec_tick=0; _nec_clock() { _nec_now=$_nec_tick; }
sleep() { _nec_tick=$((_nec_tick + 1)); }
native_event_converge`)
  assert.equal(r.status, 77); assert.equal(r.observed, '')
  assert.equal(r.leader && r.dirty, true); assert.equal(r.gate, false)
})
test('Stop winning during admission wait is observed by the subsequently loaded worker', () => {
  const r = fixture(`eval "$(sed 's/^native_gate_acquire()/original_acquire()/' '@ROOT@/gate')"
_nec_first=1
native_gate_acquire() { if [ "$_nec_first" = 1 ]; then _nec_first=0; return 75; fi; original_acquire "$@"; }
sleep() { printf 'stopped\\n' > '@ROOT@/ready'; }
native_event_converge`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.observed, 'stopped\n')
  assert.equal(r.gate || r.leader || r.dirty, false)
})
test('failed native pass re-publishes dirty and retains uncertain admission without replay', () => {
  const r = fixture('', 'echo attempt >> "@ROOT@/observed"; exit 1')
  assert.equal(r.status, 77); assert.equal(r.observed, 'attempt\n')
  assert.equal(r.gate && r.leader && r.dirty, true)
})
test('actual timeout kills unfinished worker and retains uncertain admission without replay', () => {
  const r = fixture('_nec_remaining() { _nec_left=1; }\nnative_event_converge', 'echo attempt >> "@ROOT@/observed"; sleep 3; echo LATE >> "@ROOT@/observed"')
  assert.equal(r.status, 77); assert.equal(r.observed, 'attempt\n')
  assert.equal(r.gate && r.leader && r.dirty, true)
})
test('worker exit zero after total deadline cannot claim completed convergence', () => {
  const r = fixture('_nec_clock() { _nec_now=1; [ ! -e "@ROOT@/observed" ] || _nec_now=16; }\nnative_event_converge', 'echo attempt >> "@ROOT@/observed"')
  assert.equal(r.status, 77); assert.equal(r.observed, 'attempt\n')
  assert.equal(r.gate && r.leader && r.dirty, true)
})
test('event during a pass requires another bounded current-state pass', () => {
  const r = fixture('', `if [ ! -e '@ROOT@/observed' ]; then mkdir -m 700 '@ROOT@/events/dirty'; fi
echo pass >> '@ROOT@/observed'`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.observed, 'pass\npass\n')
  assert.equal(r.gate || r.leader || r.dirty, false)
})
test('continuous notifications hit pass cap, leave explicit unresolved event, no ninth replay', () => {
  const r = fixture('', `mkdir -m 700 '@ROOT@/events/dirty'
echo pass >> '@ROOT@/observed'`)
  assert.equal(r.status, 77, r.stderr); assert.equal(r.observed, 'pass\n'.repeat(8))
  assert.equal(r.leader && r.dirty, true); assert.equal(r.gate, false)
})
