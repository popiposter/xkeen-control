import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { join } from 'node:path'
import { test } from 'node:test'

const hash = value => createHash('sha256').update(value).digest('hex')
const argvHash = hash(Buffer.from('/opt/bin/sh\0/opt/sbin/xkeen\0-uk_post_update\0'))
assert.ok(readFileSync('scripts/native-update-context.sh', 'utf8').includes(argvHash))
function fixture({ action = 'update-xkeen', setup = '', child = 'native_update_stage_context "$stage"', body = '', borrowed = false, beforeBind = '', execFlow = false, stageFlow = false, execProbe = 'native_update_exec_context', stagedChange = '', execArgument = '-uk_post_update', withoutExec = false } = {}) {
  const root = mkdtempSync('/tmp/native-update-context-')
  const code = mkdtempSync('/root/native-update-exec-')
  chmodSync(code, 0o700)
  const executable = join(code, 'xkeen')
  chmodSync(root, 0o700)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', root).replaceAll('@ROOT@', root)
    .replaceAll('/opt/sbin/.xkeen.stage.', `${code}/.xkeen.stage.`)
    .replaceAll('/opt/sbin/xkeen', executable).replaceAll('@EXEC@', executable)
    .replaceAll(argvHash, hash(Buffer.from(`/bin/sh\0${executable}\0-uk_post_update\0`)))
  const put = (name, text) => writeFileSync(join(root, name), rewrite(text), { mode: 0o600 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  put('child', `. '@ROOT@/native-operation-gate'; . '@ROOT@/native-admission-entry'; . '@ROOT@/native-update-context'
${setup}
${execFlow ? `native_update_bind_staged "$stage" || exit $?
${stagedChange}` : child}
`)
  writeFileSync(executable, rewrite(`#!/bin/sh
. '@ROOT@/native-operation-gate'; . '@ROOT@/native-admission-entry'; . '@ROOT@/native-update-context'
${execProbe}
`), { mode: 0o600 })
  put('body', `. '@ROOT@/native-operation-gate'; . '@ROOT@/native-admission-entry'; . '@ROOT@/native-update-context'
${beforeBind}
native_update_bind_body || exit $?
stage="/opt/sbin/.xkeen.stage.$_ng_self_pid"; export stage
${execFlow || stageFlow ? `mkdir -m 700 "$stage"; cp '@EXEC@' "$stage/xkeen"; chmod 600 "$stage/xkeen"` : ''}
${body}
/bin/sh '@ROOT@/child'
${execFlow ? `[ "$?" = 0 ] || exit 95
${withoutExec ? execProbe : `exec /bin/sh '@EXEC@' '${execArgument}'`}` : ''}
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
    return { ...result, bodyPresent: existsSync(join(root, 'operation.lock.d/call.update/body')),
      stagedPresent: existsSync(join(root, 'operation.lock.d/call.update/staged')),
      execUsed: existsSync(join(root, 'operation.lock.d/call.update/exec.used')),
      execArgv: existsSync(join(root, 'operation.lock.d/call.update/exec.argv')) }
  } finally { rmSync(root, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('only the fixed stage child of the bound native update body authenticates', () => {
  const r = fixture(); assert.equal(r.status, 0, r.stderr)
})
test('foreground update wrapper may borrow the same ancestor owner without a second gate', () => {
  const r = fixture({ borrowed: true }); assert.equal(r.status, 0, r.stderr)
})
test('body binding is one-use and a deeper inherited child cannot publish it', () => {
  const repeated = fixture({ body: 'native_update_bind_body; exit $?' })
  assert.equal(repeated.status, 77, repeated.stderr)
  const deeper = fixture({ body: `rm '@ROOT@/operation.lock.d/call.update/body'
/bin/sh -c '. "@ROOT@/native-operation-gate"; . "@ROOT@/native-admission-entry"; . "@ROOT@/native-update-context"; native_update_bind_body'
exit $?` })
  assert.equal(deeper.status, 77, deeper.stderr)
})
test('body binding never opens or replaces an existing foreign path', () => {
  for (const beforeBind of [
    "mkdir '@ROOT@/operation.lock.d/call.update/body'",
    "mkfifo '@ROOT@/operation.lock.d/call.update/body'",
    "ln -s '@ROOT@/absent' '@ROOT@/operation.lock.d/call.update/body'",
  ]) {
    const r = fixture({ beforeBind }); assert.equal(r.status, 77, r.stderr)
  }
})
test('context drift after body publication retains the body record and refuses completion', () => {
  const r = fixture({ beforeBind: `eval "$(sed 's/^_nu_recheck_call()/original_recheck_call()/' '@ROOT@/native-update-context')"
_nu_recheck_call() {
  printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/context'
  original_recheck_call
}` })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.bodyPresent, true)
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
test('real same-process exec authenticates only the fixed post-update generation', () => {
  const r = fixture({ execFlow: true }); assert.equal(r.status, 0, r.stderr)
})
test('fixed stage child publishes once and never replaces an existing receipt path', () => {
  const success = fixture({ stageFlow: true, child: 'native_update_bind_staged "$stage"' })
  assert.equal(success.status, 0, success.stderr)
  assert.equal(success.stagedPresent, true)
  const repeated = fixture({ stageFlow: true, child: 'native_update_bind_staged "$stage" || exit $?; native_update_bind_staged "$stage"' })
  assert.equal(repeated.status, 77, repeated.stderr)
  for (const setup of [
    "mkdir '@ROOT@/operation.lock.d/call.update/staged'",
    "mkfifo '@ROOT@/operation.lock.d/call.update/staged'",
    "ln -s '@ROOT@/absent' '@ROOT@/operation.lock.d/call.update/staged'",
  ]) {
    const r = fixture({ stageFlow: true, setup, child: 'native_update_bind_staged "$stage"' })
    assert.equal(r.status, 77, r.stderr)
  }
})
test('staged receipt refuses missing, unsafe and oversized dispatcher without publishing', () => {
  for (const setup of [
    'rm "$stage/xkeen"',
    'chmod 666 "$stage/xkeen"',
    'chmod 777 "$stage"',
    'rm "$stage/xkeen"; ln -s "@EXEC@" "$stage/xkeen"',
    'truncate -s 524289 "$stage/xkeen"',
    ': > "$stage/xkeen"',
  ]) {
    const r = fixture({ stageFlow: true, setup, child: 'native_update_bind_staged "$stage"' })
    assert.equal(r.status, 76, r.stderr)
    assert.equal(r.stagedPresent, false)
  }
})
test('inherited grandchild cannot publish staged evidence even with the correct path', () => {
  const r = fixture({ stageFlow: true, child: `/bin/sh -c '. "@ROOT@/native-operation-gate"; . "@ROOT@/native-admission-entry"; . "@ROOT@/native-update-context"; native_update_bind_staged "$stage"'` })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.stagedPresent, false)
})
test('post-publication context, body or dispatcher drift retains receipt and admission', () => {
  for (const change of [
    "printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/context'",
    "printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/body'",
    "printf '# changed\\n' >> \"$stage/xkeen\"",
  ]) {
    const r = fixture({ stageFlow: true, setup: `eval "$(sed 's/^_nu_recheck_call()/original_recheck_call()/' '@ROOT@/native-update-context')"
_nu_recheck_call() { [ ! -f '@ROOT@/operation.lock.d/call.update/staged' ] || { ${change}; }; original_recheck_call; }`, child: 'native_update_bind_staged "$stage"' })
    assert.equal(r.status, 77, r.stderr)
    assert.equal(r.stagedPresent && r.bodyPresent, true)
  }
})
test('internally valid ancestor owner replacement after publication cannot change generation', () => {
  const r = fixture({ stageFlow: true, setup: `eval "$(sed 's/^_nu_load_call()/original_load_call()/' '@ROOT@/native-update-context')"
_nu_load_call() {
  if [ -f '@ROOT@/operation.lock.d/call.update/staged' ]; then
    _native_gate_proc "$_nu_body_pid" || return 77
    printf 'v1 %s %s %s %s update-xkeen\\n' "$_ng_current_boot" "$_nu_body_pid" "$_ng_proc_start" "$XKEEN_GATE_TOKEN" > '@ROOT@/operation.lock.d/owner'
  fi
  original_load_call
}`, child: 'native_update_bind_staged "$stage"' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.stagedPresent && r.bodyPresent, true)
})
test('same body without exec and incorrect exec arguments cannot enter post-update', () => {
  for (const options of [{ withoutExec: true }, { execArgument: '-uk' }]) {
    const r = fixture({ execFlow: true, ...options }); assert.equal(r.status, 77, r.stderr)
  }
})
test('post-update phase is consumed once and cannot be replayed in the same body', () => {
  const r = fixture({ execFlow: true, execProbe: 'native_update_exec_context || exit $?; native_update_exec_context' })
  assert.equal(r.status, 77, r.stderr)
})
test('missing or unsafe staged receipt and changed dispatcher refuse the exec phase', () => {
  for (const stagedChange of [
    "rm '@ROOT@/operation.lock.d/call.update/staged'",
    "chmod 644 '@ROOT@/operation.lock.d/call.update/staged'",
    "printf '# changed\\n' >> '@EXEC@'",
    "mkdir '@ROOT@/operation.lock.d/call.update/exec.used'",
  ]) {
    const r = fixture({ execFlow: true, stagedChange }); assert.equal(r.status, 77, r.stderr)
  }
})
test('post-consumption drift refuses while retaining phase and owner evidence', () => {
  for (const change of [
    "printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/context'",
    "printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/body'",
    "printf 'changed\\n' > '@ROOT@/operation.lock.d/call.update/staged'",
    "printf '# changed\\n' >> '@EXEC@'",
  ]) {
    const r = fixture({ execFlow: true, execProbe: `eval "$(sed 's/^_nu_recheck_call()/original_recheck_call()/' '@ROOT@/native-update-context')"
_nu_recheck_call() { [ ! -d '@ROOT@/operation.lock.d/call.update/exec.used' ] || { ${change}; }; original_recheck_call; }
native_update_exec_context` })
    assert.equal(r.status, 77, r.stderr)
    assert.equal(r.bodyPresent && r.execUsed && r.execArgv, true)
  }
})
