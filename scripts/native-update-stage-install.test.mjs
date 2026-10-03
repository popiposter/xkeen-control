// Actual pinned native installer function + authenticated complete stage worker.
// Only disposable paths/functions execute; the complete candidates stay fenced.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildUpdateCandidate } from './native-admission-update-patch.mjs'
import { buildStageWorker, buildUpdateProfile, readProfileDirectory } from './native-update-profile.mjs'

const sourceRoot = process.env.XKEEN_ADMISSION_PROFILE_ROOT
assert.ok(sourceRoot, 'complete pinned public profile required')
const built = buildUpdateProfile(readProfileDirectory(sourceRoot))
const installer = buildUpdateCandidate(readFileSync(join(sourceRoot, '_xkeen/02_install/03_install_xkeen.sh'))).candidate.toString('utf8')
const fnBegin = installer.indexOf('install_xkeen() {\n'), fnEnd = installer.indexOf('\ncheck_keen_mode()', fnBegin)
assert.ok(fnBegin > 0 && fnEnd > fnBegin)
const worker = buildStageWorker(built).toString('utf8')
const begin = worker.indexOf('# BEGIN PROFILE STAGE FUNCTIONS\n'), end = worker.indexOf('# END PROFILE STAGE FUNCTIONS\n')
assert.ok(begin > 0 && end > begin)
const sha = bytes => createHash('sha256').update(bytes).digest('hex')

function fixture(mode) {
  const code = mkdtempSync('/root/native-install-profile-'), root = mkdtempSync('/tmp/native-install-call-')
  chmodSync(code, 0o700); chmodSync(root, 0o700)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', root)
    .replaceAll('/opt/sbin/.xkeen.stage.', `${code}/live/.xkeen.stage.`)
    .replaceAll('/opt/lib/xkeen/native-profile-v1', code)
    .replaceAll('/opt/lib/xkeen/native-update-stage.sh', join(code, 'runtime-worker'))
    .replaceAll('/opt/bin/sh', '/bin/sh').replaceAll('@ROOT@', root).replaceAll('@CODE@', code)
  const put = (name, text) => writeFileSync(join(code, name), rewrite(text), { mode: 0o600 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) {
    put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  }
  for (const [index, bytes] of [...built.overlays.values()].entries()) writeFileSync(join(code, `overlay-${index}.disabled.sh`), bytes, { mode: 0o600 })
  const libs = `. '@CODE@/native-operation-gate'; . '@CODE@/native-admission-entry'; . '@CODE@/native-update-context'`
  put('runtime-worker', `${libs}\n${worker.slice(begin, end)}\nnative_update_stage_apply "$@"\nexit $?\n`)
  put('body', `${libs}
native_update_bind_body || exit $?
mkdir -m 700 '@CODE@/live' '@CODE@/ram' '@CODE@/log' '@CODE@/archive' || exit 90
mkdir -m 700 '@CODE@/live/.xkeen' || exit 90
printf 'old dispatcher\\n' > '@CODE@/live/xkeen'
printf 'old module\\n' > '@CODE@/live/.xkeen/old'
cp -R '${sourceRoot}/.' '@CODE@/archive/' || exit 90
${mode === 'unknown' ? "printf unexpected > '@CODE@/archive/_xkeen/unexpected.sh'" : ''}
${mode === 'missing' ? "rm '@CODE@/runtime-worker'" : ''}
tmp_ram='@CODE@/ram'; install_dir='@CODE@/live'; log_dir='@CODE@/log'
tar -czf "$tmp_ram/xkeen.tar.gz" -C '@CODE@/archive' xkeen _xkeen || exit 90
${installer.slice(fnBegin, fnEnd)}
install_xkeen
`)
  put('wrapper', `. '@CODE@/native-operation-gate'
native_gate_acquire '@ROOT@' update-xkeen || exit 90
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@ROOT@/operation.lock.d/call.update' || exit 91
printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@ROOT@/operation.lock.d/call.update/context'
chmod 600 '@ROOT@/operation.lock.d/call.update/context'
/bin/sh '@CODE@/body'
rc=$?
[ -f '@ROOT@/operation.lock.d/owner' ] || exit 93
exit "$rc"
`)
  try {
    const result = spawnSync('/bin/sh', [join(code, 'wrapper')], { encoding: 'utf8', timeout: 5000 })
    assert.ifError(result.error)
    return { ...result, receipt: existsSync(join(root, 'operation.lock.d/call.update/staged')),
      oldModule: existsSync(join(code, 'live/.xkeen/old')),
      archive: existsSync(join(code, 'ram/xkeen.tar.gz')),
      files: new Map(built.manifest.prepared.map(file => {
        const path = join(code, 'live', file.path.replace(/^_xkeen\//, '.xkeen/'))
        return [file.path, existsSync(path) ? readFileSync(path) : null]
      })) }
  } finally { rmSync(code, { recursive: true, force: true }); rmSync(root, { recursive: true, force: true }) }
}

for (const mode of ['missing', 'unknown']) test(`native ${mode} preservation refuses before both live replacements`, () => {
  const r = fixture(mode); assert.equal(r.status, 1, r.stderr)
  assert.equal(r.files.get('xkeen').toString(), 'old dispatcher\n')
  assert.equal(r.oldModule, true); assert.equal(r.receipt, false); assert.equal(r.archive, false)
})
test('actual native replacement receives complete decorated generation and bound receipt', () => {
  const r = fixture('pass'); assert.equal(r.status, 0, r.stderr); assert.equal(r.receipt, true)
  assert.equal(r.oldModule, false)
  // Native post-update cleanup owns successful archive retirement later.
  assert.equal(r.archive, true)
  for (const file of built.manifest.prepared) {
    assert.equal(r.files.get(file.path).length, file.size)
    assert.equal(sha(r.files.get(file.path)), file.sha256)
  }
})
