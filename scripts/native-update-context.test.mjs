import assert from 'node:assert/strict'
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { join } from 'node:path'
import { test } from 'node:test'

function fixture({ action = 'update-xkeen', setup = '', child = 'native_update_stage_context "$stage"', body = '', borrowed = false } = {}) {
  const root = mkdtempSync('/tmp/native-update-context-')
  chmodSync(root, 0o700)
  const put = (name, text) => writeFileSync(join(root, name), text.replaceAll('/tmp/.xkeen-admission', root).replaceAll('@ROOT@', root), { mode: 0o600 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  put('child', `. '@ROOT@/native-operation-gate'; . '@ROOT@/native-admission-entry'; . '@ROOT@/native-update-context'
${setup}
${child}
`)
  put('body', `. '@ROOT@/native-operation-gate'
_native_gate_self || exit 90
stage="/opt/sbin/.xkeen.stage.$_ng_self_pid"; export stage
(umask 077; set -C; printf 'v1 %s %s %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" > '@ROOT@/operation.lock.d/call.update/body') || exit 91
${body}
/bin/sh '@ROOT@/child'
`)
  put('wrapper', `. '@ROOT@/native-operation-gate'
${borrowed ? "native_gate_join '@ROOT@' \"$XKEEN_GATE_TOKEN\"" : `native_gate_acquire '@ROOT@' '${action}'`} || exit 90
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@ROOT@/operation.lock.d/call.update' || exit 91
(umask 077; printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@ROOT@/operation.lock.d/call.update/context') || exit 92
/bin/sh '@ROOT@/body'
rc=$?
# Authentication helper must never release or settle the owner.
[ -f '@ROOT@/operation.lock.d/owner' ] || exit 93
exit "$rc"
`)
  put('owner', `. '@ROOT@/native-operation-gate'
native_gate_acquire '@ROOT@' '${action}' || exit 90
/bin/sh '@ROOT@/wrapper'
`)
  try {
    const result = spawnSync('/bin/sh', [join(root, borrowed ? 'owner' : 'wrapper')], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(result.error)
    return result
  } finally { rmSync(root, { recursive: true, force: true }) }
}

test('only the fixed stage child of the bound native update body authenticates', () => {
  const r = fixture(); assert.equal(r.status, 0, r.stderr)
})
test('foreground update wrapper may borrow the same ancestor owner without a second gate', () => {
  const r = fixture({ borrowed: true }); assert.equal(r.status, 0, r.stderr)
})
test('lifecycle gate and forged update nonce cannot grant stage authority', () => {
  for (const options of [{ action: 'restart' }, { setup: 'XKEEN_ADMISSION_CALL=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' }]) {
    const r = fixture(options); assert.equal(r.status, 77, r.stderr)
  }
})
test('extra descendant with inherited hints is not a stage writer', () => {
  const r = fixture({ child: `/bin/sh -c '. "@ROOT@/native-operation-gate"; . "@ROOT@/native-admission-entry"; . "@ROOT@/native-update-context"; native_update_stage_context "$stage"'` })
  assert.equal(r.status, 77, r.stderr)
})
test('caller cannot choose a different stage path', () => {
  const r = fixture({ child: 'native_update_stage_context /opt/sbin/.xkeen.stage.1' })
  assert.equal(r.status, 76, r.stderr)
})
test('missing, noncanonical and unsafe body records fail closed', () => {
  for (const body of [
    "rm '@ROOT@/operation.lock.d/call.update/body'",
    "printf 'invalid\\n' > '@ROOT@/operation.lock.d/call.update/body'",
    "chmod 644 '@ROOT@/operation.lock.d/call.update/body'",
    "printf 'v1 %s 1 %s\\n' \"$_ng_self_pid\" \"$XKEEN_ADMISSION_CALL\" > '@ROOT@/operation.lock.d/call.update/body'",
    "mv '@ROOT@/operation.lock.d/call.update/body' '@ROOT@/saved'; ln -s '@ROOT@/saved' '@ROOT@/operation.lock.d/call.update/body'",
  ]) {
    const r = fixture({ body }); assert.equal(r.status, 77, r.stderr)
  }
})
test('changed wrapper context and gate token fail closed', () => {
  for (const setup of [
    'XKEEN_GATE_TOKEN=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    "sed -i 's/ forced / automatic /' '@ROOT@/operation.lock.d/call.update/context'",
    "chmod 644 '@ROOT@/operation.lock.d/call.update/context'",
  ]) {
    const r = fixture({ setup }); assert.equal(r.status, 77, r.stderr)
  }
})
test('context or body changed after initial reads fails final proof without settling the owner', () => {
  for (const target of ['context', 'body']) {
    const r = fixture({ setup: `eval "$(sed 's/^_nu_read_record()/original_read_record()/' '@ROOT@/native-update-context')"
_nu_reads=0
_nu_read_record() {
  original_read_record "$1" || return $?
  _nu_reads=$((_nu_reads + 1))
  if [ "$_nu_reads" = 2 ]; then printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/${target}'; fi
}` })
    assert.equal(r.status, 77, r.stderr)
  }
})
