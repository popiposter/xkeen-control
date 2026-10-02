// Offline Linux fixture: execute only the extracted upstream proxy_stop function.
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { test } from 'node:test'

const upstream = process.env.XKEEN_STOP_FIX_SOURCE
if (!upstream) throw new Error('XKEEN_STOP_FIX_SOURCE must name the pinned public template')
const original = readFileSync(upstream)
assert.equal(createHash('sha256').update(original).digest('hex'), 'fbdba1f1cca6e1923e0c43113b4b6fafc51f92248cad70818f7ac92c937e32e3')
const red = process.env.XKEEN_STOP_FIX_TEST_UPSTREAM === '1'
const module = red ? null : await import('./native-xkeen-stop-fix.mjs')
const candidate = red ? original : module.patchSource(original)
const text = candidate.toString('utf8')
const start = text.indexOf('\nproxy_stop() {\n')
const end = text.indexOf('\n}\n', start) + 3
assert.ok(start > 0 && end > start)
const nativeStop = text.slice(start, end)

function run({ running = 0, mutex = 0, repeats = 1 } = {}) {
  // No upstream top-level code executes. Every external command used by the
  // extracted function is stubbed; firewall state is purely shell variables.
  const script = `
running=${running}; owned=1; unrelated=1; cleanup=0; releases=0; kills=0; emergencies=0
xkeen_rundir=/synthetic; start_attempts=2
_acquire_proxy_mutex() { return ${mutex}; }
_release_proxy_mutex() { releases=$((releases+1)); }
proxy_status() { [ "$running" = 1 ]; }
cleanup_fd_monitor() { :; }
clean_firewall() { owned=0; cleanup=$((cleanup+1)); }
enable_killswitch() { emergencies=$((emergencies+1)); }
_release_coldstart_guard() { :; }
log_info_router() { :; }; log_warning_terminal() { :; }; log_error_terminal() { :; }
rm() { :; }; sleep() { :; }; usleep() { :; }
killall() { running=0; kills=$((kills+1)); }
pidof() { [ "$running" = 1 ]; }
${nativeStop}
i=0; result=0
while [ "$i" -lt ${repeats} ]; do proxy_stop >/dev/null; result=$?; i=$((i+1)); done
printf '%s %s %s %s %s %s %s %s' "$result" "$running" "$owned" "$unrelated" "$cleanup" "$releases" "$kills" "$emergencies"
`
  const result = spawnSync('/bin/sh', ['-c', script], { encoding: 'utf8', timeout: 2000 })
  assert.equal(result.status, 0, result.error?.message || result.stderr)
  return result.stdout.split(' ').map(Number)
}

test('already stopped core clears stale native rules without emergency killswitch', () => {
  assert.deepEqual(run(), [0, 0, 0, 1, 1, 1, 0, 0])
})
test('repeated Stop remains safe and cleans native rules each time', () => {
  assert.deepEqual(run({ repeats: 2 }), [0, 0, 0, 1, 2, 2, 0, 0])
})
test('running core retains native cleanup-before-stop behavior', () => {
  assert.deepEqual(run({ running: 1 }), [0, 0, 0, 1, 1, 1, 1, 0])
})
test('contended mutex refuses cleanup and stop', () => {
  assert.deepEqual(run({ mutex: 1 }), [1, 0, 1, 1, 0, 0, 0, 0])
})
test('reentrant owner cleans without releasing the outer mutex', () => {
  assert.deepEqual(run({ mutex: 2 }), [0, 0, 0, 1, 1, 0, 0, 0])
})
test('patch rejects source drift and repeated application', { skip: red }, () => {
  assert.throws(() => module.patchSource(Buffer.concat([original, Buffer.from('\n')])), /source identity/)
  assert.throws(() => module.patchSource(candidate), /source identity/)
  assert.equal(text.replace('        clean_firewall\n        cleanup_fd_monitor\n    else', '        cleanup_fd_monitor\n    else'), original.toString('utf8'))
})
