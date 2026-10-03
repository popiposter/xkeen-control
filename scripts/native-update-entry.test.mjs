// Fixed updater envelope with real process/gate/exec/terminal authentication.
// Native bodies, verifier postconditions and event worker are synthetic; this
// never runs a native installer, persistent writer or complete router updater.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

const sha = text => createHash('sha256').update(text).digest('hex')
function fixture({ argument = '-uk', before = '', initial = '', terminal = 'native_update_complete 0 || exit $?', post = '', preFail = false, postFail = false, borrowed = false, action = 'update-xkeen', dirty = false, drain = 0, cleanupDrift = '', cleanupBefore = '' } = {}) {
  const code = mkdtempSync('/root/native-update-entry-'), ram = mkdtempSync('/tmp/native-update-entry-')
  chmodSync(code, 0o700); chmodSync(ram, 0o700)
  const path = name => join(code, name)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', path('gate'))
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', path('entry'))
    .replaceAll('/opt/lib/xkeen/native-update-context.sh', path('context'))
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', path('verify'))
    .replaceAll('/opt/lib/xkeen/native-event-notification.sh', path('event'))
    .replaceAll('/opt/lib/xkeen/native-event-convergence.sh', path('converge'))
    .replaceAll('/opt/sbin/.xkeen.stage.', `${code}/.xkeen.stage.`).replaceAll('/opt/sbin/xkeen', path('xkeen'))
    .replaceAll('/opt/bin/sh', '/bin/sh')
    .replaceAll('f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe', sha(Buffer.from(`/bin/sh\0${path('xkeen')}\0-uk_post_update\0`)))
    .replaceAll('@CODE@', code).replaceAll('@RAM@', ram)
  const put = (name, text) => writeFileSync(path(name), rewrite(text), { mode: 0o600 })
  const libraries = ". '@CODE@/gate'; . '@CODE@/entry'; . '@CODE@/context'"
  for (const [name, file] of [['gate', 'native-operation-gate'], ['entry', 'native-admission-entry'], ['context', 'native-update-context']]) put(name, readFileSync(`scripts/${file}.sh`, 'utf8'))
  const queries = (phase) => phase === 'pre' ? `
mkdir -m 700 "$_nu_call/environment.pre" "$_nu_call/init-identity.pre" "$_nu_call/package-identity.pre"
printf 'fixture\\n' > "$_nu_call/environment.pre/packages"
for name in live-code live-settings template-code template-settings; do printf 'fixture\\n' > "$_nu_call/init-identity.pre/$name"; done
for name in other identity control; do printf 'fixture\\n' > "$_nu_call/package-identity.pre/$name"; done
printf 'fixture\\n' > "$_nu_call/update-preflight"
` : `
mkdir -m 700 "$_nu_call/init-identity.post" "$_nu_call/package-identity.post"
for name in live-code live-settings template-code template-settings; do printf 'fixture\\n' > "$_nu_call/init-identity.post/$name"; done
for name in other identity control; do printf 'fixture\\n' > "$_nu_call/package-identity.post/$name"; done
rm "$_nu_call/update-preflight"
${post}
`
  put('verify', `${libraries}
umask 077
native_update_verifier_context "$1" || exit $?
printf 'VERIFY-%s\\n' "$1" >> '@CODE@/effects'
case "$1" in
pre) ${preFail ? 'exit 8' : queries('pre')};;
post) ${postFail ? 'exit 9' : queries('post')};;
esac
`)
  put('event', `_ne_prepare() { _ne_root='@RAM@/events'; mkdir -m 700 -p "$_ne_root"; }`)
  put('converge', `native_event_converge() {
[ -z "\${XKEEN_GATE_ROOT-}\${XKEEN_GATE_TOKEN-}\${XKEEN_ADMISSION_ROLE-}\${XKEEN_ADMISSION_ACTION-}\${XKEEN_ADMISSION_CALL-}" ] || return 91
[ ! -e '@RAM@/operation.lock.d' ] || return 92
echo DRAIN-CURRENT >> '@CODE@/effects'
return ${drain}
}`)
  put('stage', `${libraries}
native_update_bind_staged "$stage"
`)
  put('xkeen', `${libraries}
${cleanupBefore ? `eval "$(sed 's/^_nu_cleanup()/original_cleanup()/' '@CODE@/context')"
_nu_cleanup() {
${cleanupBefore}
original_cleanup
}
` : ''}
${cleanupDrift ? `eval "$(sed 's/^_nu_cleanup_shape()/original_cleanup_shape()/' '@CODE@/context')"
_nu_cleanup_shape() { original_cleanup_shape || return $?; ${cleanupDrift}; }
` : ''}
native_update_enter "$1" || exit $?
[ "$_na_body" = 1 ] || exit 0
umask 077
case "$1" in
-uk)
echo BODY >> '@CODE@/effects'
${initial}
mkdir -m 700 "$_nu_call/packages.initial"
printf 'fixture\\n' > "$_nu_call/packages.initial/packages"
stage="/opt/sbin/.xkeen.stage.$_ng_self_pid"; export stage
mkdir -m 700 "$stage"; cp '@CODE@/xkeen' "$stage/xkeen"; chmod 600 "$stage/xkeen"
/bin/sh '@CODE@/stage' || exit $?
exec /bin/sh '@CODE@/xkeen' -uk_post_update
exit 95;;
-uk_post_update)
echo POST-BODY >> '@CODE@/effects'
mkdir -m 700 "$_nu_call/packages.post"
for name in packages argv.before argv.after; do printf 'fixture\\n' > "$_nu_call/packages.post/$name"; done
${terminal}
exit 0;;
esac
`)
  put('wrapper', `${libraries}
${before}
/bin/sh '@CODE@/xkeen' '${argument}'
`)
  put('owner', `${libraries}
native_gate_acquire '@RAM@' '${action}' || exit $?
/bin/sh '@CODE@/wrapper'
rc=$?
echo ANCESTOR >> '@CODE@/effects'
[ -f '@RAM@/operation.lock.d/owner' ] || exit 93
exit "$rc"
`)
  if (dirty) { put('prepare', `mkdir -m 700 -p '@RAM@/events/dirty'`); spawnSync('/bin/sh', [path('prepare')]) }
  try {
    const r = spawnSync('/bin/sh', [path(borrowed ? 'owner' : 'wrapper')], { encoding: 'utf8', timeout: 5000 })
    assert.ifError(r.error)
    const call = name => existsSync(join(ram, 'operation.lock.d/call.update', name))
    return { ...r, effects: existsSync(path('effects')) ? readFileSync(path('effects'), 'utf8').trim().split('\n') : [], held: existsSync(join(ram, 'operation.lock.d/owner')), context: call('context'), body: call('body'), completed: call('completed'), query: call('environment.pre/packages'), unresolved: existsSync(join(ram, 'operation.lock.d/unresolved')) }
  } finally { rmSync(ram, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('one real body survives exec, publishes terminal, independent postproof precedes fixed cleanup and owner release', () => {
  const r = fixture(); assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(r.effects, ['VERIFY-pre', 'BODY', 'POST-BODY', 'VERIFY-post'])
  assert.equal(r.held || r.context || r.query || r.completed, false)
})
test('raw post entry, unsupported argv and malformed inherited hints never create a body or verifier', () => {
  for (const options of [{ argument: '-uk_post_update' }, { argument: '-ux' }, { before: 'XKEEN_ADMISSION_ROLE=dispatcher; export XKEEN_ADMISSION_ROLE' }, { before: 'XKEEN_ADMISSION_CALL=spoof; export XKEEN_ADMISSION_CALL' }]) {
    const r = fixture(options); assert.notEqual(r.status, 0); assert.deepEqual(r.effects, []); assert.equal(r.body, false)
  }
})
test('preflight failure prevents body; failed body, zero without completed and failed postproof retain evidence and admission', () => {
  const pre = fixture({ preFail: true }); assert.equal(pre.status, 77); assert.deepEqual(pre.effects, ['VERIFY-pre']); assert.equal(pre.body, false); assert.equal(pre.held && pre.context && pre.unresolved, true)
  for (const options of [{ initial: 'exit 8' }, { terminal: ':' }, { postFail: true }]) {
    const r = fixture(options); assert.equal(r.status, 77, r.stderr); assert.equal(r.held && r.context && r.query && r.body && r.unresolved, true)
  }
})
test('unknown root/nested/hidden entries and unsafe known files retain ALL cleanup evidence', () => {
  for (const post of ['touch "$_nu_call/foreign"', 'touch "$_nu_call/.hidden"', 'touch "$_nu_call/environment.pre/.hidden"', 'touch "$_nu_call/exec.used/foreign"', 'ln -s absent "$_nu_call/packages.post/foreign"', 'chmod 666 "$_nu_call/package-identity.post/other"', 'rm "$_nu_call/packages.post/argv.after"', 'touch "$_nu_call/update-preflight"']) {
    const r = fixture({ post }); assert.equal(r.status, 77, `${post}: ${r.stderr}`); assert.equal(r.held && r.context && r.query && r.completed && r.unresolved, true)
  }
})
test('late whole-shape mutation refuses before any file removal', () => {
  const r = fixture({ cleanupDrift: 'touch "$_nu_call/.late"' }); assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held && r.context && r.query && r.completed, true)
})
test('foreign operation-root siblings retain all evidence for both own and borrowed updates', () => {
  for (const borrowed of [false, true]) {
    for (const post of ['touch "$_nu_call/../foreign"', 'touch "$_nu_call/../.hidden"', 'ln -s absent "$_nu_call/../foreign"', 'mkdir "$_nu_call/../call.init"', 'mkdir "$_nu_call/../call.hook"', 'mkdir "$_nu_call/../unresolved"']) {
      const r = fixture({ borrowed, post }); assert.equal(r.status, 77, `${borrowed}:${post}: ${r.stderr}`)
      assert.equal(r.held && r.context && r.query && r.completed, true)
    }
  }
})
test('borrowed owner with unknown or unresolved sibling state refuses before creating call.update or running preflight', () => {
  for (const before of ['touch "$XKEEN_GATE_ROOT/operation.lock.d/foreign"', 'mkdir "$XKEEN_GATE_ROOT/operation.lock.d/unresolved"', 'mkdir "$XKEEN_GATE_ROOT/operation.lock.d/call.init"']) {
    const r = fixture({ borrowed: true, before }); assert.equal(r.status, 77)
    assert.equal(r.context || r.body, false); assert.deepEqual(r.effects, ['ANCESTOR']); assert.equal(r.held, true)
  }
})
test('valid live ancestor owner replacement immediately before cleanup cannot create a new accepted generation', () => {
  for (const borrowed of [false, true]) {
    const r = fixture({ borrowed, cleanupBefore: `
set -- $_na_gate_identity
_native_gate_proc "$3" || return 94
replacement_pid=$_ng_parent
_native_gate_proc "$replacement_pid" || return 94
printf 'v1 %s %s %s %s update-xkeen\\n' "$2" "$replacement_pid" "$_ng_proc_start" "$5" > '@RAM@/operation.lock.d/owner'
` })
    assert.equal(r.status, 77, r.stderr); assert.equal(r.held && r.context && r.query && r.completed, true)
  }
})
test('ancestor update owner is borrowed, never released/drained; wrong action refuses before preflight', () => {
  const r = fixture({ borrowed: true, dirty: true }); assert.equal(r.status, 0, r.stderr)
  assert.equal(r.held, true); assert.equal(r.context, false); assert.equal(r.effects.includes('DRAIN-CURRENT'), false)
  const wrong = fixture({ borrowed: true, action: 'config-change' }); assert.equal(wrong.status, 77); assert.deepEqual(wrong.effects, ['ANCESTOR']); assert.equal(wrong.context, false)
})
test('real dirty completion drains only after update release, queued and unresolved drain results remain distinct', () => {
  for (const drain of [0, 75, 77]) {
    const r = fixture({ dirty: true, drain }); assert.equal(r.status, drain, r.stderr)
    assert.equal(r.effects.at(-1), 'DRAIN-CURRENT'); assert.equal(r.held || r.context, false)
  }
})
