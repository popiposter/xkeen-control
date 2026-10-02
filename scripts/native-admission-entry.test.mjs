import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

const entrySource = readFileSync('scripts/native-admission-entry.sh', 'utf8')
const gateSource = readFileSync('scripts/native-operation-gate.sh', 'utf8')
function fixture({ body = 'echo BODY >> "$evidence"', verifier = 'exit 0', setup = '', tail = '', action = 'start', mode = 'forced', role: entryRole = 'init', nested = false } = {}) {
  const root = mkdtempSync(join(tmpdir(), 'native-entry-gate-'))
  chmodSync(root, 0o700)
  // Runtime executable paths must have protected ancestors; never execute a
  // full native dispatcher/init in these fixtures.
  const code = mkdtempSync('/opt/native-entry-fixture-')
  chmodSync(code, 0o700)
  const evidence = join(code, 'evidence')
  const paths = { gate: join(code, 'gate.sh'), entry: join(code, 'entry.sh'), init: join(code, 'init.sh'), dispatcher: join(code, 'dispatcher.sh'), verifier: join(code, 'verify.sh') }
  const put = (path, text) => writeFileSync(path, text, { mode: 0o600 })
  let source = entrySource.replaceAll('/tmp/.xkeen-admission', root)
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', paths.gate)
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', paths.verifier)
    .replaceAll('/opt/etc/init.d/S05xkeen', paths.init)
    .replaceAll('/opt/sbin/xkeen', paths.dispatcher)
    .replaceAll('/opt/bin/sh', '/bin/sh')
  put(paths.entry, source); put(paths.gate, gateSource)
  put(paths.verifier, '#!/bin/sh\n' + verifier + '\n')
  for (const role of ['init', 'dispatcher']) put(paths[role], `#!/bin/sh
. '${paths.entry}'
evidence='${evidence}'
native_admission_enter ${role} "\${1#-}" '${mode}' || exit $?
[ "$_na_body" = 1 ] || exit 0
${nested && role === 'dispatcher' ? `/bin/sh '${paths.init}' "\${1#-}" on || exit $?` : body}
native_admission_finish 0
exit $?
`)
  try {
    const expand = text => text.replaceAll('@ROOT@', root).replaceAll('@GATE@', paths.gate)
    const r = spawnSync('/bin/sh', ['-c', `${expand(setup)}\n/bin/sh '${paths[entryRole]}' '${action}'\nrc=$?\n${expand(tail)}\nexit "$rc"`], { encoding: 'utf8', timeout: 3000 })
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
  const r = fixture({ verifier: 'exit 1' })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
})
test('unsupported action rejects before acquiring or executing body', () => {
  const r = fixture({ action: 'update' })
  assert.equal(r.status, 76, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, false)
})
test('automatic boot mode is explicitly unsupported before body or admission', () => {
  const r = fixture({ mode: 'automatic' })
  assert.equal(r.status, 76, r.stderr)
  assert.equal(r.evidence, '')
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
