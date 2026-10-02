// Offline native-source fixtures. No complete upstream script is executed.
import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

const init = readFileSync(process.env.XKEEN_ADMISSION_INIT)
const dispatcher = readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER)
const red = process.env.XKEEN_ADMISSION_UPSTREAM === '1'
const builder = red ? null : await import('./native-admission-patch.mjs')
const result = red ? { init, dispatcher } : builder.buildCandidates({ init, dispatcher })
const source = result.init.toString()
function run(body, args = []) {
  const r = spawnSync('/bin/sh', ['-c', body, 'fixture', ...args], { encoding: 'utf8', timeout: 2000 })
  assert.ifError(r.error)
  return r
}
const manager = source.slice(source.indexOf('\n_cmd_rc=0\n'))
const setup = `
native_admission_finish() { return "$1"; }
ipset() { :; }; log_info_router() { :; }; sleep() { :; }
_acquire_coldstart_guard() { return 0; }
_set_coldstart_pid() { :; }
wait_for_ready() { return "\${READY_RC:-0}"; }
proxy_start() { echo START; return "\${START_RC:-0}"; }
proxy_stop() { echo STOP; return "\${STOP_RC:-0}"; }
start_auto=on; init_delay=0
nohup() { echo DETACHED; }
`
test('native Restart does not start after failed Stop', () => {
  const r = run(setup + '\nSTOP_RC=1\n' + manager, ['restart', 'on'])
  assert.equal(r.status, 1)
  assert.equal(r.stdout.trim(), 'STOP')
})
test('native Restart preserves successful Stop then Start', () => {
  const r = run(setup + manager, ['restart', 'on'])
  assert.equal(r.status, 0)
  assert.equal(r.stdout.trim(), 'STOP\nSTART')
})
test('classified lifecycle dispatcher prefix never runs installer rename or package self-heal', () => {
  const dispatched = result.dispatcher.toString()
  const begin = dispatched.indexOf('# Определение директории')
  const end = dispatched.indexOf('# -toff действует', begin)
  assert.ok(begin > 0 && end > begin)
  const prefix = dispatched.slice(begin, end).replace('. "$script_dir/.xkeen/import.sh"', ': # synthetic import')
  for (const action of ['-start', '-stop', '-restart']) {
    const r = run(`
XKEEN_FOREGROUND=1
rm() { echo RENAME; }; mv() { echo RENAME; }
_load_packages_info() { echo PACKAGES; }; _ensure_installed_packages() { echo PACKAGES; }
${prefix}
echo DONE
`, [action])
    assert.equal(r.status, 0, r.stderr)
    assert.equal(r.stdout.trim(), 'DONE')
  }
})
function runNativeStop({ action = 'restart', survives = 1, mutex = 0, running = 1 } = {}) {
  const begin = source.indexOf('\nproxy_stop() {\n')
  const end = source.indexOf('\n}\n', begin) + 3
  assert.ok(begin > 0 && end > begin)
  return run(`
running=${running}; survives=${survives}; starts=0; cleaned=0; released=0; kills=0
start_attempts=2; xkeen_rundir=/synthetic; name_client=synthetic
_acquire_proxy_mutex() { return ${mutex}; }
_release_proxy_mutex() { released=$((released+1)); }
proxy_status() { [ "$running" = 1 ]; }
pidof() { [ "$running" = 1 ]; }
killall() { kills=$((kills+1)); [ "$survives" = 1 ] || running=0; }
clean_firewall() { cleaned=$((cleaned+1)); }
proxy_start() { starts=$((starts+1)); }
cleanup_fd_monitor() { :; }; _release_coldstart_guard() { :; }
log_info_router() { :; }; log_error_terminal() { :; }; log_warning_terminal() { :; }
rm() { :; }; sleep() { :; }; usleep() { :; }
trap 'printf "RESULT %s %s %s %s\\n" "$starts" "$cleaned" "$released" "$kills"' EXIT
${source.slice(begin, end)}
native_admission_finish() { return "$1"; }
${manager}
`, [action, 'on'])
}
test('actual native Stop exhaustion fails and Restart never invokes Start', () => {
  for (const action of ['stop', 'restart']) {
    for (const mutex of [0, 2]) {
      const r = runNativeStop({ action, mutex })
      assert.equal(r.status, 1, r.stdout)
      assert.ok(r.stdout.endsWith(`RESULT 0 2 ${mutex === 0 ? 1 : 0} 4\n`), r.stdout)
    }
  }
})
test('actual native Stop success and already-absent cleanup retain native behavior', () => {
  const stopped = runNativeStop({ survives: 0 })
  assert.equal(stopped.status, 0, stopped.stdout)
  assert.ok(stopped.stdout.endsWith('RESULT 1 1 1 1\n'), stopped.stdout)
  const absent = runNativeStop({ action: 'stop', running: 0 })
  assert.equal(absent.status, 0, absent.stdout)
  assert.ok(absent.stdout.endsWith('RESULT 0 1 1 0\n'), absent.stdout)
})
test('actual native Start exhaustion fails after cleanup; disabled autostart stays no-op success', () => {
  const begin = source.indexOf('\nproxy_start() {\n')
  const end = source.indexOf('\n}\n', begin) + 3
  assert.ok(begin > 0 && end > begin)
  const stubs = ['_invalidate_inbounds_cache', '_invalidate_mihomo_config_cache', 'apply_ipv6_state',
    'get_ipver_support', 'info_health_binary', 'validate_xkeen_json', 'check_policy_name_conflict',
    'check_xray_backups', 'api_cache_init', 'get_policy_mark', 'resolve_user_policies',
    'validate_entware_proxy_mark', 'validate_pbr_routing_mark', 'log_clean', 'sync_deny_mac_ipset',
    'process_user_ports', 'process_mark_var', 'detect_architecture', 'get_port_redirect',
    'get_network_redirect', 'get_port_tproxy', 'get_network_tproxy', 'log_warning_router',
    'log_info_router', 'log_error_terminal', 'ulimit', 'usleep', '_release_coldstart_guard']
    .map(name => `${name}() { :; }`).join('\n')
  for (const mutex of [0, 2]) {
    for (const enabled of [true, false]) {
      const r = run(`
${stubs}
_acquire_proxy_mutex() { return ${mutex}; }
_release_proxy_mutex() { released=$((released+1)); }
get_mode_proxy() { echo Other; }; proxy_status() { return 1; }
clean_firewall() { cleaned=$((cleaned+1)); }; enable_killswitch() { emergency=$((emergency+1)); }
cleaned=0; released=0; emergency=0; start_attempts=1; start_auto=off; name_client=synthetic
xkeen_cfg=/synthetic
${source.slice(begin, end)}
proxy_start '${enabled ? 'on' : ''}' >/dev/null; rc=$?
printf '%s %s %s %s' "$rc" "$cleaned" "$released" "$emergency"
`)
      assert.equal(r.status, 0, r.stderr)
      assert.equal(r.stdout, `${enabled ? 1 : 0} 1 ${mutex === 0 ? 1 : 0} ${enabled ? 1 : 0}`)
    }
  }
})
test('automatic start joins cold startup and reports startup failure', () => {
  const r = run(setup + '\nSTART_RC=9\n' + manager, ['start'])
  assert.equal(r.status, 9)
  assert.equal(r.stdout.trim(), 'START')
})
test('automatic-disabled native command manager reaches typed finish without Start', () => {
  const r = run(setup + '\nstart_auto=off\nnative_admission_finish() { echo FINISH:$1; return "$1"; }\n' + manager, ['start'])
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout.trim(), 'FINISH:0')
})
test('contended native cold-start guard reaches failed finish', () => {
  const r = run(setup + '\n_acquire_coldstart_guard() { return 1; }\nnative_admission_finish() { echo FINISH:$1; return "$1"; }\n' + manager, ['start'])
  assert.equal(r.status, 75, r.stderr)
  assert.equal(r.stdout.trim(), 'FINISH:75')
})
test('cold startup refuses Start when readiness fails', () => {
  const r = run(setup + '\nREADY_RC=8\n' + manager, ['cold_start'])
  assert.equal(r.status, 8)
  assert.equal(r.stdout.trim(), '')
})
test('initial crash uses same-shell emergency cleanup and returns failure', () => {
  // Extract the actual native crash-check block, including its delimiters.
  const comment = source.indexOf('                        # Даём ядру прокси')
  const begin = source.lastIndexOf('\n', source.lastIndexOf('\n', comment) - 1) + 1
  const end = source.indexOf('                    if [ -n "$api_policy_json" ]; then', comment)
  assert.ok(comment > 0 && end > comment)
  const block = source.slice(begin, end)
  const emergencyBegin = source.indexOf('\nemergency_clear() {\n')
  const emergencyEnd = source.indexOf('\n}\n', emergencyBegin) + 3
  assert.ok(emergencyBegin > 0 && emergencyEnd > emergencyBegin)
  const emergency = source.slice(emergencyBegin, emergencyEnd)
  const r = run(`
pidof() { echo 123; }; sleep() { :; }; kill() { return 1; }; proxy_status() { return 1; }
log_error_router() { :; }; _release_proxy_mutex() { released=1; }
_acquire_proxy_mutex() { return 2; }; rm() { :; }; _release_coldstart_guard() { :; }
cleanup_fd_monitor() { :; }; clean_firewall() { cleaned=1; }; enable_killswitch() { :; }
${emergency}
cleaned=0; released=0; _ps_mutex_rc=0
native_start_segment() {
${block}
return 0
}
native_start_segment >/dev/null; rc=$?
printf '%s %s %s' "$rc" "$cleaned" "$released"
`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout, '1 1 1')
})
test('candidates are unconditionally disabled before any upstream code', { skip: red }, () => {
  for (const bytes of [result.init, result.dispatcher]) {
    const prefix = bytes.toString().split('# END SOURCE-ONLY FENCE')[0]
    for (const args of [['start'], ['-start'], ['-uk'], ['-sbt'], ['cold_start'], ['status']]) {
      const r = run(prefix + '\necho SIDE_EFFECT\n', args)
      assert.equal(r.status, 76)
      assert.equal(r.stdout, '')
    }
    assert.equal(run('XKEEN_ADMISSION_ENABLED=1\n' + prefix + '\necho SIDE_EFFECT').status, 76)
    const syntax = spawnSync('/bin/sh', ['-n'], { input: bytes, encoding: 'utf8' })
    assert.equal(syntax.status, 0, syntax.stderr)
  }
  assert.equal(result.manifest.enabled, false)
  assert.equal(result.manifest.installed, false)
})
test('both inputs must match and patched output cannot be repatched', { skip: red }, () => {
  for (const invalid of [Buffer.concat([init, Buffer.from('\n')]), result.init]) {
    assert.throws(() => builder.buildCandidates({ init: invalid, dispatcher }), /identity/)
  }
  assert.throws(() => builder.buildCandidates({ init, dispatcher: Buffer.concat([dispatcher, Buffer.from('\n')]) }), /identity/)
})
test('CLI publishes disabled set with manifest last and refuses existing destination', { skip: red }, () => {
  const root = mkdtempSync(join(tmpdir(), 'native-admission-fixture-'))
  const destination = join(root, 'candidate')
  try {
    const args = ['scripts/native-admission-patch.mjs', process.env.XKEEN_ADMISSION_DISPATCHER, process.env.XKEEN_ADMISSION_INIT, destination]
    const first = spawnSync(process.execPath, args, { encoding: 'utf8', timeout: 2000 })
    assert.equal(first.status, 0, first.stderr)
    const manifest = readFileSync(join(destination, 'manifest.json'))
    assert.deepEqual(JSON.parse(manifest), result.manifest)
    assert.deepEqual(readFileSync(join(destination, 'init.disabled.sh')), result.init)
    assert.deepEqual(readFileSync(join(destination, 'dispatcher.disabled.sh')), result.dispatcher)
    const repeated = spawnSync(process.execPath, args, { encoding: 'utf8', timeout: 2000 })
    assert.equal(repeated.status, 1)
    assert.deepEqual(readFileSync(join(destination, 'manifest.json')), manifest)
  } finally { rmSync(root, { recursive: true, force: true }) }
})
