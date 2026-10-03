// Pinned native no-migration/new-feature/cron guards, isolated public fixtures.
// Upstream fragments: jameszeroX, BSD-3-Clause; native-xkeen-stop-fix.LICENSE.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildUpdateProfile, readProfileDirectory } from './native-update-profile.mjs'

const source = readProfileDirectory(process.env.XKEEN_ADMISSION_PROFILE_ROOT)
buildUpdateProfile(source) // Complete pinned bytes, not merely a module name.
function fragment(path, first, last) {
  const text = source.get(path).toString('utf8'), begin = text.indexOf(first), end = last ? text.indexOf(last, begin) : text.length
  assert.ok(begin >= 0 && end > begin)
  return text.slice(begin, end)
}
const nativeNew = fragment('_xkeen/02_install/03_install_xkeen.sh', 'new_features() {\n')
const nativeCron = fragment('_xkeen/02_install/07_install_register/03_register_cron.sh', 'register_cron_initd() {\n', '\n# Обновление cron задач')
const nativeCronRewrite = fragment('_xkeen/02_install/07_install_register/03_register_cron.sh', 'update_cron_geofile_task() {\n')
const nativeConfig = fragment('_xkeen/02_install/07_install_register/02_register_xkeen.sh', 'create_xkeen_cfg() {\n')
const cronPayload = nativeCron.match(/script_content='([\s\S]*?)'\n/)[1]
// The pinned echo-e literal contains only doubled backslashes. No interpreter
// or arbitrary unescaping: derive its known native emitted script bytes.
assert.equal(cronPayload.replaceAll('\\\\', '').includes('\\'), false)
const nativeCronInit = cronPayload.replaceAll('\\\\', '\\') + '\n'
assert.equal(Buffer.byteLength(nativeCronInit), 1711)
assert.equal(createHash('sha256').update(nativeCronInit).digest('hex'), '516226b527a140fc733d349c42dd8e92b3182dbc7ac3df38c78e75bbb7a713e2')
const nativePorts = fragment('_xkeen/04_tools/01_tools_ports.sh', 'migrate_ports_from_initd() {\n')
const verifier = readFileSync('scripts/native-admission-verify.sh', 'utf8').split('\n_nv_main() {')[0]

