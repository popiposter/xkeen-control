// Authenticated bounded package projections, isolated metadata/RAM only.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

const status = 'Package: fixture\nArchitecture: aarch64-fixture\nUnknown: unchanged\n\nPackage: xkeen\nVersion: 2.0.1\nDepends: jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack\nStatus: install user installed\nArchitecture: aarch64-fixture\nInstalled-Time: 1720000000\n'
const control = 'Package: xkeen\nVersion: 2.0.1\nDepends: jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack\nSource: Skrill\nSourceName: xkeen\nSection: net\nSourceDateEpoch: 1720000000\nMaintainer: Skrill / jameszero\nArchitecture: aarch64-fixture\nInstalled-Size: 1234\nDescription: The platform that makes Xray work.\n'
const verifier = readFileSync('scripts/native-admission-verify.sh', 'utf8').split('\n_nv_main() {')[0]
function fixture({ input = status, controlInput = control, before = '', setup = '', phase = 'pre', nested = false } = {}) {
  const ram = mkdtempSync('/tmp/native-package-read-'), code = mkdtempSync('/root/native-package-read-')
  chmodSync(ram, 0o700); chmodSync(code, 0o700)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/lib/opkg/status', `${code}/status`)
    .replaceAll('/opt/lib/opkg/info/xkeen.control', `${code}/control`)
    .replaceAll('/opt/lib/xkeen/native-update-packages.awk', `${code}/parser`)
    .replaceAll('/opt/sbin/xkeen', `${code}/dispatcher`)
    .replaceAll('/opt/etc/xkeen-control/secrets/previous/.pending', `${code}/legacy-pending`)
    .replaceAll('/opt/etc/xkeen-control/previous/.pending', `${code}/pending`)
    .replaceAll('@CODE@', code).replaceAll('@RAM@', ram)
  const put = (name, text) => writeFileSync(join(code, name), rewrite(text), { mode: 0o600 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  writeFileSync(join(code, 'status'), input, { mode: 0o600 })
  writeFileSync(join(code, 'control'), controlInput, { mode: 0o600 })
  put('parser', readFileSync('scripts/native-update-packages.awk', 'utf8'))
  put('functions', verifier); put('dispatcher', '# fixture dispatcher, never executed\n')
  put('reader', `. '@CODE@/native-operation-gate'; . '@CODE@/native-admission-entry'; . '@CODE@/native-update-context'; . '@CODE@/functions'
${setup}
_nv_update_package_identity '${phase}' || exit $?
printf '%s %s\\n' "$_nv_pkg_other_hash" "$_nv_pkg_identity_hash"
`)
  put('wrapper', `. '@CODE@/native-operation-gate'
native_gate_acquire '@RAM@' update-xkeen || exit 90
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@RAM@/operation.lock.d/call.update'
printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@RAM@/operation.lock.d/call.update/context'
chmod 600 '@RAM@/operation.lock.d/call.update/context'
${phase === 'post' ? `# Synthetic terminal evidence, not real updater execution.
body='v1 999999 1 0123456789abcdef0123456789abcdef'
hash=$(sha256sum '@CODE@/dispatcher'); hash=\${hash%% *}
printf '%s\\n' "$body" > '@RAM@/operation.lock.d/call.update/body'
printf '%s %s\\n' "$body" "$hash" > '@RAM@/operation.lock.d/call.update/staged'
printf '%s updated %s\\n' "$body" "$hash" > '@RAM@/operation.lock.d/call.update/completed'
mkdir -m 700 '@RAM@/operation.lock.d/call.update/exec.used'
printf '/opt/bin/sh\\000/opt/sbin/%s\\000-uk_post_update\\000' xkeen > '@RAM@/operation.lock.d/call.update/exec.argv'
chmod 600 '@RAM@/operation.lock.d/call.update/'body '@RAM@/operation.lock.d/call.update/'staged '@RAM@/operation.lock.d/call.update/'completed '@RAM@/operation.lock.d/call.update/'exec.argv` : ''}
${before}
${nested ? "/bin/sh -c '/bin/sh @CODE@/reader'" : "/bin/sh '@CODE@/reader'"}
`)
  try {
    const r = spawnSync('/bin/sh', [join(code, 'wrapper')], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(r.error)
    assert.equal(existsSync(join(ram, 'operation.lock.d/owner')), true)
    return { ...r, query: existsSync(join(ram, `operation.lock.d/call.update/package-identity.${phase}`)) }
  } finally { rmSync(ram, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}
const sha = text => createHash('sha256').update(text).digest('hex')
test('direct authenticated pre/post reader returns only bounded metadata hashes', () => {
  for (const phase of ['pre', 'post']) {
    const r = fixture({ phase }); assert.equal(r.status, 0, r.stderr); assert.equal(r.query, true)
    assert.equal(r.stdout, `${sha('Package: fixture\nArchitecture: aarch64-fixture\nUnknown: unchanged\n\n')} ${sha('xkeen 2.0.1 aarch64-fixture\n')}\n`)
  }
})
test('unsafe, nonterminated or oversized status/parser refuse before creating query', () => {
  for (const before of ["chmod 666 '@CODE@/status'", "chmod 666 '@CODE@/parser'", "chmod 777 '@CODE@'",
    "mv '@CODE@/parser' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/parser'",
    "ln '@CODE@/status' '@CODE@/linked'", "truncate -s 524289 '@CODE@/status'",
    "truncate -s 65537 '@CODE@/parser'", "rm '@CODE@/parser'",
    "chmod 666 '@CODE@/control'", "truncate -s 16385 '@CODE@/control'", "rm '@CODE@/control'",
  ]) { const r = fixture({ before }); assert.equal(r.status, 76, r.stderr); assert.equal(r.query, false); assert.equal(r.stdout, '') }
  const r = fixture({ input: status.trimEnd() }); assert.equal(r.status, 76); assert.equal(r.query, false)
})
test('malformed projections, existing query, missing terminal and nested reader retain admission', () => {
  for (const options of [{ input: status.replace('Version: 2.0.1', 'Version: unknown') },
    { before: "mkdir -m 700 '@RAM@/operation.lock.d/call.update/package-identity.pre'" },
    { phase: 'post', before: "rm '@RAM@/operation.lock.d/call.update/completed'" }, { nested: true },
  ]) { const r = fixture(options); assert.ok([76, 77].includes(r.status)); assert.equal(r.stdout, '') }
})
test('pending configuration blocks metadata reads without query creation', () => {
  for (const before of ["touch '@CODE@/pending'", "ln -s /not-found '@CODE@/legacy-pending'"]) {
    const r = fixture({ before }); assert.equal(r.status, 75); assert.equal(r.query, false); assert.equal(r.stdout, '')
  }
})
test('late status/parser content or protection and owner drift refuse with query retained', () => {
  for (const change of ["printf changed >> '@CODE@/status'", "chmod 666 '@CODE@/status'",
    "chmod 666 '@CODE@/parser'", "printf '# changed\\n' >> '@CODE@/parser'",
    "chmod 666 '@CODE@/control'", "printf '# changed\\n' >> '@CODE@/control'",
    "printf 'v1 changed\\n' >| '@RAM@/operation.lock.d/owner'",
  ]) {
    const r = fixture({ setup: `awk() {
command awk "$@" || return $?
case "$*" in *mode=control*) ${change};; esac
}` })
    assert.equal(r.status, 77, r.stderr); assert.equal(r.query, true); assert.equal(r.stdout, '')
  }
})
test('control and status must agree; native timestamp/size changes preserve metadata identity', () => {
  const original = fixture(), changed = fixture({ controlInput: control.replace('1720000000', '1720000001').replace('1234', '5678') })
  assert.equal(original.status, 0); assert.equal(changed.status, 0); assert.equal(original.stdout, changed.stdout)
  for (const controlInput of [control.replace('aarch64-fixture', 'different-architecture'), control.replace('Version: 2.0.1', 'Version: 2.0.2'), control.trimEnd()]) {
    const r = fixture({ controlInput }); assert.equal(r.status, 76, r.stderr); assert.equal(r.stdout, '')
  }
})
