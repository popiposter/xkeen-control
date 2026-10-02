// Pinned launch fragments only; synthetic core executables, no router commands.
import assert from 'node:assert/strict'
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const source = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) }).init.toString()
const entry = readFileSync('scripts/native-admission-entry.sh', 'utf8').replaceAll('\r\n', '\n')
const strip = entry.slice(entry.indexOf('\nnative_admission_strip() {\n'))
const polluted = `
XKEEN_GATE_ROOT=/synthetic; XKEEN_GATE_TOKEN=token; XKEEN_ADMISSION_ROLE=init; XKEEN_ADMISSION_ACTION=start; XKEEN_ADMISSION_CALL=call
_na_body=1; _na_record=finish; _ng_owned_record=owner
export XKEEN_GATE_ROOT XKEEN_GATE_TOKEN XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL _na_body _na_record _ng_owned_record
`
const hints = '${XKEEN_GATE_ROOT-}${XKEEN_GATE_TOKEN-}${XKEEN_ADMISSION_ROLE-}${XKEEN_ADMISSION_ACTION-}${XKEEN_ADMISSION_CALL-}${_na_body-}${_na_record-}${_ng_owned_record-}'
function fragment(kind) {
  const begin = source.indexOf(kind === 'init' ? '                case "$name_client" in\n' : '    case "$name_client" in\n', kind === 'init' ? source.indexOf('\nproxy_start() {\n') : source.indexOf('    printf \'%s\' "$$" > "$_xkeen_start_lock/pid"'))
  const end = source.indexOf(kind === 'init' ? '                esac\n' : '    esac\n', begin)
  assert.ok(begin > 0 && end > begin)
  return source.slice(begin, end) + '\nesac\n'
}
function runCore(kind, name, fdOut, verbose) {
  const dir = mkdtempSync(join(tmpdir(), 'native-background-'))
  try {
    const core = join(dir, name)
    writeFileSync(core, `#!/bin/sh
printf '%s\\n' "$$" "$*" "${hints}" "\${SSL_CERT_FILE-}" "\${XRAY_LOCATION_CONFDIR-}" "\${XRAY_LOCATION_ASSET-}" "\${CLASH_HOME_DIR-}" "\${GOMEMLIMIT-}" > "$EVIDENCE"
`, { mode: 0o700 })
    chmodSync(core, 0o700)
    let definition = strip
    if (kind === 'hook') {
      const hook = source.indexOf('cat > "$file_netfilter_hook" <<\'EOL\'')
      const start = source.indexOf('\nnative_admission_strip() {\n', hook)
      const end = source.indexOf('\n}\n', start) + 3
      // A generated standalone hook cannot inherit shell functions from init.
      definition = start > hook && end > start ? source.slice(start, end) : ''
    }
    const r = spawnSync('/bin/sh', ['-c', `
${definition}
${polluted}
PATH='${dir}':$PATH; export PATH
EVIDENCE='${join(dir, 'receipt')}'; export EVIDENCE
name_client=${name}; fd_out=${fdOut}; start_verbose=${verbose}
directory_xray_config=/synthetic/config; directory_xray_asset=/synthetic/assets
directory_configs_app=/synthetic/clash; SSL_CERT_FILE=/synthetic/ca; export SSL_CERT_FILE
gomemlimit_value=100MiB; unset GOMEMLIMIT
apply_gomemlimit() { GOMEMLIMIT=100MiB; export GOMEMLIMIT; }; find() { :; }
${fragment(kind)}
child=$!; wait "$child" || exit $?
printf '%s\\n' "$child" "${hints}"
`], { encoding: 'utf8', timeout: 2000 })
    assert.ifError(r.error)
    assert.equal(r.status, 0, r.stderr)
    const [child, parentHints] = r.stdout.trim().split('\n')
    const data = readFileSync(join(dir, 'receipt'), 'utf8').split('\n')
    assert.equal(data[0], child, 'exec preserves the native $! core PID')
    assert.equal(data[1], name === 'xray' ? 'run' : '')
    assert.equal(data[2], '', 'child inherited admission authority')
    assert.equal(parentHints, '/synthetictokeninitstartcall1finishowner')
    assert.equal(data[3], '/synthetic/ca')
    if (name === 'xray') assert.deepEqual(data.slice(4, 6), ['/synthetic/config', '/synthetic/assets'])
    else assert.deepEqual(data.slice(6, 8), ['/synthetic/clash', '100MiB'])
  } finally { rmSync(dir, { recursive: true, force: true }) }
}
for (const name of ['xray', 'mihomo']) {
  for (const [fdOut, verbose] of [['true', 'off'], ['', 'on'], ['', 'off']]) {
    test(`init ${name} fd_out=${fdOut || 'off'} verbose=${verbose} strips authority preserving argv/env/PID`, () => runCore('init', name, fdOut, verbose))
  }
  test(`generated hook ${name} has local strip and preserves native core launch`, () => runCore('hook', name, '', 'off'))
}
test('monitor launch strips private authority before requesting fresh admission', () => {
  const begin = source.indexOf('\n_native_monitor_sample() {\n')
  const end = source.indexOf('\nload_ipset() {\n', begin)
  const monitor = source.slice(begin, end).replace('monitor_fd()', 'actual_monitor_fd()')
  const launch = source.split('\n').find(line => line.trim().endsWith('monitor_fd) &') || line.trim() === 'monitor_fd &')
  assert.ok(launch)
  const r = spawnSync('/bin/sh', ['-c', `
${strip}
${polluted}
${readFileSync('scripts/native-operation-gate.sh', 'utf8')}
${monitor}
monitor_fd() { [ -z "${hints}" ] || exit 99; actual_monitor_fd; }
native_gate_acquire() { [ -z "${hints}" ] || return 99; return 76; }
pidof() { echo $$; }; awk() { case "$1" in '/Max open files/'*) echo 1;; *) cat;; esac; }
log_warning_router() { echo MUTATION; }; rm() { echo MUTATION; }
proxy_stop() { echo MUTATION; }; proxy_start() { echo MUTATION; }
${launch}
child=$!; wait "$child"; rc=$?
printf '%s\\n' "$rc" "${hints}"
`], { encoding: 'utf8', timeout: 2000 })
  assert.ifError(r.error)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout, '76\n/synthetictokeninitstartcall1finishowner\n')
})
