// Execute extracted candidate entry/command-manager blocks only, never native top-level code.
import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const originalInit = readFileSync(process.env.XKEEN_ADMISSION_INIT)
const originalDispatcher = readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER)
const candidate = buildCandidates({ init: originalInit, dispatcher: originalDispatcher })
const init = candidate.init.toString()
const dispatcher = candidate.dispatcher.toString()
function prelude(text) {
  const begin = text.indexOf('# BEGIN NATIVE ADMISSION ENTRY\n')
  const end = text.indexOf('# END NATIVE ADMISSION ENTRY\n') + '# END NATIVE ADMISSION ENTRY\n'.length
  assert.ok(begin > 0 && end > begin)
  return text.slice(begin, end)
}
function fixture({ role = 'init', args = ['start'], automatic = 'on', startRC = 0, busy = false, rewriteChild = false, premature = false } = {}) {
  const code = mkdtempSync('/opt/native-connected-fixture-')
  const root = mkdtempSync(join(tmpdir(), 'native-connected-gate-'))
  chmodSync(code, 0o700); chmodSync(root, 0o700)
  const evidence = join(code, 'evidence')
  const paths = { entry: join(code, 'entry.sh'), gate: join(code, 'gate.sh'), init: join(code, 'init.sh'), dispatcher: join(code, 'dispatcher.sh'), verifier: join(code, 'verify.sh') }
  function substitute(text) {
    return text.replaceAll('/tmp/.xkeen-admission', root)
      .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', paths.entry)
      .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', paths.gate)
      .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', paths.verifier)
      .replaceAll('/opt/etc/init.d/S05xkeen', paths.init)
      .replaceAll('/opt/sbin/xkeen', paths.dispatcher).replaceAll('/opt/bin/sh', '/bin/sh')
  }
  const put = (path, text) => writeFileSync(path, substitute(text), { mode: 0o700 })
  put(paths.entry, readFileSync('scripts/native-admission-entry.sh', 'utf8'))
  put(paths.gate, readFileSync('scripts/native-operation-gate.sh', 'utf8'))
  put(paths.verifier, `#!/bin/sh\nprintf 'VERIFY:%s:%s:%s\\n' "$1" "$2" "$3" >> '${evidence}'\n`)
  const nativeManager = init.slice(init.indexOf('\n_cmd_rc=0\n'))
  put(paths.init, `#!/bin/sh
${rewriteChild ? 'if [ "${XKEEN_ADMISSION_ROLE-}" = init ]; then set -- start on; fi' : ''}
${prelude(init)}
evidence='${evidence}'
start_auto=${automatic}; init_delay=0
ipset() { :; }; log_info_router() { :; }; sleep() { :; }
_acquire_coldstart_guard() { return 0; }; _set_coldstart_pid() { :; }
wait_for_ready() { echo READY >> "$evidence"; }
proxy_start() { printf 'START:%s\\n' "$1" >> "$evidence"; ${premature ? 'exit 0' : `return ${startRC}`}; }
proxy_stop() { echo STOP >> "$evidence"; }
${nativeManager}
`)
  const caseBegin = dispatcher.indexOf('        -start)    #')
  const caseEnd = dispatcher.indexOf('        -status)    #', caseBegin)
  assert.ok(caseBegin > 0 && caseEnd > caseBegin)
  put(paths.dispatcher, `#!/bin/sh
${prelude(dispatcher)}
xkeen_rc=0; initd_file='${paths.init}'
smart_clear() { :; }; add_chmod_init() { :; }
case "$1" in
${dispatcher.slice(caseBegin, caseEnd)}
esac
native_admission_finish "$xkeen_rc"
exit $?
`)
  try {
    if (busy) {
      const claim = spawnSync('/bin/sh', ['-c', `. '${paths.gate}'; native_gate_acquire '${root}' start`], { encoding: 'utf8' })
      assert.equal(claim.status, 0, claim.stderr)
    }
    const r = spawnSync('/bin/sh', [paths[role], ...args], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(r.error)
    return { ...r, evidence: existsSync(evidence) ? readFileSync(evidence, 'utf8') : '', held: existsSync(join(root, 'operation.lock.d')) }
  } finally { rmSync(root, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}
test('connected automatic-disabled native manager finishes without startup', () => {
  const r = fixture({ automatic: 'off' })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'VERIFY:init:start:automatic\n')
  assert.equal(r.held, false)
})
test('connected bare Start joins native cold-start path without forced argument', () => {
  const r = fixture()
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'READY\nSTART:\nVERIFY:init:start:automatic\n')
  assert.equal(r.held, false)
})
test('connected forced Start preserves on and bypasses automatic delay path', () => {
  const r = fixture({ args: ['start', 'on'] })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'START:on\nVERIFY:init:start:forced\n')
})
test('connected cold-start failure retains gate without postcondition success', () => {
  const r = fixture({ startRC: 9 })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.evidence, 'READY\nSTART:\n')
  assert.equal(r.held, true)
})
test('premature native success exit bypassing manager finish retains gate', () => {
  const r = fixture({ premature: true })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.evidence, 'READY\nSTART:\n')
  assert.equal(r.held, true)
})
test('bare init Restart preserves automatic argument through its native case', () => {
  const r = fixture({ args: ['restart'] })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'STOP\nSTART:\nVERIFY:init:restart:automatic\n')
  assert.equal(r.held, false)
})
test('actual dispatcher lifecycle case nests init inside admitted foreground call', () => {
  const r = fixture({ role: 'dispatcher', args: ['-restart'] })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.evidence, 'STOP\nSTART:on\nVERIFY:init:restart:forced\nVERIFY:dispatcher:restart:forced\n')
  assert.equal(r.held, false)
})
test('mode substitution cannot reuse a valid automatic child proof', () => {
  const r = fixture({ rewriteChild: true })
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.evidence, '')
  assert.equal(r.held, true)
})
test('early typed admission refuses contention and unsupported inputs before native manager', () => {
  const r = fixture({ busy: true })
  assert.equal(r.status, 75, r.stderr)
  assert.equal(r.evidence, '')
  for (const [role, args] of [['init', ['cold_start']], ['init', ['start', 'off']], ['dispatcher', ['-uk']], ['dispatcher', ['-start', '-stop']]]) {
    const rejected = fixture({ role, args })
    assert.equal(rejected.status, 76, rejected.stderr)
    assert.equal(rejected.evidence, '')
    assert.equal(rejected.held, false)
  }
})
test('entry precedes pinned mutators while unconditional fence precedes entry', () => {
  for (const [text, firstEffect] of [[dispatcher, 'script_dir="$(cd'], [init, '# Информация о службе']]) {
    assert.ok(text.indexOf('# END SOURCE-ONLY FENCE') < text.indexOf('# BEGIN NATIVE ADMISSION ENTRY'))
    assert.ok(text.indexOf('# END NATIVE ADMISSION ENTRY') < text.indexOf(firstEffect))
  }
})
