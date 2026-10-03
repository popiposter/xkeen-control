// Actual pinned helper bodies in disposable storage; never a native updater.
import assert from 'node:assert/strict'
import { existsSync, readFileSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildRegistrationWrites, commonPath, deletePath, registrationPath } from './native-update-registration-patch.mjs'

const root = process.env.XKEEN_ADMISSION_PROFILE_ROOT
assert.ok(root, 'pinned complete public profile input required')
const source = new Map([commonPath, deletePath, registrationPath].map(path => [path, readFileSync(join(root, path))]))
const built = buildRegistrationWrites(source)
const unfenced = path => built.get(path).toString().replace(/^# SOURCE-ONLY FENCE[^\n]*\nreturn 76[^\n]*\n# END SOURCE-ONLY FENCE\n/, '')
const foreign = 'Package: unrelated\nVersion: 1\nStatus: install user installed\nInstalled-Time: 1\n\n'
const own = 'Package: xkeen\nVersion: old\nStatus: install user installed\nInstalled-Time: 1\n\n'
function run(code, { initial = foreign } = {}) {
  const dir = mkdtempSync(join(tmpdir(), 'native-reg-write-'))
  try {
    writeFileSync(join(dir, 'status'), initial)
    const result = spawnSync('/bin/sh', ['-c', `
      register_dir='${dir}'; status_file="$register_dir/status"; install_dir="$register_dir"
      status_architecture=all; TMPDIR="$register_dir"; export TMPDIR
      ${unfenced(commonPath)}
      ${unfenced(deletePath)}
      ${unfenced(registrationPath)}
      ${code}
    `], { encoding: 'utf8', timeout: 2000 })
    assert.ifError(result.error)
    return { ...result, statusBytes: readFileSync(join(dir, 'status'), 'utf8'),
      list: existsSync(join(dir, 'xkeen.list')) ? readFileSync(join(dir, 'xkeen.list'), 'utf8') : null,
      init: existsSync(join(dir, 'init/S05xkeen')) ? readFileSync(join(dir, 'init/S05xkeen'), 'utf8') : null }
  } finally { rmSync(dir, { recursive: true, force: true }) }
}
test('all registration sources are pinned and fenced; unrelated deletion helpers unchanged', () => {
  for (const path of [commonPath, deletePath, registrationPath]) {
    const altered = new Map(source); altered.set(path, Buffer.from('changed'))
    assert.throws(() => buildRegistrationWrites(altered), /unsupported/)
    const fenced = spawnSync('/bin/sh', ['-c', built.get(path).toString()], { encoding: 'utf8', timeout: 1000 })
    assert.equal(fenced.status, 76)
  }
  const split = 'delete_register_xkeen() {\n'
  assert.equal(unfenced(deletePath).split(split)[0], source.get(deletePath).toString().split(split)[0])
})
test('actual registration success preserves foreign stanza and optional dependencies', () => {
  for (const depends of ['', 'jq, curl']) {
    const result = run(`write_opkg_control xkeen 2 '${depends}' source origin maintainer description || exit $?
      write_opkg_status xkeen 2 '${depends}' || exit $?
      cat "$register_dir/xkeen.control"`)
    assert.equal(result.status, 0, result.stderr)
    assert.ok(result.statusBytes.includes(foreign.trim()))
    assert.equal((result.statusBytes.match(/^Package: xkeen$/gm) || []).length, 1)
    assert.equal(result.statusBytes.includes('Depends:'), Boolean(depends))
    assert.match(result.stdout, /Package: xkeen\nVersion: 2/)
  }
})
test('control producer/read/write failures cannot be masked by later fields', () => {
  for (const failure of ['du', 'date', 'echo:1', 'echo:3', 'echo:8', 'redirection']) {
    const setup = failure === 'du' ? 'du() { return 7; }'
      : failure === 'date' ? 'date() { return 7; }'
      : failure === 'redirection' ? 'register_dir="$register_dir/missing"'
      : `n=0; echo() { n=$((n+1)); [ "$n" != '${failure.split(':')[1]}' ] || return 7; command echo "$@"; }`
    const result = run(`${setup}; write_opkg_control xkeen 2 deps source origin maintainer description`)
    assert.notEqual(result.status, 0, failure)
    assert.equal(result.statusBytes, foreign)
  }
})
test('status temporary creation/date/field writes and assembly errors stop before rename', () => {
  for (const failure of ['mktemp', 'date', 'echo:1', 'echo:3', 'echo:6', 'cat:1', 'cat:2', 'sed', 'rm']) {
    let setup
    if (failure.startsWith('echo:') || failure.startsWith('cat:')) {
      const [command, failAt] = failure.split(':')
      setup = `n=0; ${command}() { n=$((n+1)); [ "$n" != '${failAt}' ] || return 7; command ${command} "$@"; }`
    } else setup = `${failure}() { return 7; }`
    const result = run(`${setup}
      mv() { printf 'RENAME\n'; command mv "$@"; }
      write_opkg_status xkeen 2 deps`)
    assert.notEqual(result.status, 0, failure)
    assert.equal(result.statusBytes, foreign, failure)
    assert.ok(!result.stdout.includes('RENAME'), failure)
  }
  const failedRename = run('mv() { return 7; }; write_opkg_status xkeen 2 deps')
  assert.notEqual(failedRename.status, 0)
  assert.equal(failedRename.statusBytes, foreign)
})
test('failed deletion AWK must not rename partial output or remove control/list', () => {
  const result = run(`awk() { printf 'partial'; return 7; }
    mv() { printf 'RENAME\n'; command mv "$@"; }
    rm() { printf 'REMOVE\n'; command rm "$@"; }
    delete_register_xkeen`, { initial: foreign + own })
  assert.notEqual(result.status, 0)
  assert.equal(result.statusBytes, foreign + own)
  assert.equal(result.stdout, '')
})
test('native delete rename/removal errors propagate and success retains other package', () => {
  const failedRename = run('mv() { return 7; }; delete_register_xkeen', { initial: foreign + own })
  assert.notEqual(failedRename.status, 0)
  assert.equal(failedRename.statusBytes, foreign + own)
  const failedRemove = run('touch "$register_dir/xkeen.control"; rm() { return 7; }; delete_register_xkeen', { initial: foreign + own })
  assert.notEqual(failedRemove.status, 0)
  assert.ok(failedRemove.statusBytes.includes(foreign.trim()))
  const success = run('delete_register_xkeen', { initial: foreign + own })
  assert.equal(success.status, 0)
  assert.ok(success.statusBytes.includes(foreign.trim()))
  assert.ok(!success.statusBytes.includes('Package: xkeen'))
})

const listSetup = `mkdir "$register_dir/modules" || exit 99
  touch "$register_dir/modules/module.sh"
  xkeen_dir="$register_dir/modules"; initd_file=/fixture/S05xkeen; log_dir=/fixture/log
  printf 'old inventory\\n' > "$register_dir/xkeen.list"`
test('failed or partial native find, append and rename preserve old package list', () => {
  for (const failure of ['find', 'echo:1', 'echo:3', 'mv', 'existing-temp']) {
    const setup = failure === 'find' ? 'find() { printf "partial\\n"; return 7; }'
      : failure.startsWith('echo:') ? `n=0; echo() { n=$((n+1)); [ "$n" != '${failure.split(':')[1]}' ] || return 7; command echo "$@"; }`
      : failure === 'existing-temp' ? 'touch "$register_dir/xkeen.list.tmp.$$"'
      : 'mv() { return 7; }'
    const result = run(`${listSetup}\n${setup}\nregister_xkeen_list`)
    assert.notEqual(result.status, 0, failure)
    assert.equal(result.list, 'old inventory\n', failure)
  }
})
test('complete native inventory publishes once without changing caller directory', () => {
  const result = run(`${listSetup}\nbefore=$PWD; register_xkeen_list || exit $?; [ "$PWD" = "$before" ]`)
  assert.equal(result.status, 0, result.stderr)
  assert.ok(!result.list.includes('old inventory'))
  assert.equal(result.list.trim().split('\n').length, 5)
  assert.match(result.list, /\/modules\/module.sh\n/)
  assert.ok(result.list.endsWith('/fixture/S05xkeen\n/fixture/log/xkeen-detached.log\n'))
})

const oldInit = 'start_auto="off"\nstart_delay="5"\nname_policy="policy&/#name"\nbackup="on"\n'
const templateInit = 'start_auto="on"\nstart_delay="0"\nname_policy="default"\nbackup="off"\n'
const initSetup = `mkdir "$register_dir/init" "$register_dir/backups" "$register_dir/07_install_register" || exit 99
  initd_dir="$register_dir/init"; initd_file="$initd_dir/S05xkeen"
  backups_dir="$register_dir/backups"; xinstall_dir="$register_dir"
  printf '%s' '${oldInit}' > "$initd_file"
  printf '%s' '${templateInit}' > "$xinstall_dir/07_install_register/04_register_init.sh"
  choice_backup_xkeen() { return 1; }`
test('native init backup/date/settings/copy/chmod/rename failures stop before live replacement', () => {
  for (const failure of ['date', 'cp:1', 'cp:2', 'awk', 'grep', 'sed', 'chmod', 'mv']) {
    const setup = failure.startsWith('cp:') ? `n=0; cp() { n=$((n+1)); [ "$n" != '${failure.split(':')[1]}' ] || return 7; command cp "$@"; }`
      : `${failure}() { return 7; }`
    const result = run(`${initSetup}\n${setup}\nregister_xkeen_initd`)
    assert.notEqual(result.status, 0, failure)
    assert.equal(result.init, oldInit, failure)
  }
})
test('native init preserves declared settings and legitimate missing optional fields', () => {
  const result = run(`${initSetup}\nregister_xkeen_initd`)
  assert.equal(result.status, 0, result.stderr)
  assert.equal(result.init, oldInit)
})
