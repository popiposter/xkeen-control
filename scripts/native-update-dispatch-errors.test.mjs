// Exact pinned dispatcher branches with synthetic mandatory-operation results.
// No native modules, router commands or complete dispatcher execute.
import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const original = readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER)
const candidate = buildCandidates({ dispatcher: original, init: readFileSync(process.env.XKEEN_ADMISSION_INIT) })
const red = process.env.XKEEN_ADMISSION_UPSTREAM === '1'
const text = (red ? original : candidate.dispatcher).toString('utf8')
const begin = text.indexOf('        -uk_post_update)\n'), end = text.indexOf('        -ux)', begin)
assert.ok(begin > 0 && end > begin)
const commands = ['init_directories', 'xkeen_set_info', 'new_features', 'register_cron_initd', 'migrate_ports_from_initd',
  'register_xkeen_initd', 'create_xkeen_cfg', 'update_cron_geofile_task', 'smart_clear', 'delete_register_xkeen',
  'logs_delete_register_xkeen_info_console', 'register_xkeen_list', 'logs_register_xkeen_list_info_console',
  'register_xkeen_control', 'logs_register_xkeen_control_info_console', 'register_xkeen_status',
  'logs_register_xkeen_status_info_console', 'fixed_register_packages', 'delete_tmp', 'version_xkeen']

function post(failed = '', running = true) {
  const root = mkdtempSync('/tmp/native-post-errors-'), trace = join(root, 'trace')
  const record = name => `printf '%s\\n' '${name}' >> '${trace}'`
  writeFileSync(join(root, 'import.sh'), commands.map(name => `${name}() { ${record(name)}; [ '${failed}' != '${name}' ]; }`).join('\n') + `
chmod() { printf 'chmod-%s\\n' "$1" >> '${trace}'; [ '${failed}' != "chmod-$1" ]; }
is_proxy_running() { [ '${running}' = true ]; }
`)
  writeFileSync(join(root, 'init'), `#!/bin/sh
${record('restart')}
[ "$#:$1:$2" = '2:restart:on' ] || exit 99
[ '${failed}' != restart ] || exit 8
` , { mode: 0o700 })
  try {
    const r = spawnSync('/bin/sh', ['-c', `xkeen_dir='${root}'; initd_file='${join(root, 'init')}'
xkeen_cfg=fixture; xkeen_config=fixture
case -uk_post_update in
${text.slice(begin, end)}
esac
`, 'fixture'], { encoding: 'utf8', timeout: 2000 })
    assert.ifError(r.error)
    return { ...r, trace: readFileSync(trace, 'utf8').trim().split('\n') }
  } finally { rmSync(root, { recursive: true, force: true }) }
}

test('failed native post-update restart cannot reach package cleanup or success', () => {
  const r = post('restart'); assert.equal(r.status, 1, r.stderr)
  assert.equal(r.trace.includes('restart'), true)
  assert.equal(r.trace.includes('fixed_register_packages'), false)
  assert.equal(r.trace.includes('delete_tmp'), false)
  assert.equal(r.trace.includes('version_xkeen'), false)
})
test('reported registration and permissions failures cannot be hidden by later output', () => {
  for (const failed of ['register_xkeen_initd', 'create_xkeen_cfg', 'register_xkeen_list', 'register_xkeen_control',
    'register_xkeen_status', 'fixed_register_packages', 'chmod-700', 'chmod-600']) {
    const r = post(failed); assert.equal(r.status, 1, `${failed}: ${r.stderr}`)
    assert.equal(r.trace.at(-1), failed)
    assert.equal(r.trace.includes('version_xkeen'), false)
  }
})
test('successful running update preserves native ordering and stopped update does not restart', () => {
  const r = post(); assert.equal(r.status, 0, r.stderr)
  assert.equal(r.trace.at(-1), 'version_xkeen')
  assert.ok(r.trace.indexOf('restart') < r.trace.indexOf('fixed_register_packages'))
  const stopped = post('', false); assert.equal(stopped.status, 0, stopped.stderr)
  assert.equal(stopped.trace.includes('restart'), false)
})
test('missing post-update handoff refuses rather than falling through after live replacement', () => {
  const tailStart = text.indexOf('            # Перезапуск скрипта\n')
  const tailEnd = text.indexOf('        ;;', tailStart)
  assert.ok(tailStart > 0 && tailEnd > tailStart)
  const r = spawnSync('/bin/sh', ['-c', `grep() { return 2; }
${text.slice(tailStart, tailEnd)}
echo WRONG_SUCCESS
`], { encoding: 'utf8', timeout: 1000 })
  assert.ifError(r.error); assert.equal(r.status, 1, r.stderr)
  assert.equal(r.stdout, '')
})
