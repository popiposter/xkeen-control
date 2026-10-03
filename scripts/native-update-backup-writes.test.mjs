// Actual pinned backup functions only, in disposable paths; no native updater.
import assert from 'node:assert/strict'
import { existsSync, readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildBackupWrites, choicePath, backupPath } from './native-update-backup-patch.mjs'
const root = process.env.XKEEN_ADMISSION_PROFILE_ROOT
assert.ok(root, 'pinned complete public input required')
const source = new Map([choicePath, backupPath].map(path => [path, readFileSync(join(root, path))]))
const built = buildBackupWrites(source)
const unfenced = path => built.get(path).toString().replace(/^# SOURCE-ONLY FENCE[^\n]*\nreturn 76[^\n]*\n# END SOURCE-ONLY FENCE\n/, '')
function run(code) {
  const dir = mkdtempSync('/tmp/native-backup-write-')
  try {
    const result = spawnSync('/bin/sh', ['-c', `
      base='${dir}'; backups_dir="$base/backups"; install_dir="$base/install"; initd_file="$base/S05xkeen"
      command mkdir "$backups_dir" "$install_dir" "$install_dir/.xkeen" || exit 99
      printf 'dispatcher\\n' > "$install_dir/xkeen"
      printf 'module\\n' > "$install_dir/.xkeen/module"
      printf 'backup="on"\\n' > "$initd_file"
      current_datetime=fixture; xkeen_current_version=2; manual_backup=
      ${unfenced(choicePath)}
      ${unfenced(backupPath)}
      ${code}
    `], { encoding: 'utf8', timeout: 2000 })
    assert.ifError(result.error)
    const backup = join(dir, 'backups/fixture_xkeen_v2')
    return { ...result, created: existsSync(backup),
      copied: existsSync(join(backup, 'xkeen')) ? readFileSync(join(backup, 'xkeen'), 'utf8') : null,
      module: existsSync(join(backup, '_xkeen/module')) ? readFileSync(join(backup, '_xkeen/module'), 'utf8') : null }
  } finally { rmSync(dir, { recursive: true, force: true }) }
}
test('backup sources are exact-pinned/fenced and native restore function unchanged', () => {
  for (const path of [choicePath, backupPath]) {
    const changed = new Map(source); changed.set(path, Buffer.from('altered'))
    assert.throws(() => buildBackupWrites(changed), /unsupported/)
    const result = spawnSync('/bin/sh', ['-c', built.get(path).toString()], { timeout: 1000 })
    assert.equal(result.status, 76)
  }
  const marker = 'restore_backup_xkeen() {\n'
  assert.equal(unfenced(backupPath).split(marker)[1], source.get(backupPath).toString().split(marker)[1])
})
test('failed native backup choice is distinct from disabled and fails before mkdir', () => {
  const result = run('awk() { return 7; }; backup_xkeen')
  assert.notEqual(result.status, 0)
  assert.equal(result.created, false)
})
test('disabled native automatic backup is no-op; manual backup still copies', () => {
  const disabled = run('printf \'backup="off"\\n\' > "$initd_file"; backup_xkeen')
  assert.equal(disabled.status, 0); assert.equal(disabled.created, false)
  const manual = run('printf \'backup="off"\\n\' > "$initd_file"; manual_backup=yes; backup_xkeen')
  assert.equal(manual.status, 0, manual.stderr)
  assert.equal(manual.copied, 'dispatcher\n'); assert.equal(manual.module, 'module\n')
})
test('native mkdir/copy/rename failures cannot produce successful backup output', () => {
  for (const command of ['mkdir', 'cp', 'mv']) {
    const result = run(`${command}() { return 7; }; backup_xkeen`)
    assert.notEqual(result.status, 0, command)
    assert.ok(!result.stdout.includes('создана'), command)
    assert.equal(result.module, null)
  }
})
test('successful native backup retains dispatcher and all module bytes', () => {
  const result = run('backup_xkeen')
  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.copied, 'dispatcher\n'); assert.equal(result.module, 'module\n')
})
