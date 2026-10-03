// Fixed installed inventory and pinned native list fragment in disposable paths.
// No complete native scripts, service commands or router calls.
import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildInstalledProfileCheck, buildUpdateProfile, readProfileDirectory } from './native-update-profile.mjs'

assert.ok(process.env.XKEEN_ADMISSION_PROFILE_ROOT, 'complete pinned public input required')
const source = readProfileDirectory(process.env.XKEEN_ADMISSION_PROFILE_ROOT)
const built = buildUpdateProfile(source)
const check = buildInstalledProfileCheck(built).toString('utf8')
const register = source.get('_xkeen/02_install/07_install_register/02_register_xkeen.sh').toString('utf8')
const nativeList = register.slice(register.indexOf('register_xkeen_list() {\n'), register.indexOf('register_xkeen_status() {\n'))
assert.ok(nativeList.startsWith('register_xkeen_list() {\n'))
const block = check.slice(check.indexOf('# BEGIN INSTALLED PROFILE FUNCTIONS\n'), check.indexOf('# END INSTALLED PROFILE FUNCTIONS\n'))
assert.ok(block.startsWith('# BEGIN INSTALLED PROFILE FUNCTIONS\n'))

function fixture({ before = '', setup = '', nested = false, bootstrap = false, phase = 'pre', completion = true } = {}) {
  const code = mkdtempSync('/root/native-installed-code-'), root = mkdtempSync('/tmp/native-installed-call-')
  chmodSync(code, 0o700); chmodSync(root, 0o700)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', root)
    .replaceAll('/opt/sbin/.xkeen', `${code}/modules`).replaceAll('/opt/sbin/xkeen', `${code}/xkeen`)
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', `${code}/entry`)
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', `${code}/gate`)
    .replaceAll('/opt/lib/xkeen/native-update-context.sh', `${code}/context`)
    .replaceAll('/opt/lib/opkg/info/xkeen.list', `${code}/xkeen.list`)
    .replaceAll('@CODE@', code).replaceAll('@ROOT@', root)
  const put = (name, text) => writeFileSync(join(code, name), rewrite(text), { mode: 0o600 })
  for (const [name, file] of [['entry', 'native-admission-entry'], ['gate', 'native-operation-gate'], ['context', 'native-update-context']]) put(name, readFileSync(`scripts/${file}.sh`, 'utf8'))
  for (const [path, bytes] of source) {
    const target = path === 'xkeen' ? join(code, 'xkeen') : join(code, 'modules', path.slice('_xkeen/'.length))
    mkdirSync(dirname(target), { recursive: true, mode: 0o700 })
    writeFileSync(target, built.overlays.get(path) ?? bytes, { mode: 0o600 })
  }
  const libs = `. '@CODE@/gate'; . '@CODE@/entry'; . '@CODE@/context'`
  put('child', bootstrap ? check.slice(check.indexOf('# END SOURCE-ONLY FENCE\n') + '# END SOURCE-ONLY FENCE\n'.length)
    : `${libs}\n_np_phase=${phase}\n${block}\n${setup}\n_np_main\nexit $?\n`)
  put('wrapper', `. '@CODE@/gate'
native_gate_acquire '@ROOT@' update-xkeen || exit 90
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@ROOT@/operation.lock.d/call.update' || exit 91
printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@ROOT@/operation.lock.d/call.update/context'
chmod 600 '@ROOT@/operation.lock.d/call.update/context'
register_dir='@CODE@'; xkeen_dir='@CODE@/modules'; install_dir='@CODE@'
initd_file=/opt/etc/init.d/S05xkeen; log_dir=/opt/var/log
${nativeList}
register_xkeen_list || exit 92
${phase === 'post' ? `# Synthetic completion evidence, not actual updater execution.
body='v1 999999 1 0123456789abcdef0123456789abcdef'
hash=$(sha256sum '@CODE@/xkeen'); hash=\${hash%% *}
printf '%s\\n' "$body" > '@ROOT@/operation.lock.d/call.update/body'
printf '%s %s\\n' "$body" "$hash" > '@ROOT@/operation.lock.d/call.update/staged'
mkdir -m 700 '@ROOT@/operation.lock.d/call.update/exec.used'
printf '/opt/bin/sh\\000/opt/sbin/%s\\000-uk_post_update\\000' xkeen > '@ROOT@/operation.lock.d/call.update/exec.argv'
${completion ? `printf '%s updated %s\\n' "$body" "$hash" > '@ROOT@/operation.lock.d/call.update/completed'` : ''}
chmod 600 '@ROOT@/operation.lock.d/call.update/'context '@ROOT@/operation.lock.d/call.update/'body '@ROOT@/operation.lock.d/call.update/'staged '@ROOT@/operation.lock.d/call.update/'exec.argv
${completion ? "chmod 600 '@ROOT@/operation.lock.d/call.update/completed'" : ''}` : ''}
${before}
${nested ? `/bin/sh -c '/bin/sh @CODE@/child ${phase}'` : `/bin/sh '@CODE@/child' ${phase}`}
`)
  try {
    const result = spawnSync('/bin/sh', [join(code, 'wrapper')], { encoding: 'utf8', timeout: 5000 })
    assert.ifError(result.error)
    assert.equal(existsSync(join(root, 'operation.lock.d/owner')), true, 'read-only checker must retain admission')
    assert.equal(existsSync(join(root, 'operation.lock.d/call.update/body')), phase === 'post', 'checker cannot bind writer')
    return result
  } finally { rmSync(code, { recursive: true, force: true }); rmSync(root, { recursive: true, force: true }) }
}

