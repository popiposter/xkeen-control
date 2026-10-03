// Data-only opkg projection; no package/service commands or router access.
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

export const own = 'Package: xkeen\nVersion: 2.0.1\nDepends: jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack\nStatus: install user installed\nArchitecture: aarch64-fixture\nInstalled-Time: 1720000000\n'
const other = 'Package: fixture\nVersion: 1.0\nStatus: install ok installed\nArchitecture: aarch64-fixture\nDescription: preserved unknown fields\n continuation: not a header\nUnknown-Field: literal value\n'
function project(input, mode = 'other', version = '2.0.1', command = 'awk') {
  const r = spawnSync(command, ['-v', `mode=${mode}`, '-v', `version=${version}`, '-f', 'scripts/native-update-packages.awk'], { input, encoding: 'utf8', timeout: 2000 })
  assert.ifError(r.error); return r
}
function ok(input, mode) { const r = project(input, mode); assert.equal(r.status, 0, r.stderr); return r.stdout }

test('unrelated paragraph bytes/order survive native separator normalization and own registration moves', () => {
  const second = other.replace('Package: fixture', 'Package: second')
  const before = `${other}\n\n${own}\n${second}\n`
  const after = `${other}\n${second}\n\n\n${own.replace('1720000000', '1720000001')}\n`
  assert.equal(ok(before), `${other}\n${second}\n`)
  assert.equal(ok(after), ok(before))
  assert.equal(ok(before, 'identity'), 'xkeen 2.0.1 aarch64-fixture\n')
  assert.equal(ok(after, 'identity'), ok(before, 'identity'))
  assert.notEqual(ok(before.replace('literal value', 'changed value')), ok(before))
})
test('native prefix removal hazards, duplicate and missing package stanzas refuse without output', () => {
  for (const input of [other, own + '\n' + own, own + '\n' + other + '\n' + other,
    own + '\n' + other.replace('Package: fixture', 'Package: xkeen-extra'),
    own + '\n' + other.replace('Package: fixture', 'Package: xkeenx'),
    other.replace('Package: fixture\n', '') + '\n' + own,
  ]) { const r = project(input); assert.equal(r.status, 76); assert.equal(r.stdout, '') }
})
test('unknown native metadata, malformed status and changed architecture refuse without projection', () => {
  for (const input of [
    own.replace('2.0.1', '2.0.2'), own.replace('install user installed', 'install ok installed'),
    own.replace('Depends: jq, ', 'Depends: '), own + 'Extra: unsupported\n',
    own.replace('Installed-Time: 1720000000', 'Installed-Time: $(id)'),
    own.replace('Installed-Time: 1720000000', 'Installed-Time: 0001'),
    own.replace('Architecture: aarch64-fixture\n', ''), own.replace('Version: 2.0.1', 'Version: 2.0.1\nVersion: 2.0.1'),
    own.replaceAll('\n', '\r\n'), own + '\n' + 'not a package\n', own.replace('aarch64-fixture', 'aarch64\0-fixture'),
    other.replace('Architecture: aarch64-fixture', 'Architecture: all') + '\n' + own,
    other.replace('Architecture: aarch64-fixture', 'Architecture: ') + '\n' + own,
  ]) { const r = project(input); assert.equal(r.status, 76, input); assert.equal(r.stdout, '') }
  for (const [mode, version] of [['unknown', '2.0.1'], ['other', '2.0.2']]) {
    const r = project(own, mode, version); assert.equal(r.status, 76); assert.equal(r.stdout, '')
  }
})
test('bounded malformed/oversized stanza and package inventories do not emit partial proof', () => {
  for (const input of [own + '\n' + other.replace('literal value', 'x'.repeat(8200)),
    own + '\n' + Array.from({ length: 512 }, (_, i) => other.replace('Package: fixture', `Package: fixture-${i}`)).join('\n'),
    own + '\n' + other.replace('literal value', 'x'.repeat(66000)),
  ]) { const r = project(input); assert.equal(r.status, 76); assert.equal(r.stdout, '') }
})
