import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

function fixture(body) {
  const root = mkdtempSync(join(tmpdir(), 'native-event-'))
  chmodSync(root, 0o700)
  const gate = join(root, 'gate'), helper = join(root, 'notification')
  writeFileSync(gate, readFileSync('scripts/native-operation-gate.sh'), { mode: 0o600 })
  writeFileSync(helper, readFileSync('scripts/native-event-notification.sh', 'utf8').replaceAll('/tmp/.xkeen-admission', root), { mode: 0o600 })
  try {
    const r = spawnSync('/bin/sh', ['-c', `. '${gate}'; . '${helper}'\n${body.replaceAll('@ROOT@', root).replaceAll('@HELPER@', helper).replaceAll('@GATE@', gate)}`], { encoding: 'utf8', timeout: 2000 })
    assert.ifError(r.error)
    return { ...r, leader: existsSync(join(root, 'events/leader')), dirty: existsSync(join(root, 'events/dirty')) }
  } finally { rmSync(root, { recursive: true, force: true }) }
}

test('one invocation owns transient notification election and clean retirement', () => {
  const r = fixture('native_event_notify || exit $?; native_event_consume || exit $?; native_event_retire')
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, false); assert.equal(r.dirty, false)
})

test('own unused election defers while preserving dirty state', () => {
  const r = fixture('native_event_notify || exit $?; native_event_defer')
  assert.equal(r.status, 75); assert.equal(r.leader, false); assert.equal(r.dirty, true)
})

test('deferral refuses unknown dirty or leader entries before unlinking its owner', () => {
  for (const dir of ['dirty', 'leader']) {
    const r = fixture(`native_event_notify || exit $?
touch '@ROOT@/events/${dir}/.foreign'
native_event_defer; [ "$?" = 77 ] && [ -f '@ROOT@/events/leader/owner' ]`)
    assert.equal(r.status, 0, r.stderr); assert.equal(r.leader && r.dirty, true)
  }
})

test('inherited leader hints do not authorize deferral', () => {
  const r = fixture(`native_event_notify || exit $?; export _ne_owned
/bin/sh -c '. "@GATE@"; . "@HELPER@"; native_event_defer; [ "$?" = 77 ]'`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader && r.dirty, true)
})

test('notification during deferral cleanup stays dirty without reconstructing a leader', () => {
  const r = fixture(`native_event_notify || exit $?
rm() { /bin/rm "$@" || return $?; /bin/sh -c '. "@GATE@"; . "@HELPER@"; native_event_notify; [ "$?" = 77 ]'; }
native_event_defer; [ "$?" = 75 ] || exit 9
unset -f rm
native_event_notify || exit $?; native_event_consume || exit $?; native_event_retire`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader || r.dirty, false)
})

test('deferral never removes a successor elected after its leader retirement', () => {
  const r = fixture(`native_event_notify || exit $?
rmdir() { /bin/rmdir "$@" || return $?; /bin/sh -c '. "@GATE@"; . "@HELPER@"; native_event_notify' || return $?; }
native_event_defer; [ "$?" = 75 ] && [ -z "$_ne_owned" ] && [ -f '@ROOT@/events/leader/owner' ]`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader && r.dirty, true)
})
test('concurrent event queues for current leader, without a second executor', () => {
  const r = fixture(`native_event_notify || exit $?
native_event_consume || exit $?
/bin/sh -c '. "@GATE@"; . "@HELPER@"; native_event_notify; [ "$?" = 75 ]' || exit 9
native_event_retire; [ "$?" = 75 ] || exit 10
native_event_consume || exit $?
native_event_retire`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, false); assert.equal(r.dirty, false)
})
test('event arriving during retirement remains dirty and causes re-election', () => {
  const r = fixture(`native_event_notify || exit $?; native_event_consume || exit $?
rmdir() { /bin/rmdir "$@" || return $?; if [ "$1" = '@ROOT@/events/leader' ]; then mkdir -m 700 '@ROOT@/events/dirty'; fi; }
native_event_retire; [ "$?" = 75 ] && [ -z "$_ne_owned" ] || exit 9
unset -f rmdir
native_event_notify || exit $?; native_event_consume || exit $?; native_event_retire`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, false); assert.equal(r.dirty, false)
})
test('inherited leader hints cannot authorize another process to consume or retire', () => {
  const r = fixture(`native_event_notify || exit $?
export _ne_owned
/bin/sh -c '. "@GATE@"; . "@HELPER@"; native_event_consume; [ "$?" = 77 ] && native_event_retire; [ "$?" = 77 ]'`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, true); assert.equal(r.dirty, true)
})
test('dead or incomplete election is unresolved and is never reaped', () => {
  for (const body of [
    '/bin/sh -c \'. "@GATE@"; . "@HELPER@"; native_event_notify\' || exit $?;',
    'mkdir -m 700 "@ROOT@/events" "@ROOT@/events/leader";',
  ]) {
    const r = fixture(`${body}\nnative_event_notify; [ "$?" = 77 ]`)
    assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, true); assert.equal(r.dirty, true)
  }
})
test('replacement record prevents stale retirement and leaves notification intact', () => {
  const r = fixture(`native_event_notify || exit $?
printf 'invalid\n' > '@ROOT@/events/leader/owner'
native_event_retire; [ "$?" = 77 ]`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, true); assert.equal(r.dirty, true)
})
test('unsafe notification directory refuses before election or deletion', () => {
  const r = fixture(`mkdir -m 777 '@ROOT@/events'
native_event_notify; [ "$?" = 76 ]`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, false); assert.equal(r.dirty, false)
})
test('non-empty dirty state refuses consumption rather than deleting unknown entries', () => {
  const r = fixture(`native_event_notify || exit $?; touch '@ROOT@/events/dirty/unknown'
native_event_consume; [ "$?" = 77 ]`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.leader, true); assert.equal(r.dirty, true)
})
