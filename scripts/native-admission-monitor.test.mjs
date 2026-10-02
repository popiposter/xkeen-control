// Source-only monitor fixtures: real RAM gate/entry, synthetic native restart and verifier.
import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const source = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) }).init.toString()
const begin = source.indexOf('\n_native_monitor_sample() {\n') >= 0 ? source.indexOf('\n_native_monitor_sample() {\n') : source.indexOf('\nmonitor_fd() {\n')
const end = source.indexOf('\nload_ipset() {\n', begin)
assert.ok(begin > 0 && end > begin)
const monitor = source.slice(begin, end)
function fixture(mode = 'success') {
  const root = mkdtempSync(join(tmpdir(), 'native-monitor-gate-'))
  const code = mkdtempSync('/opt/native-monitor-fixture-')
  chmodSync(root, 0o700); chmodSync(code, 0o700)
  const marker = join(code, 'monitor.pid'), evidence = join(code, 'evidence')
  const paths = { gate: join(code, 'gate.sh'), entry: join(code, 'entry.sh'), init: join(code, 'init.sh'), verify: join(code, 'verify.sh') }
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', root).replaceAll('/opt/lib/xkeen/native-operation-gate.sh', paths.gate)
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', paths.entry).replaceAll('/opt/lib/xkeen/native-admission-verify.sh', paths.verify)
    .replaceAll('/opt/etc/init.d/S05xkeen', paths.init).replaceAll('/opt/bin/sh', '/bin/sh')
  const put = (path, text) => writeFileSync(path, rewrite(text), { mode: 0o600 })
  const gate = readFileSync('scripts/native-operation-gate.sh', 'utf8')
  put(paths.gate, gate); put(paths.entry, readFileSync('scripts/native-admission-entry.sh', 'utf8'))
  put(paths.verify, `#!/bin/sh\ncase "$1" in pre) exit 0;; post) exit ${mode === 'verify-failure' ? 1 : 0};; *) exit 76;; esac\n`)
  put(paths.init, `#!/bin/sh
. '${paths.entry}'
native_admission_enter init "$1" forced || exit $?
[ "$_na_body" = 1 ] || exit 0
printf 'RESTART:%s:%s:%s\\n' "$1" "$2" "$fd_out" >> '${evidence}'
${mode === 'child-failure' ? 'exit 7' : ':'}
${mode === 'cancel-owner' ? `set -- $(cat '${root}/operation.lock.d/owner'); kill -TERM "$3"; sleep 0.05; exit 7` : ':'}
printf '987654\\n' > '${marker}'
native_admission_finish 0
exit $?
`)
  try {
    if (mode === 'busy') {
      const claim = spawnSync('/bin/sh', ['-c', `. '${paths.gate}'; native_gate_acquire '${root}' start`])
      assert.equal(claim.status, 0)
    }
    const r = spawnSync('/bin/sh', ['-c', rewrite(`
. '${paths.entry}'
${gate.replace('native_gate_acquire() {', 'fixture_acquire() {').replace('_native_gate_proc() {', 'fixture_proc() {')}
_fixture_claimed=0; core_pid=$PPID
native_gate_acquire() { fixture_acquire "$@"; rc=$?; [ "$rc" != 0 ] || _fixture_claimed=1; return "$rc"; }
_native_gate_proc() { fixture_proc "$1" || return $?; if [ '${mode}' = reused ] && [ "$_fixture_claimed" = 1 ] && [ "$1" = "$core_pid" ]; then _ng_proc_start=$((_ng_proc_start+1)); fi; }
${monitor}
native_admission_strip
_native_gate_self || exit 98
printf '%s\\n' "$_ng_self_pid" > '${marker}'
${mode === 'replacement' ? `printf '999999\\n' > '${marker}'` : ':'}
file_pid_fd='${marker}'; name_client=xray; delay_fd=1
pidof() { if [ '${mode}' = changed ] && [ "$_fixture_claimed" = 1 ]; then echo $$; else echo "$core_pid"; fi; }
awk() { case "$1" in '/Max open files/'*) if [ '${mode}' = cleared ] && [ "$_fixture_claimed" = 1 ]; then echo 100000; else echo 1; fi;; *) /usr/bin/awk "$@";; esac; }
sleep() { echo POLL >> '${evidence}'; exit 0; }
log_warning_router() { echo LOG >> '${evidence}'; }
proxy_stop() { echo UNOWNED_STOP >> '${evidence}'; }
proxy_start() { echo UNOWNED_START >> '${evidence}'; }
monitor_fd
`)], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(r.error)
    const nextAdmission = mode === 'cancel-owner' ? spawnSync('/bin/sh', ['-c', `. '${paths.gate}'; native_gate_acquire '${root}' restart`]).status : null
    return { ...r, nextAdmission, held: existsSync(join(root, 'operation.lock.d')), marker: existsSync(marker) ? readFileSync(marker, 'utf8').trim() : '', evidence: existsSync(evidence) ? readFileSync(evidence, 'utf8') : '' }
  } finally { rmSync(root, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}
test('monitor fresh owner joins fixed init restart with fd_out and leaves replacement marker', () => {
  const r = fixture()
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.held, false)
  assert.equal(r.marker, '987654')
  assert.equal(r.evidence, 'RESTART:restart:on:true\n')
})
test('busy gate uses native polling cadence without marker or lifecycle mutation', () => {
  const r = fixture('busy')
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.held, true)
  assert.match(r.marker, /^[1-9][0-9]*$/)
  assert.equal(r.evidence, 'POLL\n')
})
for (const mode of ['changed', 'reused', 'cleared']) test(`monitor rechecks ${mode} core evidence after admission`, () => {
  const r = fixture(mode)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.held, false)
  assert.equal(r.evidence, 'POLL\n')
})
test('old monitor never removes a replacement marker', () => {
  const r = fixture('replacement')
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, false)
  assert.equal(r.marker, '999999')
  assert.equal(r.evidence, '')
})
for (const mode of ['child-failure', 'verify-failure']) test(`monitor ${mode} retains admission and never retries native lifecycle`, () => {
  const r = fixture(mode)
  assert.equal(r.status, 77, r.stderr)
  assert.equal(r.held, true)
  assert.equal(r.evidence, 'RESTART:restart:on:true\n')
  assert.equal(r.marker, mode === 'verify-failure' ? '987654' : '')
})
test('monitor owner cancellation during active child retains gate and blocks replay', () => {
  const r = fixture('cancel-owner')
  assert.equal(r.signal, 'SIGTERM', r.stderr)
  assert.equal(r.held, true)
  assert.equal(r.nextAdmission, 75)
  assert.equal(r.evidence, 'RESTART:restart:on:true\n')
  assert.equal(r.marker, '')
})
