// Real exec/gate/init/hook wrappers; synthetic lifecycle verifier and body.
// No native updater, kernel effect or router mutation is executed.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

const sha = value => createHash('sha256').update(value).digest('hex')
const canonical = sha(Buffer.from('/opt/bin/sh\0/opt/sbin/xkeen\0-uk_post_update\0'))
function fixture({ action = 'restart', mode = 'forced', nested = false, hook = false, beforeInit = '', initSetup = '', afterInit = '', bodySetup = '', realExec = true, verifier = ':', verifierSetup = '', initBody = ':' } = {}) {
  const ram = mkdtempSync('/tmp/native-update-init-'), code = mkdtempSync('/root/native-update-init-')
  chmodSync(ram, 0o700); chmodSync(code, 0o700)
  const path = name => join(code, name)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', path('gate'))
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', path('entry'))
    .replaceAll('/opt/lib/xkeen/native-update-context.sh', path('context'))
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', path('verify'))
    .replaceAll('/opt/etc/init.d/S05xkeen', path('init'))
    .replaceAll('/opt/etc/ndm/netfilter.d/proxy.sh', path('hook'))
    .replaceAll('/opt/sbin/.xkeen.stage.', `${code}/.xkeen.stage.`)
    .replaceAll('/opt/sbin/xkeen', path('xkeen')).replaceAll('/opt/bin/sh', '/bin/sh')
    .replaceAll(canonical, sha(Buffer.from(`/bin/sh\0${path('xkeen')}\0-uk_post_update\0`)))
    .replaceAll('@CODE@', code).replaceAll('@RAM@', ram)
  const put = (name, text) => writeFileSync(path(name), rewrite(text), { mode: 0o600 })
  const libraries = ". '@CODE@/gate'; . '@CODE@/entry'; . '@CODE@/context'"
  put('gate', readFileSync('scripts/native-operation-gate.sh', 'utf8'))
  put('entry', readFileSync('scripts/native-admission-entry.sh', 'utf8'))
  put('context', readFileSync('scripts/native-update-context.sh', 'utf8'))
  put('verify', `${libraries}
_na_role=$2; _na_action=$3; _na_mode=$4
_na_call_dir='@RAM@/operation.lock.d/call.'$_na_role
${verifierSetup}
_na_child_ok || exit $?
echo "$*" >> '@CODE@/verify-calls'
${verifier}
`)
  put('init', `${libraries}
${initSetup}
native_admission_enter init '$action' '$mode' || exit $?
[ "$_na_body" = 1 ] || exit 0
echo INIT >> '@CODE@/effects'
${initBody}
${hook ? "/bin/sh '@CODE@/hook' || exit $?" : ''}
native_admission_finish 0
exit $?
`.replaceAll('$action', action).replaceAll('$mode', mode))
  put('hook', `${libraries}
native_admission_hook_enter || exit $?
[ "$_na_body" = 1 ] || exit 0
echo HOOK >> '@CODE@/effects'
native_admission_finish 0
exit $?
`)
  const initInvoke = nested ? "/bin/sh -c '/bin/sh @CODE@/init restart on'" : "/bin/sh '@CODE@/init' restart on"
  const nativeEnd = `${beforeInit}
${initInvoke} || exit $?
${afterInit}
native_update_complete 0
exit $?
`
  put('xkeen', `${libraries}
native_update_exec_context || exit $?
${nativeEnd}`)
  put('stage', `${libraries}
native_update_bind_staged "$stage"
`)
  put('body', `${libraries}
native_update_bind_body || exit $?
stage="/opt/sbin/.xkeen.stage.$_ng_self_pid"; export stage
mkdir -m 700 "$stage"; cp '@CODE@/xkeen' "$stage/xkeen"; chmod 600 "$stage/xkeen"
/bin/sh '@CODE@/stage' || exit $?
${bodySetup}
${realExec ? "exec /bin/sh '@CODE@/xkeen' -uk_post_update" : `# Synthetic stored exec proof cannot substitute actual post-exec argv.
mkdir -m 700 '@RAM@/operation.lock.d/call.update/exec.used'
printf '/bin/%s\\000@CODE@/%s\\000-uk_post_update\\000' sh xkeen > '@RAM@/operation.lock.d/call.update/exec.argv'
chmod 600 '@RAM@/operation.lock.d/call.update/exec.argv'
${nativeEnd}`}
`)
  put('wrapper', `. '@CODE@/gate'
native_gate_acquire '@RAM@' update-xkeen || exit $?
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@RAM@/operation.lock.d/call.update'
printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@RAM@/operation.lock.d/call.update/context'
chmod 600 '@RAM@/operation.lock.d/call.update/context'
/bin/sh '@CODE@/body'
`)
  try {
    const r = spawnSync('/bin/sh', [path('wrapper')], { encoding: 'utf8', timeout: 5000 })
    assert.ifError(r.error)
    const read = name => existsSync(path(name)) ? readFileSync(path(name), 'utf8') : ''
    return { ...r, effects: read('effects'), calls: read('verify-calls'), held: existsSync(join(ram, 'operation.lock.d/owner')), completed: existsSync(join(ram, 'operation.lock.d/call.update/completed')), initCall: existsSync(join(ram, 'operation.lock.d/call.init')), argv: existsSync(join(ram, 'operation.lock.d/call.update/init-parent.argv')) }
  } finally { rmSync(ram, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('post-exec body directly borrows one forced init restart and its hook, never releases owner', () => {
  for (const hook of [false, true]) {
    const r = fixture({ hook }); assert.equal(r.status, 0, JSON.stringify(r)); assert.equal(r.held && r.completed && r.argv, true); assert.equal(r.initCall, false)
    assert.equal(r.effects, hook ? 'INIT\nHOOK\n' : 'INIT\n')
    assert.ok(r.calls.includes('pre init restart forced\n')); assert.ok(r.calls.includes('post init restart forced\n'))
    if (hook) assert.ok(r.calls.includes('post hook restart forced\n'))
  }
})
test('deeper child, start/stop and automatic restart cannot borrow update authority', () => {
  for (const options of [{ nested: true }, { action: 'start' }, { action: 'stop' }, { mode: 'automatic' }]) {
    const r = fixture(options); assert.notEqual(r.status, 0, JSON.stringify(r)); assert.equal(r.effects, ''); assert.equal(r.completed, false); assert.equal(r.held, true)
  }
})
test('stored phase spoof without exec, missing consumed proof and completed body refuse before init', () => {
  for (const options of [{ realExec: false },
    { beforeInit: "rm '@RAM@/operation.lock.d/call.update/exec.argv'" },
    { beforeInit: "sed -i 's/^v1 /v1  /' '@RAM@/operation.lock.d/call.update/body' '@RAM@/operation.lock.d/call.update/staged'" },
    { beforeInit: 'native_update_complete 0 || exit $?' },
  ]) {
    const r = fixture(options); assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.effects, ''); assert.equal(r.held, true)
  }
})
test('second init invocation and foreign prior parent proof cannot become a new restart', () => {
  const r = fixture({ afterInit: "/bin/sh '@CODE@/init' restart on || exit $?" })
  assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.effects, 'INIT\n'); assert.equal(r.completed, false); assert.equal(r.held && r.argv, true)
  for (const beforeInit of ["touch '@RAM@/operation.lock.d/call.update/init-parent.argv'",
    "ln -s /not-found '@RAM@/operation.lock.d/call.update/init-parent.argv'"]) {
    const bad = fixture({ beforeInit }); assert.equal(bad.status, 77); assert.equal(bad.effects, ''); assert.equal(bad.held, true)
  }
})
test('failed native init body or readback retains subordinate context and blocks update completion', () => {
  for (const options of [{ initBody: 'exit 3' }, { verifier: '[ "$1:$2" != post:init ]' }]) {
    const r = fixture(options); assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.effects, 'INIT\n')
    assert.equal(r.held && r.initCall, true); assert.equal(r.completed, false)
  }
})
test('owner replacement during actual parent argv capture refuses before init effects', () => {
  for (const change of [
    "command printf 'v1 changed\\n' >| '@RAM@/operation.lock.d/owner'",
    `IFS= read -r saved < '@RAM@/operation.lock.d/owner'
set -- $saved
boot=$2; owner=$3; token=$5; action=$6
_native_gate_proc "$owner" || return 90
ancestor=$_ng_parent
_native_gate_proc "$ancestor" || return 91
command printf 'v1 %s %s %s %s %s\\n' "$boot" "$ancestor" "$_ng_proc_start" "$token" "$action" >| '@RAM@/operation.lock.d/owner'`,
  ]) {
    const r = fixture({ initSetup: `dd() {
command dd "$@" || return $?
${change}
}` })
    assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.effects, ''); assert.equal(r.held && r.argv, true); assert.equal(r.completed, false)
  }
})

test('parent init context or protection changing during ancestry proof refuses before effects', () => {
  for (const change of [
    "printf 'v1 changed\\n' > '@RAM@/operation.lock.d/call.init/context'",
    "chmod 755 '@RAM@/operation.lock.d/call.init'",
  ]) {
    const r = fixture({ verifierSetup: `
eval "$(sed -n '/^_nu_observer_chain() {/,/^}/p' '@CODE@/context' | sed '1s/_nu_observer_chain/_original_observer_chain/')"
_nu_observer_chain() {
  if [ "$_nu_init_hint_role" = init ]; then
    ${change}
  fi
  _original_observer_chain
}
native_update_init_observer_context || exit $?` })
    assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.effects, '')
    assert.equal(r.held && r.initCall, true); assert.equal(r.completed, false)
  }
})
