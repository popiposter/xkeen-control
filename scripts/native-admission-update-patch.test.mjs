// Opt-in exact public native installer fixture. No router or whole XKeen execution.
import assert from 'node:assert/strict'
import { existsSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildUpdateCandidate } from './native-admission-update-patch.mjs'

const source = readFileSync(process.env.XKEEN_ADMISSION_INSTALLER)
const built = buildUpdateCandidate(source)
const text = built.candidate.toString('utf8')
const begin = text.indexOf('install_xkeen() {\n')
const end = text.indexOf('\ncheck_keen_mode()', begin)
assert.ok(begin >= 0 && end > begin)
// Extract only the reviewed native installer function. The complete module stays fenced.
const fn = text.slice(begin, end)

test('candidate is unconditionally fenced and rejects changed upstream bytes', () => {
  assert.ok(text.indexOf('return 76 2>/dev/null || exit 76') < begin)
  assert.equal(built.manifest.enabled, false)
  assert.equal(built.manifest.installed, false)
  assert.throws(() => buildUpdateCandidate(Buffer.concat([source, Buffer.from('\n')])) )
})

function fixture(mode) {
  const root = mkdtempSync('/tmp/native-update-stage-')
  const path = name => join(root, name)
  for (const dir of ['ram', 'live', 'live/.xkeen', 'archive', 'archive/_xkeen', 'log']) mkdirSync(path(dir), { mode: 0o700 })
  writeFileSync(path('live/xkeen'), 'old dispatcher\n')
  writeFileSync(path('live/.xkeen/profile'), 'old profile\n')
  writeFileSync(path('archive/xkeen'), 'new dispatcher\n')
  writeFileSync(path('archive/_xkeen/profile'), 'new profile\n')
  writeFileSync(path('worker'), `#!/bin/sh
printf '%s\\n' "$1" >> '${path('calls')}'
[ '${mode}' != reject ] || exit 76
printf '%s\\n' 'decorated dispatcher' > "$1/xkeen"
printf '%s\\n' 'decorated profile' > "$1/_xkeen/profile"
`)
  // Fixed callback paths are rewritten only inside this disposable fixture.
  const isolated = fn.replaceAll('/opt/lib/xkeen/native-update-stage.sh', path('worker')).replaceAll('/opt/bin/sh', '/bin/sh')
  const script = `
tmp_ram='${path('ram')}'; install_dir='${path('live')}'; log_dir='${path('log')}'
tar -czf "$tmp_ram/xkeen.tar.gz" -C '${path('archive')}' xkeen _xkeen || exit 90
_na_file_ok() { [ '${mode}' != missing ]; }
# This earlier fixture isolates stage callback ordering. Archive admission is
# independently exercised with actual public bytes in stage-install fixtures.
native_update_archive_ready() { return 0; }
mkdir -p "$log_dir/xkeen"
chmod() { [ '${mode}' != chmod-failure ] || return 1; command chmod "$@"; }
mv() {
    if [ '${mode}' = dispatcher-backup-failure ] && [ "$1" = "$install_dir/xkeen" ]; then return 1; fi
    if [ '${mode}' = module-backup-failure ] && [ "$1" = "$install_dir/.xkeen" ]; then return 1; fi
    command mv "$@"
}
rm() {
    if [ '${mode}' = stage-cleanup-failure ] && [ "$#" = 3 ] && [ "$2" = "$install_dir/.xkeen.old" ]; then return 1; fi
    if [ '${mode}' = dispatcher-cleanup-failure ] && [ "$2" = "$install_dir/xkeen.old" ] && [ -f "$install_dir/xkeen.old" ]; then return 1; fi
    if [ '${mode}' = log-cleanup-failure ] && [ "$2" = "$log_dir/xkeen" ]; then return 1; fi
    command rm "$@"
}
${isolated}
install_xkeen
`
  try {
    const result = spawnSync('/bin/sh', ['-c', script], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(result.error)
    return { status: result.status, dispatcher: readFileSync(path('live/xkeen'), 'utf8'),
      profile: readFileSync(path('live/.xkeen/profile'), 'utf8'),
      calls: existsSync(path('calls')) ? readFileSync(path('calls'), 'utf8').trim().split('\n') : [],
      liveEntries: readdirSync(path('live')).sort(), archivePresent: existsSync(path('ram/xkeen.tar.gz')) }
  } finally { rmSync(root, { recursive: true, force: true }) }
}

for (const mode of ['missing', 'reject']) test(`${mode} preservation refuses before native live replacements`, () => {
  const result = fixture(mode)
  assert.equal(result.status, 1)
  assert.equal(result.dispatcher, 'old dispatcher\n')
  assert.equal(result.profile, 'old profile\n')
  assert.equal(result.calls.length, mode === 'missing' ? 0 : 1)
  assert.deepEqual(result.liveEntries, ['.xkeen', 'xkeen'])
  assert.equal(result.archivePresent, false)
})
test('successful preservation precedes both native live replacements', () => {
  const result = fixture('pass')
  assert.equal(result.status, 0)
  assert.equal(result.dispatcher, 'decorated dispatcher\n')
  assert.equal(result.profile, 'decorated profile\n')
  assert.equal(result.calls.length, 1)
  assert.deepEqual(result.liveEntries, ['.xkeen', 'xkeen'])
})

test('native permission, backup and successful-path cleanup failures cannot return success', () => {
  for (const mode of ['chmod-failure', 'dispatcher-backup-failure', 'module-backup-failure',
    'dispatcher-cleanup-failure', 'stage-cleanup-failure', 'log-cleanup-failure']) {
    const result = fixture(mode)
    assert.equal(result.status, 1, mode)
    assert.equal(result.calls.length, 1, mode)
    assert.equal(result.archivePresent, true, mode)
    if (mode === 'chmod-failure' || mode === 'dispatcher-backup-failure') {
      assert.equal(result.dispatcher, 'old dispatcher\n', mode)
      assert.equal(result.profile, 'old profile\n', mode)
    } else if (mode === 'module-backup-failure') {
      assert.equal(result.dispatcher, 'decorated dispatcher\n', mode)
      assert.equal(result.profile, 'old profile\n', mode)
    } else {
      assert.equal(result.dispatcher, 'decorated dispatcher\n', mode)
      assert.equal(result.profile, 'decorated profile\n', mode)
    }
  }
})
