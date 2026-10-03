// Actual pinned native registration fragments on disposable public metadata.
import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildUpdateProfile, readProfileDirectory } from './native-update-profile.mjs'

const source = readProfileDirectory(process.env.XKEEN_ADMISSION_PROFILE_ROOT)
buildUpdateProfile(source)
const own = 'Package: xkeen\nVersion: 2.0.1\nDepends: jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack\nStatus: install user installed\nArchitecture: aarch64-fixture\nInstalled-Time: 1720000000\n'
const other = 'Package: fixture\nVersion: 1.0\nArchitecture: aarch64-fixture\nDescription: unchanged\n continuation: literal\nUnknown: value\n'
const common = source.get('_xkeen/02_install/07_install_register/00_register_common.sh').toString('utf8')
const deleted = source.get('_xkeen/03_delete/05_delete_register.sh').toString('utf8')
const registered = source.get('_xkeen/02_install/07_install_register/02_register_xkeen.sh').toString('utf8')
const architectureReader = source.get('_xkeen/01_info/08_info_router.sh').toString('utf8').split('\n').find(line => line.trim().startsWith('status_architecture='))
assert.ok(architectureReader?.includes("grep -m 1 '^Architecture:'"))
const fragments = common.slice(common.indexOf('write_opkg_status() {\n')) + '\n' +
  deleted.slice(deleted.indexOf('delete_register_xkeen() {\n')) + '\n' +
  registered.slice(registered.indexOf('register_xkeen_status() {\n'), registered.indexOf('register_xkeen_initd() {\n'))
function fixture(input) {
  const root = mkdtempSync('/tmp/native-package-registration-')
  const before = join(root, 'before'), after = join(root, 'status')
  writeFileSync(before, input); writeFileSync(after, input)
  const script = `status_file='${after}'; register_dir='${root}'; xkeen_current_version=2.0.1
${architectureReader}
awk -v mode=other -v version=2.0.1 -f scripts/native-update-packages.awk "$status_file" > '${root}/other-before' || exit $?
${fragments}
delete_register_xkeen
register_xkeen_status
fixed_register_packages
awk -v mode=other -v version=2.0.1 -f scripts/native-update-packages.awk "$status_file" > '${root}/other-after' || exit $?
cmp '${root}/other-before' '${root}/other-after' || exit 90
awk -v mode=identity -v version=2.0.1 -f scripts/native-update-packages.awk "$status_file"
`
  try {
    const result = spawnSync('/bin/sh', ['-c', script], { encoding: 'utf8', timeout: 2000 })
    assert.ifError(result.error)
    return { ...result, preserved: readFileSync(after, 'utf8') === input }
  } finally { rmSync(root, { recursive: true, force: true }) }
}
test('native delete/register/normalize preserves unrelated data and verifies exactly one replacement', () => {
  const r = fixture(other + '\n\n' + own + '\n')
  assert.equal(r.status, 0, r.stderr); assert.equal(r.stdout, 'xkeen 2.0.1 aarch64-fixture\n')
  assert.equal(r.preserved, false, 'native timestamp and stanza separator change is real')
})
test('preflight refuses native prefix hazard before any registration fragment can mutate metadata', () => {
  for (const input of [other + '\n' + own + '\n' + other.replace('Package: fixture', 'Package: xkeen-extra') + '\n',
    other.replace('Architecture: aarch64-fixture', 'Architecture: ') + '\n' + own + '\n']) {
    const r = fixture(input)
    assert.equal(r.status, 76, r.stderr); assert.equal(r.preserved, true); assert.equal(r.stdout, '')
  }
})
