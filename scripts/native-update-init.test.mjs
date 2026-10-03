// Pure scalar/code projection and actual pinned native regeneration on disposable
// files. No init execution, installed-file mutation or router access.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const register = readFileSync(process.env.XKEEN_ADMISSION_REGISTER)
assert.equal(createHash('sha256').update(register).digest('hex'), 'bb18de4914aea76c30d8b5743b7e8d74121badd46ebf092bbda0e8f6e2df6140')
const built = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) })
const template = built.registrationTemplate.toString('utf8')
const fnText = register.toString('utf8')
const start = fnText.indexOf('register_xkeen_initd() {\n'), end = fnText.indexOf('\n# Создание конфигурации XKeen', start)
assert.ok(start >= 0 && end > start)
const fn = fnText.slice(start, end)
const declared = fn.match(/variables_to_extract="([^"]+)"/)[1].split(' ').concat('start_auto', 'start_delay')
function project(input, mode, command = 'awk') {
  const result = spawnSync(command, ['-v', `mode=${mode}`, '-f', 'scripts/native-update-init.awk'], { input, encoding: 'utf8', timeout: 2000 })
  assert.ifError(result.error)
  return result
}
function ok(input, mode) { const result = project(input, mode); assert.equal(result.status, 0, result.stderr); return result.stdout }
const changed = template.replace(/^name_policy=.*$/m, 'name_policy="fixture&A/#"')
  .replace(/^arm64_fd=.*$/m, 'arm64_fd=23456').replace(/^init_delay=.*$/m, 'init_delay=2')
  .replace(/^start_auto=.*$/m, 'start_auto="off"').replace(/^start_delay=.*$/m, 'start_delay=7')

test('projection matches the exact native declared list; settings differ while code is preserved', () => {
  const settings = ok(template, 'settings')
  assert.deepEqual(settings.trimEnd().split('\n').map(line => line.split('=')[0]), declared)
  assert.equal(ok(changed, 'code'), ok(template, 'code'))
  assert.notEqual(ok(changed, 'settings'), settings)
  for (const line of changed.split('\n').filter(line => declared.some(key => line.startsWith(`${key}=`)))) assert.ok(ok(changed, 'settings').includes(`${line}\n`))
})

test('actual native regeneration preserves exact declared bytes and prepared code', () => {
  const root = mkdtempSync('/tmp/native-init-identity-')
  chmodSync(root, 0o700)
  try {
    writeFileSync(join(root, 'init'), changed, { mode: 0o600 })
    writeFileSync(join(root, 'template'), template, { mode: 0o600 })
    const script = `initd_dir='${root}'; initd_file='${root}/init'; backups_dir='${root}/backups'; xinstall_dir='${root}'
mkdir -m 700 "$backups_dir" "$xinstall_dir/07_install_register"
cp '${root}/template' "$xinstall_dir/07_install_register/04_register_init.sh"
choice_backup_xkeen() { return 0; }
${fn}
register_xkeen_initd
`
    const result = spawnSync('/bin/sh', ['-c', script], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(result.error); assert.equal(result.status, 0, result.stderr)
    const regenerated = readFileSync(join(root, 'init'), 'utf8')
    assert.equal(ok(regenerated, 'settings'), ok(changed, 'settings'))
    assert.equal(ok(regenerated, 'code'), ok(template, 'code'))
  } finally { rmSync(root, { recursive: true, force: true }) }
})

test('unsupported missing/duplicate/dynamic/compound assignments refuse without partial output', () => {
  const values = ['"$(touch /tmp/not-executed)"', '"`id`"', '"$HOME"', '"a\\b"', '"a"; false', 'on', '"unterminated', '"a\rb"', "'a\"b'", '"a\tb"']
  const invalid = values.map(value => template.replace(/^name_policy=.*$/m, `name_policy=${value}`))
  for (const key of declared) {
    invalid.push(template.replace(new RegExp(`^${key}=.*\\n`, 'm'), ''))
    invalid.push(`${template}\n${template.match(new RegExp(`^${key}=.*$`, 'm'))[0]}\n`)
  }
  for (const value of ['01', '-1', '0x10', '1;true', '99999999999', '"$(id)"']) invalid.push(template.replace(/^arm64_fd=.*$/m, `arm64_fd=${value}`))
  for (const input of invalid) for (const mode of ['code', 'settings']) {
    const result = project(input, mode); assert.equal(result.status, 76); assert.equal(result.stdout, '')
  }
  assert.equal(project(template, 'unsupported').status, 76)
})

test('non-declared code drift is visible even when settings remain unchanged', () => {
  for (const input of [template + '\nfalse\n', template.replace('PATH=', 'MANUAL_PATH='), template + '\n name_policy="shadow"\n']) {
    assert.equal(ok(input, 'settings'), ok(template, 'settings'))
    assert.notEqual(ok(input, 'code'), ok(template, 'code'))
  }
})