function fixture({ setup = '', query = "printf 'cron - 1.0\nfixture - 1.0\n'", cron = '0 4 * * * /opt/sbin/xkeen -ug\n0 5 * * * /opt/sbin/xkeen -sbt\n', readerSetup = '', cronInit = null } = {}) {
  const ram = mkdtempSync('/tmp/native-update-env-'), code = mkdtempSync('/root/native-update-env-')
  chmodSync(ram, 0o700); chmodSync(code, 0o700)
  const path = name => join(code, name)
  const rewrite = value => value.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/etc/init.d', path('init.d')).replaceAll('/opt/etc/xkeen_exclude.lst', path('legacy-exclude'))
    .replaceAll('/opt/etc/xkeen', path('settings')).replaceAll('/opt/lib/opkg/status', path('status'))
    .replaceAll('/opt/var/spool/cron/crontabs', path('crontabs')).replaceAll('/opt/bin/opkg', path('bin/opkg'))
    .replaceAll('/opt/libexec/timeout-coreutils', path('bin/timeout')).replaceAll('@CODE@', code).replaceAll('@RAM@', ram)
  const put = (name, text, mode = 0o600) => writeFileSync(path(name), rewrite(text), { mode })
  for (const name of ['settings', 'settings/ipset', 'init.d', 'crontabs', 'bin']) mkdirSync(path(name), { mode: 0o700 })
  for (const name of ['native-operation-gate', 'native-admission-entry', 'native-update-context']) put(name, readFileSync(`scripts/${name}.sh`, 'utf8'))
  put('functions', verifier)
  put('settings/xkeen.json', '{}\n')
  for (const name of ['port_proxying.lst', 'port_exclude.lst', 'ip_exclude.lst']) put(`settings/${name}`, '# synthetic list\n')
  put('status', 'Package: cron\nVersion: 1.0\nStatus: install ok installed\n\nPackage: fixture\nVersion: 1.0\nStatus: install ok installed\n\n')
  if (cron !== null) writeFileSync(path('crontabs/root'), cron, { mode: 0o600 })
  if (cronInit !== null) writeFileSync(path('init.d/S05crond'), cronInit, { mode: 0o600 })
  put('bin/opkg', `#!/bin/sh
[ "$*" = list-installed ] || exit 99
${query}
`, 0o700)
  put('bin/timeout', '#!/bin/sh\n[ "$1 $2 $3" = "-s KILL 15" ] || exit 99\nshift 3\nexec "$@"\n', 0o700)
  put('reader', `. '@CODE@/native-operation-gate'; . '@CODE@/native-admission-entry'; . '@CODE@/native-update-context'; . '@CODE@/functions'
${readerSetup}
_nv_update_environment_pre || exit $?
# Actual pinned native functions; callbacks trace any forbidden effect branch.
PATH='@CODE@/bin':$PATH; export PATH
ipset_cfg='@CODE@/settings/ipset'; initd_cron='@CODE@/init.d/S05crond'
cron_dir='@CODE@/crontabs'; cron_file=root; install_dir=/opt/sbin
xkeen_cfg='@CODE@/settings'; xkeen_config="$xkeen_cfg/xkeen.json"
file_port_proxying="$xkeen_cfg/port_proxying.lst"; file_port_exclude="$xkeen_cfg/port_exclude.lst"; file_ip_exclude="$xkeen_cfg/ip_exclude.lst"
test_github() { echo NETWORK >> '@CODE@/effects'; }
smart_clear() { :; }
install_geoipset() { echo INSTALL >> '@CODE@/effects'; }
${nativeNew}
${nativeCron}
${nativeCronRewrite}
${nativeConfig}
${nativePorts}
new_features; [ "$?" = 0 ] || exit 90
register_cron_initd || exit 91
migrate_ports_from_initd; [ "$?" = 1 ] || exit 92
create_xkeen_cfg || exit 94
update_cron_geofile_task || exit 95
[ "$(_nv_update_environment_inputs)" = "$_nv_env_before" ] || exit 96
[ ! -s '@CODE@/effects' ] || exit 93
printf '%s\\n' "$_nv_env_digest"
`)
  put('wrapper', `. '@CODE@/native-operation-gate'
native_gate_acquire '@RAM@' update-xkeen || exit $?
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@RAM@/operation.lock.d/call.update'
(umask 077; printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@RAM@/operation.lock.d/call.update/context')
${setup}
/bin/sh '@CODE@/reader'
`)
  try {
    const result = spawnSync('/bin/sh', [path('wrapper')], { encoding: 'utf8', timeout: 4000 })
    assert.ifError(result.error)
    return { ...result, query: existsSync(join(ram, 'operation.lock.d/call.update/environment.pre')), held: existsSync(join(ram, 'operation.lock.d/owner')), effects: existsSync(path('effects')), cronInit: existsSync(path('init.d/S05crond')) }
  } finally { rmSync(ram, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('supported environment makes actual native feature/cron/migration functions no-op', () => {
  for (const cron of [null, '', '0 4 * * * /opt/sbin/xkeen -ug\n0 5 * * * /opt/sbin/xkeen -sbt\n']) {
    const r = fixture({ cron }); assert.equal(r.status, 0, r.stderr); assert.match(r.stdout, /^[a-f0-9]{64}\n$/)
    assert.equal(r.effects || r.cronInit, false); assert.equal(r.query && r.held, true)
  }
})
test('native script cron without a cron package preserves exact upstream init and tasks', () => {
  const r = fixture({ query: "printf 'fixture - 1.0\\n'", cronInit: nativeCronInit })
  assert.equal(r.status, 0, r.stderr); assert.match(r.stdout, /^[a-f0-9]{64}\n$/); assert.equal(r.effects, false); assert.equal(r.cronInit, true)
  for (const cronInit of [null, nativeCronInit + '# manual drift\n', nativeCronInit.replace('version="0.6"', 'version="0.5"')]) {
    const bad = fixture({ query: "printf 'fixture - 1.0\\n'", cronInit }); assert.equal(bad.status, 76); assert.equal(bad.stdout, ''); assert.equal(bad.query && bad.held, true)
  }
})
test('legacy/new-feature/missing native configuration refuses before package query or body', () => {
  for (const setup of [
    "rmdir '@CODE@/settings/ipset'", "chmod 777 '@CODE@/settings/ipset'",
    ...['S99xkeen', 'S24xray', 'S99xkeenstart'].flatMap(name => [`touch '@CODE@/init.d/${name}'`, `ln -s '@CODE@/absent' '@CODE@/init.d/${name}'`]),
    "touch '@CODE@/legacy-exclude'", "ln -s '@CODE@/absent' '@CODE@/legacy-exclude'",
    ...['xkeen.json', 'port_proxying.lst', 'port_exclude.lst', 'ip_exclude.lst'].map(name => `rm '@CODE@/settings/${name}'`),
    "printf 'false\\n' > '@CODE@/settings/xkeen.json'", "printf '{\"xkeen\":false}\\n' > '@CODE@/settings/xkeen.json'",
  ]) {
    const r = fixture({ setup }); assert.equal(r.status, 76, r.stderr); assert.equal(r.query || r.effects || r.cronInit, false); assert.equal(r.stdout, '')
  }
})
test('cron removal matches and unsafe/nonterminated crontab refuse before body', () => {
  for (const suffix of ['ugi', 'ugs', 'ux', 'uk', 'uk_post_update']) for (const prefix of ['0 4 * * * ', '# ']) {
    const r = fixture({ cron: `${prefix}/opt/sbin/xkeen -${suffix}\n` }); assert.equal(r.status, 76, r.stderr); assert.equal(r.query || r.effects, false)
  }
  for (const options of [{ cron: '0 4 * * * /opt/sbin/xkeen -ug' }, { setup: "chmod 666 '@CODE@/crontabs/root'" }, { setup: "mv '@CODE@/crontabs/root' '@CODE@/saved'; ln -s '@CODE@/saved' '@CODE@/crontabs/root'" }]) {
    const r = fixture(options); assert.equal(r.status, 76, r.stderr); assert.equal(r.query || r.effects, false)
  }
})
test('missing/duplicate/partial/failed/oversized package results retain query and admission', () => {
  for (const query of [
    "printf 'fixture - 1.0\\n'", "printf 'cron - 1.0\\ncron - 2.0\\n'", "printf 'cron - 1.0\\n'; exit 3",
    'exit 3', "printf 'cron - 1.0\\nmalformed\\n'", 'head -c 1048576 /dev/zero',
  ]) {
    const r = fixture({ query }); assert.equal(r.status, 76, r.stderr); assert.equal(r.stdout, ''); assert.equal(r.query && r.held, true); assert.equal(r.effects || r.cronInit, false)
  }
})
test('configuration/status/cron or capability protection drift during query refuses', () => {
  for (const change of [
    "printf '# changed\\n' >> '@CODE@/settings/ip_exclude.lst'", "printf '\\nPackage: extra\\n' >> '@CODE@/status'",
    "printf '# changed\\n' >> '@CODE@/crontabs/root'", "chmod 666 '@CODE@/status'", "chmod 777 '@CODE@/crontabs'",
    "chmod 666 '@CODE@/bin/timeout'", "rm '@CODE@/crontabs/root'",
  ]) {
    const r = fixture({ query: `${change}\nprintf 'cron - 1.0\\nfixture - 1.0\\n'` }); assert.equal(r.status, 77, r.stderr); assert.equal(r.stdout, ''); assert.equal(r.query && r.held, true)
  }
})
