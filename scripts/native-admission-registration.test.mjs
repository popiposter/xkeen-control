// Opt-in pinned upstream source fixture; no whole native script/router executes.
// Upstream fragments: jameszeroX, BSD-3-Clause; native-xkeen-stop-fix.LICENSE.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const registration = readFileSync(process.env.XKEEN_ADMISSION_REGISTER)
assert.equal(createHash('sha256').update(registration).digest('hex'), 'bb18de4914aea76c30d8b5743b7e8d74121badd46ebf092bbda0e8f6e2df6140')
const init = readFileSync(process.env.XKEEN_ADMISSION_INIT)
const candidates = buildCandidates({ init, dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) })
const text = registration.toString('utf8')
const start = text.indexOf('register_xkeen_initd() {\n'), end = text.indexOf('\n# Создание конфигурации XKeen', start)
assert.ok(start >= 0 && end > start)
const fn = text.slice(start, end)

function fixture(template) {
  const root = mkdtempSync('/tmp/native-registration-')
  chmodSync(root, 0o700)
  const path = name => join(root, name)
  writeFileSync(path('template'), template, { mode: 0o600 })
  const previous = candidates.init.toString('utf8')
    .replace(/^start_auto=.*$/m, 'start_auto="off"')
    .replace(/^start_delay=.*$/m, 'start_delay=7')
    .replace(/^name_policy=.*$/m, 'name_policy="fixture&A/#"')
    .replace(/^arm64_fd=.*$/m, 'arm64_fd=23456')
  assert.ok(previous.includes('# BEGIN NATIVE ADMISSION ENTRY'))
  writeFileSync(path('init'), previous, { mode: 0o600 })
  const script = `
mkdir -m 700 '${path('backups')}'
initd_dir='${root}'; initd_file='${path('init')}'; backups_dir='${path('backups')}'
xinstall_dir='${root}'
mkdir -m 700 '${path('07_install_register')}'
cp '${path('template')}' '${path('07_install_register/04_register_init.sh')}'
choice_backup_xkeen() { return 0; }
${fn}
register_xkeen_initd
`
  try {
    const r = spawnSync('/bin/sh', ['-c', script], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(r.error); assert.equal(r.status, 0, r.stderr)
    return readFileSync(path('init'), 'utf8')
  } finally { rmSync(root, { recursive: true, force: true }) }
}
test('unmodified native registration erases an init-only admission decoration', () => {
  const output = fixture(init)
  assert.ok(!output.includes('# BEGIN NATIVE ADMISSION ENTRY'))
})
test('decorated native template retains admission and declared native settings after registration', () => {
  const output = fixture(candidates.registrationTemplate)
  assert.ok(output.includes('# BEGIN NATIVE ADMISSION ENTRY'))
  assert.ok(output.includes('# SOURCE-ONLY FENCE:'))
  assert.ok(output.indexOf('exit 76') < output.indexOf('# BEGIN NATIVE ADMISSION ENTRY'))
  assert.match(output, /^start_auto="off"$/m)
  assert.match(output, /^start_delay=7$/m)
  assert.match(output, /^proxy_dns="off"$/m)
  assert.match(output, /^proxy_router="off"$/m)
  assert.match(output, /^table_id="111"$/m)
  assert.match(output, /^name_policy="fixture&A\/#"$/m)
  assert.match(output, /^arm64_fd=23456$/m)
  assert.ok(output.includes('native_admission_hook_enter'))
  assert.ok(output.includes('native_admission_strip'))
  assert.equal(candidates.manifest.candidate.registrationTemplateSHA256, candidates.manifest.candidate.initSHA256)
})
