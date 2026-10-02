import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

const entrySource = readFileSync('scripts/native-admission-entry.sh', 'utf8')
const gateSource = readFileSync('scripts/native-operation-gate.sh', 'utf8')
function fixture({ body = 'echo BODY >> "$evidence"', verifier = 'exit 0', setup = '', tail = '', action = 'start', mode = 'forced', role: entryRole = 'init', nested = false, nestedHook = false, hookBody = 'echo HOOK >> "$evidence"' } = {}) {
  const root = mkdtempSync(join(tmpdir(), 'native-entry-gate-'))
  chmodSync(root, 0o700)
  // Runtime executable paths must have protected ancestors; never execute a
  // full native dispatcher/init in these fixtures.
  const code = mkdtempSync('/opt/native-entry-fixture-')
  chmodSync(code, 0o700)
  const evidence = join(code, 'evidence')
  const paths = { gate: join(code, 'gate.sh'), entry: join(code, 'entry.sh'), init: join(code, 'init.sh'), dispatcher: join(code, 'dispatcher.sh'), verifier: join(code, 'verify.sh'), hook: join(code, 'hook.sh') }
  const put = (path, text) => writeFileSync(path, text, { mode: 0o600 })
  let source = entrySource.replaceAll('/tmp/.xkeen-admission', root)
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', paths.gate)
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', paths.verifier)
    .replaceAll('/opt/etc/init.d/S05xkeen', paths.init)
    .replaceAll('/opt/sbin/xkeen', paths.dispatcher)
    .replaceAll('/opt/etc/ndm/netfilter.d/proxy.sh', paths.hook)
    .replaceAll('/opt/bin/sh', '/bin/sh')
  put(paths.entry, source); put(paths.gate, gateSource)
  put(paths.verifier, '#!/bin/sh\n' + verifier.replaceAll('@ENTRY@', paths.entry).replaceAll('@GATE@', paths.gate) + '\n')
  for (const role of ['init', 'dispatcher']) put(paths[role], `#!/bin/sh
. '${paths.entry}'
evidence='${evidence}'
_fixture_mode=forced
if [ '${role}' = init ] && [ "$#" = 1 ] && [ "$1" != stop ]; then _fixture_mode=automatic; fi
native_admission_enter ${role} "\${1#-}" "$_fixture_mode" || exit $?
[ "$_na_body" = 1 ] || exit 0
${nested && role === 'dispatcher' ? `/bin/sh '${paths.init}' "\${1#-}" on || exit $?` : nestedHook ? `/bin/sh '${paths.hook}' || exit $?` : body}
native_admission_finish 0
exit $?
`)
  put(paths.hook, `#!/bin/sh
. '${paths.entry}'
evidence='${evidence}'
native_admission_hook_enter || exit $?
[ "$_na_body" = 1 ] || exit 0
${hookBody}
native_admission_finish 0
exit $?
`)
  try {
    const expand = text => text.replaceAll('@ROOT@', root).replaceAll('@GATE@', paths.gate).replaceAll('@VERIFY@', paths.verifier)
    const r = spawnSync('/bin/sh', ['-c', `${expand(setup)}\n/bin/sh '${paths[entryRole]}' '${action}' ${entryRole === 'init' && mode === 'forced' ? 'on' : ''}\nrc=$?\n${expand(tail)}\nexit "$rc"`], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(r.error)
    return { ...r, held: existsSync(join(root, 'operation.lock.d')), evidence: existsSync(evidence) ? readFileSync(evidence, 'utf8') : '' }
  } finally { rmSync(root, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}
test('owner runs fixed foreground native body and releases after verified completion', () => {
  const r = fixture()
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'BODY\n')
  assert.equal(r.held, false)
})
test('untrusted internal marker grants no body authority', () => {
  const r = fixture({ setup: 'export XKEEN_ADMISSION_ROLE=init XKEEN_ADMISSION_ACTION=start XKEEN_ADMISSION_CALL=fake' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.evidence, '')
})
test('premature successful child exit cannot settle owner', () => {
  const r = fixture({ body: 'exit 0' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
})
test('native EXIT trap cannot turn failed/missing finish into success', () => {
  const r = fixture({ body: "trap 'exit 0' EXIT; exit 9" })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
})
test('failed typed finish remains unresolved even if native EXIT trap hides status', () => {
  const r = fixture({ body: "trap 'exit 0' EXIT; native_admission_finish 1; exit 0" })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
})
test('failed native postcondition retains admission', () => {
  const r = fixture({ verifier: '[ "$1" != post ]' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
})
test('unsupported action rejects before acquiring or executing body', () => {
  const r = fixture({ action: 'update' })
  assert.equal(r.status, 76, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, false)
})
test('automatic boot mode stays bare in child and is bound into verifier input', () => {
  const r = fixture({ mode: 'automatic', body: '[ "$#" = 1 ] || exit 9; echo AUTOMATIC >> "$evidence"', verifier: '[ "$2:$3:$4" = init:start:automatic ]' })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'AUTOMATIC\n')
  assert.equal(r.held, false)
})
test('borrowed foreground invocation leaves release to existing owner', () => {
  const r = fixture({ setup: ". '@GATE@'; native_gate_acquire '@ROOT@' config-change || exit $?",
    tail: "[ -f '@ROOT@/operation.lock.d/owner' ] || exit 98; native_gate_release || exit $?" })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'BODY\n')
  assert.equal(r.held, false)
})
test('poisoned retained gate rejects subsequent native join and protocol release', () => {
  const r = fixture({ setup: ". '@GATE@'; native_gate_acquire '@ROOT@' config-change || exit $?; mkdir '@ROOT@/operation.lock.d/unresolved'",
    tail: 'native_gate_release; [ "$?" = 77 ] || exit 98' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})
test('incomplete crashed owner is busy and never reaped', () => {
  const r = fixture({ setup: "mkdir -m 700 '@ROOT@/operation.lock.d'" })
  assert.equal(r.status, 75, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})
test('another process cannot acquire a live owner and does not enter native body', () => {
  const r = fixture({ setup: ". '@GATE@'; native_gate_acquire '@ROOT@' config-change || exit $?; unset XKEEN_GATE_ROOT XKEEN_GATE_TOKEN" })
  assert.equal(r.status, 75, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})
test('dead owner remains busy without reaping', () => {
  const r = fixture({ setup: "(. '@GATE@'; native_gate_acquire '@ROOT@' start) || exit $?" })
  assert.equal(r.status, 75, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})
test('borrowed context cannot change a typed lifecycle action', () => {
  const r = fixture({ setup: ". '@GATE@'; native_gate_acquire '@ROOT@' stop || exit $?" })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})
test('background context strip removes gate and internal finish authority', () => {
  const r = fixture({ body: '(native_admission_strip; [ -z "${XKEEN_GATE_TOKEN-}${XKEEN_ADMISSION_CALL-}${_na_body-}${_ng_owned_record-}" ] || exit 9; echo STRIPPED >> "$evidence") || exit $?' })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'STRIPPED\n')
})
test('native child signal retains owner', () => {
  const r = fixture({ body: 'kill -TERM $$' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
})
test('dispatcher and init nesting share one owner until both finishes', () => {
  const r = fixture({ role: 'dispatcher', nested: true })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'BODY\n')
  assert.equal(r.held, false)
})

test('foreground init hook borrows forced or automatic operation until native readback', () => {
  for (const [action, mode] of [['start', 'forced'], ['restart', 'forced'], ['start', 'automatic']]) {
    const r = fixture({ action, mode, nestedHook: true, verifier: `[ "$2" = init ] || [ "$2:$3:$4" = hook:${action}:${mode} ]` })
    assert.equal(r.status, 0, r.stderr)
    assert.equal(r.evidence, 'HOOK\n')
    assert.equal(r.held, false)
  }
})

test('direct event hook is refused before a gate or native effects', () => {
  const r = fixture({ role: 'hook' })
  assert.notEqual(r.status, 0)
  assert.equal(r.evidence, '')
  assert.equal(r.held, false)
})

test('foreground hook failed or premature completion retains its parent operation', () => {
  for (const hookBody of ['exit 9', 'exit 0', "trap 'exit 0' EXIT; native_admission_finish 1; exit 0"]) {
    const r = fixture({ nestedHook: true, hookBody })
    assert.notEqual(r.status, 0)
    assert.equal(r.held, true)
  }
})

test('foreground hook exec-self preserves its strict child and one completion record', () => {
  const r = fixture({ nestedHook: true, hookBody: 'if [ -z "${ONCE-}" ]; then ONCE=1; export ONCE; exec /bin/sh "$0"; fi; echo HOOK >> "$evidence"' })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'HOOK\n')
  assert.equal(r.held, false)
})

test('dispatcher init and foreground hook share one admission owner', () => {
  const r = fixture({ role: 'dispatcher', nested: true, nestedHook: true })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'HOOK\n')
  assert.equal(r.held, false)
})

test('inherited hook hints do not authorize an extra child to enter or finish', () => {
  const r = fixture({ nestedHook: true, hookBody: '/bin/sh "$0" || exit $?' })
  assert.notEqual(r.status, 0)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})

test('missing verifier and refused preflight execute zero native body', () => {
  for (const options of [{ setup: 'rm @VERIFY@' }, { verifier: '[ "$1" != pre ]' }]) {
    const r = fixture(options)
    assert.notEqual(r.status, 0)
    assert.equal(r.evidence, '')
    assert.equal(r.held, true)
  }
})

const descendantProof = `. '@ENTRY@'; . '@GATE@'
_na_role=$2; _na_action=$3; _na_mode=$4
(
  _na_descendant_ok || exit $?
  # A descendant verifier cannot gain the immediate-child finish capability.
  _na_child_ok && exit 99
  exit 0
)`
test('read-only descendant proof accepts nested verifier without finish authority', () => {
  const r = fixture({ verifier: descendantProof })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'BODY\n')
  assert.equal(r.held, false)
})
test('descendant proof rejects mismatched context and a fake wrapper record', () => {
  for (const corrupt of [
    'XKEEN_ADMISSION_CALL=00000000000000000000000000000000',
    '_na_action=stop',
    `record=$(cat "$XKEEN_GATE_ROOT/operation.lock.d/call.init/context")
set -- $record
printf '%s %s %s %s %s %s %s %s\\n' "$1" 1 "$3" "$4" "$5" "$6" "$7" "$8" > "$XKEEN_GATE_ROOT/operation.lock.d/call.init/context"`,
  ]) {
    const r = fixture({ verifier: descendantProof.replace('(\n', `(\n${corrupt}\n`) })
    assert.notEqual(r.status, 0)
    assert.equal(r.evidence, '')
    assert.equal(r.held, true)
  }
})
