// Real shell gate/context + complete public staging tree; no live native moves.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildStageWorker, buildUpdateProfile, profile, readProfileDirectory } from './native-update-profile.mjs'

const sourceRoot = process.env.XKEEN_ADMISSION_PROFILE_ROOT
assert.ok(sourceRoot, 'complete pinned public input required')
const built = buildUpdateProfile(readProfileDirectory(sourceRoot))
const worker = buildStageWorker(built).toString('utf8')
const begin = worker.indexOf('# BEGIN PROFILE STAGE FUNCTIONS\n')
const end = worker.indexOf('# END PROFILE STAGE FUNCTIONS\n')
assert.ok(begin > 0 && end > begin)
const sha = bytes => createHash('sha256').update(bytes).digest('hex')

function fixture({ before = '', childSetup = '', grandchild = false, bootstrap = false } = {}) {
  const code = mkdtempSync('/root/native-stage-code-'), root = mkdtempSync('/tmp/native-stage-call-')
  chmodSync(code, 0o700); chmodSync(root, 0o700)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', root)
    .replaceAll('/opt/sbin/.xkeen.stage.', `${code}/.xkeen.stage.`)
    .replaceAll('/opt/lib/xkeen/native-profile-v1', code)
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', join(code, 'native-admission-entry'))
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', join(code, 'native-operation-gate'))
    .replaceAll('/opt/lib/xkeen/native-update-context.sh', join(code, 'native-update-context'))
    .replaceAll('@ROOT@', root).replaceAll('@CODE@', code)
  const put = (name, text) => writeFileSync(join(code, name), rewrite(text), { mode: 0o600 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) {
    put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  }
  [...built.overlays.values()].forEach((bytes, index) => writeFileSync(join(code, `overlay-${index}.disabled.sh`), bytes, { mode: 0o600 }))
  const libraries = `. '@CODE@/native-operation-gate'; . '@CODE@/native-admission-entry'; . '@CODE@/native-update-context'`
  put('child', bootstrap
    ? worker.slice(worker.indexOf('# END SOURCE-ONLY FENCE\n') + '# END SOURCE-ONLY FENCE\n'.length)
    : `${libraries}\n${worker.slice(begin, end)}\n${childSetup}\nnative_update_stage_apply "$stage"\n`)
  put('body', `${libraries}
native_update_bind_body || exit $?
stage="/opt/sbin/.xkeen.stage.$_ng_self_pid"; export stage
mkdir -m 700 "$stage" || exit 90
cp -R '${sourceRoot}/.' "$stage/" || exit 90
${before}
${grandchild ? "/bin/sh -c '/bin/sh @CODE@/child \"$stage\"'" : "/bin/sh '@CODE@/child' \"$stage\""}
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
    const stageName = readdirSync(code).find(name => name.startsWith('.xkeen.stage.'))
    const stage = join(code, stageName)
    return { ...result, staged: existsSync(join(root, 'operation.lock.d/call.update/staged')),
      files: new Map(profile.files.map(file => [file.path, existsSync(join(stage, file.path)) ? readFileSync(join(stage, file.path)) : null])) }
  } finally { rmSync(code, { recursive: true, force: true }); rmSync(root, { recursive: true, force: true }) }
}

test('stage worker artifact and source template are unconditionally fenced', () => {
  for (const bytes of [worker, readFileSync('scripts/native-update-stage.sh', 'utf8')]) {
    const code = mkdtempSync('/tmp/native-stage-fence-')
    try {
      writeFileSync(join(code, 'disabled'), bytes)
      const result = spawnSync('/bin/sh', [join(code, 'disabled')], { encoding: 'utf8', timeout: 1000 })
      assert.ifError(result.error); assert.equal(result.status, 76)
    } finally { rmSync(code, { recursive: true, force: true }) }
  }
})
test('authenticated immediate child decorates exactly three files and publishes last', () => {
  const r = fixture(); assert.equal(r.status, 0, r.stderr); assert.equal(r.staged, true)
  for (const file of built.manifest.prepared) {
    assert.equal(r.files.get(file.path).length, file.size)
    assert.equal(sha(r.files.get(file.path)), file.sha256)
  }
})
test('actual isolated fixed bootstrap authenticates the worker before decoration', () => {
  const r = fixture({ bootstrap: true }); assert.equal(r.status, 0, r.stderr)
  assert.equal(r.staged, true)
  for (const file of built.manifest.prepared) assert.equal(sha(r.files.get(file.path)), file.sha256)
})
test('actual bootstrap refuses missing, writable or symlinked libraries before decoration', () => {
  for (const name of ['native-admission-entry', 'native-operation-gate', 'native-update-context']) {
    for (const before of [
      `rm '@CODE@/${name}'`,
      `chmod 666 '@CODE@/${name}'`,
      `mv '@CODE@/${name}' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/${name}'`,
    ]) {
      const r = fixture({ before, bootstrap: true }); assert.equal(r.status, 76, r.stderr)
      assert.equal(r.staged, false)
      assert.equal(sha(r.files.get('xkeen')), profile.files.find(file => file.path === 'xkeen').sha256)
    }
  }
})
test('unknown profile and unsafe payloads refuse before any decoration or receipt', () => {
  for (const before of [
    'printf extra > "$stage/_xkeen/extra.sh"',
    'mkdir "$stage/_xkeen/extra"',
    'printf changed >> "$stage/_xkeen/import.sh"',
    'rm "$stage/_xkeen/import.sh"',
    'chmod 777 "$stage/_xkeen"',
    'rm "$stage/_xkeen/import.sh"; ln -s /dev/null "$stage/_xkeen/import.sh"',
    "printf changed >> '@CODE@/overlay-1.disabled.sh'",
    "chmod 666 '@CODE@/overlay-1.disabled.sh'",
    "rm '@CODE@/overlay-1.disabled.sh'",
  ]) {
    const r = fixture({ before }); assert.equal(r.status, 76, r.stderr); assert.equal(r.staged, false)
    assert.equal(sha(r.files.get('xkeen')), profile.files.find(file => file.path === 'xkeen').sha256)
  }
})
test('grandchild inheritance cannot execute the writer', () => {
  const r = fixture({ grandchild: true }); assert.equal(r.status, 77, r.stderr); assert.equal(r.staged, false)
})
test('partial staging write never publishes a prepared receipt', () => {
  const r = fixture({ childSetup: `mv() { case "$2" in */.admission-overlay-1) return 1;; esac; command mv "$@"; }` })
  assert.equal(r.status, 77, r.stderr); assert.equal(r.staged, false)
})
test('generation drift after publication retains receipt and refuses worker completion', () => {
  const r = fixture({ childSetup: `eval "$(sed 's/^_nu_load_call()/original_load_call()/' '@CODE@/native-update-context')"
_nu_load_call() {
  if [ -f '@ROOT@/operation.lock.d/call.update/staged' ]; then
    _native_gate_proc "$_nu_body_pid" || return 77
    printf 'v1 %s %s %s %s update-xkeen\\n' "$_ng_current_boot" "$_nu_body_pid" "$_ng_proc_start" "$XKEEN_GATE_TOKEN" > '@ROOT@/operation.lock.d/owner'
  fi
  original_load_call
}` })
  assert.equal(r.status, 77, r.stderr); assert.equal(r.staged, true)
})
test('non-overlay module drift after receipt publication refuses completion', () => {
  const r = fixture({ childSetup: `eval "$(sed 's/^_nu_file_hash()/original_file_hash()/' '@CODE@/native-update-context')"
_nu_file_hash() {
  if [ -f '@ROOT@/operation.lock.d/call.update/staged' ]; then
    printf '# changed\\n' >> "$stage/_xkeen/import.sh"
  fi
  original_file_hash "$@"
}` })
  assert.equal(r.status, 77, r.stderr); assert.equal(r.staged, true)
})
