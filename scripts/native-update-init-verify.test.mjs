// Authenticated init identity reader on disposable public/synthetic files.
// The executable native verifier is not sourced or called as update acceptance.
import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const built = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) })
const template = built.registrationTemplate.toString('utf8')
const verifier = readFileSync('scripts/native-admission-verify.sh', 'utf8')
const functionsEnd = verifier.indexOf('\n_nv_main() {')
assert.ok(functionsEnd > 0 && verifier.slice(0, functionsEnd).includes('# END UPDATE INIT IDENTITY'))

function fixture({ change = value => value, setup = '', readerSetup = '', phase = 'pre', parser = readFileSync('scripts/native-update-init.awk', 'utf8'), nested = false } = {}) {
  const ram = mkdtempSync('/tmp/native-init-read-'), code = mkdtempSync('/root/native-init-read-')
  chmodSync(ram, 0o700); chmodSync(code, 0o700)
  const path = name => join(code, name)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/etc/init.d/S05xkeen', path('init'))
    .replaceAll('/opt/lib/xkeen/native-profile-v1/overlay-1.disabled.sh', path('template'))
    .replaceAll('/opt/lib/xkeen/native-update-init.awk', path('parser'))
    .replaceAll('@RAM@', ram).replaceAll('@CODE@', code)
  const put = (name, text) => writeFileSync(path(name), rewrite(text), { mode: 0o600 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  // Preserve the compared native code as data, without test path substitution.
  writeFileSync(path('init'), change(template), { mode: 0o600 })
  writeFileSync(path('template'), template, { mode: 0o600 })
  writeFileSync(path('parser'), parser, { mode: 0o600 })
  put('functions', verifier.slice(0, functionsEnd))
  put('reader', `. '@CODE@/native-operation-gate'; . '@CODE@/native-admission-entry'; . '@CODE@/native-update-context'; . '@CODE@/functions'
${readerSetup}
_nv_update_init_identity '${phase}' || exit $?
printf '%s %s %s\\n' "$_nv_update_code" "$_nv_update_settings" "$_nv_update_auto"
`)
  put('wrapper', `. '@CODE@/native-operation-gate'
native_gate_acquire '@RAM@' update-xkeen || exit $?
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@RAM@/operation.lock.d/call.update'
(umask 077; printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@RAM@/operation.lock.d/call.update/context')
${setup}
${nested ? `/bin/sh -c '/bin/sh "@CODE@/reader"'` : `/bin/sh '@CODE@/reader'`}
`)
  try {
    const result = spawnSync('/bin/sh', [path('wrapper')], { encoding: 'utf8', timeout: 4000 })
    assert.ifError(result.error)
    return { ...result, held: existsSync(join(ram, 'operation.lock.d/owner')), query: existsSync(join(ram, `operation.lock.d/call.update/init-identity.${phase}`)) }
  } finally { rmSync(ram, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('protected native init identity keeps code equal while preserving changed settings', () => {
  const original = fixture(), changed = fixture({ change: value => value.replace(/^name_policy=.*$/m, 'name_policy="fixture&A/#"').replace(/^arm64_fd=.*$/m, 'arm64_fd=23456').replace(/^start_auto=.*$/m, 'start_auto="off"') })
  assert.equal(original.status, 0, original.stderr); assert.equal(changed.status, 0, changed.stderr)
  const [code, settings, auto] = original.stdout.trim().split(' '), other = changed.stdout.trim().split(' ')
  assert.match(code, /^[a-f0-9]{64}$/); assert.match(settings, /^[a-f0-9]{64}$/)
  assert.equal(other[0], code); assert.notEqual(other[1], settings); assert.equal(auto, 'on'); assert.equal(other[2], 'off')
  assert.equal(original.held && original.query, true)
})

test('unsupported code/scalars/core identity/autostart refuse without public projection', () => {
  for (const change of [
    value => value + '\nfalse\n',
    value => value.replace(/^name_policy=.*$/m, 'name_policy="$(id)"'),
    value => value.replace(/^name_client=.*$/m, 'name_client="unsupported"'),
    value => value.replace(/^start_auto=.*$/m, 'start_auto="maybe"'),
    value => value.replace(/^start_delay=.*\n/m, ''),
  ]) {
    const result = fixture({ change }); assert.equal(result.status, 76, result.stderr); assert.equal(result.stdout, ''); assert.equal(result.held, true)
  }
})

test('unsafe dependencies, unsupported bytes and nonterminated input refuse before projection', () => {
  for (const setup of [
    "chmod 666 '@CODE@/init'", "chmod 666 '@CODE@/template'", "chmod 666 '@CODE@/parser'", "chmod 777 '@CODE@'",
    "mv '@CODE@/parser' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/parser'",
    "mv '@CODE@/init' '@CODE@/saved'; ln '@CODE@/saved' '@CODE@/init'",
    "truncate -s 524289 '@CODE@/init'",
  ]) {
    const result = fixture({ setup }); assert.equal(result.status, 76, result.stderr); assert.equal(result.query, false); assert.equal(result.stdout, '')
  }
  for (const options of [{ change: value => value.slice(0, -1) }, { parser: '' }]) {
    const result = fixture(options); assert.equal(result.status, 76); assert.equal(result.query, false)
  }
})

test('only direct authenticated reader can query; existing evidence is retained without retry', () => {
  for (const options of [
    { nested: true }, { phase: 'post' },
    { setup: "mkdir -m 700 '@RAM@/operation.lock.d/call.update/init-identity.pre'" },
    { setup: "ln -s '@RAM@/absent' '@RAM@/operation.lock.d/call.update/init-identity.pre'" },
  ]) {
    const result = fixture(options); assert.equal(result.status, 77, result.stderr); assert.equal(result.stdout, ''); assert.equal(result.held, true)
  }
})

test('producer failure/oversize retains admission and query without claiming an identity', () => {
  for (const parser of ['BEGIN { exit 99 }\n', 'BEGIN { for(i=0;i<40000;i++) print "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" }\n']) {
    const result = fixture({ parser }); assert.equal(result.status, 76, result.stderr); assert.equal(result.stdout, ''); assert.equal(result.query && result.held, true)
  }
})

test('input or parser drift during projection refuses the frozen identity', () => {
  for (const name of ['init', 'template', 'parser']) {
    const readerSetup = `eval "$(sed 's/^_nv_hash()/original_nv_hash()/' '@CODE@/functions')"
_nv_hash() {
  case "$1" in */live-settings) printf '# changed\\n' >> '@CODE@/${name}';; esac
  original_nv_hash "$1"
}`
    const result = fixture({ readerSetup }); assert.equal(result.status, 77, result.stderr); assert.equal(result.stdout, ''); assert.equal(result.query && result.held, true)
  }
})

test('same-content late protection drift cannot qualify the projected identity', () => {
  for (const change of [
    "chmod 666 '@CODE@/init'", "chmod 666 '@CODE@/template'", "chmod 666 '@CODE@/parser'", "chmod 777 '@CODE@'",
    "mv '@CODE@/init' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/init'",
    "mv '@CODE@/template' '@CODE@/saved'; ln '@CODE@/saved' '@CODE@/template'",
  ]) {
    const readerSetup = `eval "$(sed 's/^_nv_hash()/original_nv_hash()/' '@CODE@/functions')"
_nv_hash() { case "$1" in */live-settings) ${change};; esac; original_nv_hash "$1"; }`
    const result = fixture({ readerSetup }); assert.equal(result.status, 77, result.stderr); assert.equal(result.stdout, ''); assert.equal(result.query && result.held, true)
  }
})