test('compiled checker authenticates complete installed inventory directly and under bounded observer ancestry', () => {
  for (const options of [{}, { nested: true }, { bootstrap: true }]) {
    const result = fixture(options); assert.equal(result.status, 0, result.stderr); assert.equal(result.stdout, '')
  }
})
test('post inventory requires typed completion proof after body exit, not just prepared files', () => {
  for (const options of [{ phase: 'post' }, { phase: 'post', bootstrap: true, nested: true }]) {
    const r = fixture(options); assert.equal(r.status, 0, r.stderr)
  }
  const r = fixture({ phase: 'post', completion: false }); assert.equal(r.status, 77, r.stderr)
})
test('complete inventory refuses missing, changed, extra and unsafe module state', () => {
  for (const before of [
    "rm '@CODE@/modules/import.sh'",
    "printf changed >> '@CODE@/modules/import.sh'",
    "printf extra > '@CODE@/modules/extra.sh'",
    "mkdir '@CODE@/modules/unknown-empty'",
    "chmod 666 '@CODE@/modules/import.sh'",
    "chmod 777 '@CODE@/modules/02_install'",
    "mv '@CODE@/modules/import.sh' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/modules/import.sh'",
    "ln '@CODE@/modules/import.sh' '@CODE@/hardlink'",
    "printf changed >> '@CODE@/xkeen'",
  ]) { const r = fixture({ before }); assert.equal(r.status, 76, r.stderr) }
})
test('actual protected bootstrap refuses library drift before reading native profile', () => {
  for (const name of ['entry', 'gate', 'context']) for (const before of [
    `rm '@CODE@/${name}'`, `chmod 666 '@CODE@/${name}'`,
    `mv '@CODE@/${name}' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/${name}'`,
  ]) { const r = fixture({ before, bootstrap: true }); assert.equal(r.status, 76, r.stderr) }
})
test('owner generation drift during profile hashing cannot return success or release admission', () => {
  const r = fixture({ setup: `eval "$(sed 's/^_nu_file_hash()/original_file_hash()/' '@CODE@/context')"
_nu_file_hash() {
  original_file_hash "$@" || return $?
  if [ "$1" = '@CODE@/xkeen' ]; then
    printf 'v1 changed\\n' > '@ROOT@/operation.lock.d/owner'
  fi
}` })
  assert.equal(r.status, 77, r.stderr)
})
test('late content, protection, links and unknown entries refuse final inventory readback', () => {
  for (const change of [
    "printf changed >> '@CODE@/modules/import.sh'",
    "chmod 666 '@CODE@/modules/import.sh'",
    "mv '@CODE@/modules/import.sh' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/modules/import.sh'",
    "printf extra > '@CODE@/modules/extra.sh'",
    "mkdir '@CODE@/modules/unknown-empty'",
  ]) {
    const r = fixture({ setup: `eval "$(sed 's/^_nu_file_hash()/original_file_hash()/' '@CODE@/context')"
_np_drift=0
_nu_file_hash() {
  original_file_hash "$@" || return $?
  if [ "$1" = '@CODE@/xkeen' ] && [ "$_np_drift" = 0 ]; then
    _np_drift=1
    ${change}
  fi
}` })
    assert.equal(r.status, 77, r.stderr)
  }
})
test('source and compiled checker fences precede all native inventory reads', () => {
  const temporary = mkdtempSync('/tmp/native-installed-fence-')
  try { for (const bytes of [check, readFileSync('scripts/native-update-profile-check.sh', 'utf8')]) {
    writeFileSync(join(temporary, 'check'), bytes)
    const r = spawnSync('/bin/sh', [join(temporary, 'check'), 'pre'], { encoding: 'utf8', timeout: 1000 })
    assert.ifError(r.error); assert.equal(r.status, 76)
  } } finally { rmSync(temporary, { recursive: true, force: true }) }
})
test('native-generated package list rejects missing, duplicate, unknown, unsafe and nonterminated records', () => {
  for (const before of [
    "rm '@CODE@/xkeen.list'", "chmod 666 '@CODE@/xkeen.list'", "truncate -s 32769 '@CODE@/xkeen.list'",
    "printf '/unknown/path\\n' >> '@CODE@/xkeen.list'",
    "printf '%s\\n' '@CODE@/xkeen' >> '@CODE@/xkeen.list'",
    "sed -i '\\|^/opt/etc/init.d/S05xkeen$|d' '@CODE@/xkeen.list'",
    "truncate -s -1 '@CODE@/xkeen.list'",
    "mv '@CODE@/xkeen.list' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/xkeen.list'",
  ]) { const r = fixture({ before }); assert.equal(r.status, 76, r.stderr) }
})
test('package list drift after the initial inventory cannot return final success', () => {
  for (const change of ["printf '/unknown/path\\n' >> '@CODE@/xkeen.list'", "chmod 666 '@CODE@/xkeen.list'"]) {
    const r = fixture({ setup: `eval "$(sed 's/^native_update_observer_context()/original_observer_context()/' '@CODE@/context')"
_np_reads=0
native_update_observer_context() {
  original_observer_context "$@" || return $?
  _np_reads=$((_np_reads + 1))
  if [ "$_np_reads" = 2 ]; then ${change}; fi
}` })
    assert.equal(r.status, 77, r.stderr)
  }
})
