// Explicit complete pinned public profile fixtures. No native code is executed.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { cpSync, linkSync, mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildUpdateProfile, profile, readProfileDirectory } from './native-update-profile.mjs'

const root = process.env.XKEEN_ADMISSION_PROFILE_ROOT
assert.ok(root, 'pinned complete public profile input required')
const source = readProfileDirectory(root)
const sha = bytes => createHash('sha256').update(bytes).digest('hex')

test('complete pinned native import closure preserves all but three fenced overlays', () => {
  const built = buildUpdateProfile(source)
  assert.equal(profile.upstreamCommit, '5aaece27a70d5bd002c615248614914ebbc4569d')
  assert.equal(profile.archiveSHA256, '1d246871d8fc9df2e68e80cef18562e6222661e40eaea1b6853cf8e0e6348b5f')
  assert.equal(profile.files.length, 72)
  assert.equal(profile.files.reduce((sum, file) => sum + file.size, 0), 559777)
  assert.equal(built.manifest.enabled, false)
  assert.equal(built.manifest.installed, false)
  assert.equal(built.overlays.size, 3)
  assert.ok(source.has('_xkeen/import.sh'))
  for (const file of built.manifest.prepared) {
    const bytes = built.overlays.get(file.path)
    if (!bytes) { assert.deepEqual(file, profile.files.find(original => original.path === file.path)); continue }
    assert.equal(file.sha256, sha(bytes)); assert.equal(file.size, bytes.length)
    assert.ok(bytes.toString('utf8').includes('SOURCE-ONLY FENCE'))
    const fenced = mkdtempSync('/tmp/native-profile-fence-')
    try {
      writeFileSync(join(fenced, 'disabled.sh'), bytes, { mode: 0o600 })
      const result = spawnSync('/bin/sh', [join(fenced, 'disabled.sh')], { encoding: 'utf8', timeout: 1000 })
      assert.ifError(result.error); assert.equal(result.status, 76, result.stderr)
    } finally { rmSync(fenced, { recursive: true, force: true }) }
  }
})

test('every modified or missing source module refuses, including non-overlay imports', () => {
  for (const file of profile.files) {
    const changed = new Map(source)
    const bytes = Buffer.from(changed.get(file.path)); bytes[0] ^= 1
    changed.set(file.path, bytes)
    assert.throws(() => buildUpdateProfile(changed), /unsupported/)
    const missing = new Map(source); missing.delete(file.path)
    assert.throws(() => buildUpdateProfile(missing), /unsupported/)
  }
  const extra = new Map(source); extra.set('_xkeen/extra.sh', Buffer.from('unexpected'))
  assert.throws(() => buildUpdateProfile(extra), /unsupported/)
})

function copied(change) {
  const temporary = mkdtempSync('/tmp/native-profile-')
  const path = join(temporary, 'public')
  try { cpSync(root, path, { recursive: true }); change(path); return readProfileDirectory(path) }
  finally { rmSync(temporary, { recursive: true, force: true }) }
}
test('directory reader refuses extra files, empty directories, unsafe links and wrong size', () => {
  for (const change of [
    path => writeFileSync(join(path, '_xkeen/extra.sh'), 'unexpected'),
    path => mkdirSync(join(path, '_xkeen/extra')),
    path => { rmSync(join(path, 'xkeen')); symlinkSync(join(root, 'xkeen'), join(path, 'xkeen')) },
    path => { rmSync(join(path, 'xkeen')); linkSync(join(path, '_xkeen/import.sh'), join(path, 'xkeen')) },
    path => writeFileSync(join(path, 'xkeen'), 'wrong size'),
    path => { rmSync(join(path, '_xkeen'), { recursive: true }); symlinkSync(join(root, '_xkeen'), join(path, '_xkeen')) },
  ]) assert.throws(() => copied(change), /profile/)
})

test('CLI publishes only disabled files and refuses an existing output directory', () => {
  const temporary = mkdtempSync('/tmp/native-profile-output-'), output = join(temporary, 'disabled')
  try {
    const run = () => spawnSync(process.execPath, ['scripts/native-update-profile.mjs', root, output], { encoding: 'utf8', timeout: 2000 })
    const first = run(); assert.ifError(first.error); assert.equal(first.status, 0, first.stderr)
    const manifest = readFileSync(join(output, 'manifest.json'))
    const second = run(); assert.ifError(second.error); assert.equal(second.status, 1)
    assert.deepEqual(readFileSync(join(output, 'manifest.json')), manifest)
    const parsed = JSON.parse(manifest)
    assert.equal(parsed.prepared.filter(file => file.overlay).length, 3)
    const worker = readFileSync(join(output, parsed.worker.file))
    assert.equal(worker.length, parsed.worker.size)
    assert.equal(sha(worker), parsed.worker.sha256)
  } finally { rmSync(temporary, { recursive: true, force: true }) }
})
