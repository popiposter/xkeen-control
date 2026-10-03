// Real isolated body/exec/gate plus actual pinned native package classifier.
// Package manager and timeout are synthetic commands, never installers.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildUpdateProfile, readProfileDirectory } from './native-update-profile.mjs'

const source = readProfileDirectory(process.env.XKEEN_ADMISSION_PROFILE_ROOT)
const built = buildUpdateProfile(source)
const packages = ['curl', 'jq', 'ip-full', 'iptables', 'ipset', 'ca-bundle', 'coreutils-uname', 'coreutils-nohup', 'conntrack']
const query = packages.map(name => `${name} - 1.0\n`).join('')
const status = packages.map(name => `Package: ${name}\nVersion: 1.0\nStatus: install ok installed\n\n`).join('')
const sha = value => createHash('sha256').update(value).digest('hex')
const packageOverlay = built.overlays.get('_xkeen/01_info/02_info_packages.sh').toString('utf8').split('# END SOURCE-ONLY FENCE\n')[1]
const dispatcher = built.overlays.get('xkeen').toString('utf8')
const begin = dispatcher.indexOf('case "$1" in\n', dispatcher.indexOf('# Self-heal'))
const end = dispatcher.indexOf('\nesac\n', begin)
assert.ok(begin > 0 && end > begin)
const nativePrefix = dispatcher.slice(begin, end + '\nesac\n'.length)
const cronModule = built.overlays.get('_xkeen/02_install/07_install_register/03_register_cron.sh').toString().split('# END SOURCE-ONLY FENCE\n')[1]
const cronPayload = source.get('_xkeen/02_install/07_install_register/03_register_cron.sh').toString().match(/script_content='([\s\S]*?)'\n/)[1]
assert.equal(cronPayload.replaceAll('\\\\', '').includes('\\'), false)
const cronInit = cronPayload.replaceAll('\\\\', '\\') + '\n'
assert.equal(sha(cronInit), '516226b527a140fc733d349c42dd8e92b3182dbc7ac3df38c78e75bbb7a713e2')

function fixture({ output = null, before = '', initial = '', post = '', timeout = '', cron = false, cronPackage = false, cronBefore = '' } = {}) {
  output ??= `printf '${query}${cronPackage ? 'cron - 1.0\n' : ''}'`
  const code = mkdtempSync('/root/native-prefix-packages-'), ram = mkdtempSync('/tmp/native-prefix-packages-')
  chmodSync(code, 0o700); chmodSync(ram, 0o700)
  const path = name => join(code, name)
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', path('gate'))
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', path('entry'))
    .replaceAll('/opt/lib/xkeen/native-update-context.sh', path('context'))
    .replaceAll('/opt/lib/opkg/status', path('status')).replaceAll('/opt/etc/opkg.conf', path('opkg.conf'))
    .replaceAll('/opt/etc/init.d/S05crond', path('cron.init'))
    .replaceAll('/opt/libexec/timeout-coreutils', path('timeout')).replaceAll('/opt/bin/opkg', path('opkg'))
    .replaceAll('/opt/sbin/.xkeen.stage.', `${code}/.xkeen.stage.`).replaceAll('/opt/sbin/xkeen', path('xkeen'))
    .replaceAll('/opt/bin/sh', '/bin/sh')
    .replaceAll('f7549ee949d9d02fa5ba2e6586f286394c31c2243d32d7ed1b520aa37d5439fe', sha(Buffer.from(`/bin/sh\0${path('xkeen')}\0-uk_post_update\0`)))
    .replaceAll('@CODE@', code).replaceAll('@RAM@', ram)
  const put = (name, text, mode = 0o600) => writeFileSync(path(name), rewrite(text), { mode })
  const libraries = ". '@CODE@/gate'; . '@CODE@/entry'; . '@CODE@/context'"
  for (const [name, file] of [['gate', 'native-operation-gate'], ['entry', 'native-admission-entry'], ['context', 'native-update-context']]) put(name, readFileSync(`scripts/${file}.sh`, 'utf8'))
  put('status', status + (cronPackage ? 'Package: cron\nVersion: 1.0\nStatus: install ok installed\n\n' : '')); put('opkg.conf', '# fixture config\n')
  if (cron) { put('cron', cronModule); put('cron.init', cronInit) }
  put('packages', packageOverlay)
  put('install', source.get('_xkeen/02_install/01_install_packages.sh').toString('utf8'))
  put('opkg', `#!/bin/sh
[ -z "\${XKEEN_GATE_TOKEN+x}\${XKEEN_ADMISSION_ROLE+x}\${XKEEN_ADMISSION_CALL+x}" ] || exit 91
[ "$*" = list-installed ] || { echo INSTALL >> '@CODE@/effects'; exit 90; }
echo QUERY >> '@CODE@/effects'
${output}
`, 0o700)
  put('timeout', `#!/bin/sh
[ "$1 $2 $3" = '-s KILL 15' ] || exit 90
${timeout}
shift 3; exec "$@"
`, 0o700)
  const prefix = `. '@CODE@/packages'; . '@CODE@/install'
${nativePrefix}
echo ENSURED >> '@CODE@/effects'
`
  put('xkeen', `${libraries}
native_update_exec_context || exit $?
${post}
${prefix}
${cron ? `. '@CODE@/cron'
${cronBefore}
register_cron_initd || exit $?
echo CRON-NOOP >> '@CODE@/effects'` : ''}
native_update_complete 0
exit $?
`)
  put('stage', `${libraries}
native_update_bind_staged "$stage"
`)
  put('deeper', `${libraries}
native_update_packages_cache
`)
  put('deeper-cron', `${libraries}\nnative_update_cron_noop\n`)
  put('body', `${libraries}
native_update_bind_body || exit $?
${initial}
${prefix}
stage="/opt/sbin/.xkeen.stage.$_ng_self_pid"; export stage
mkdir -m 700 "$stage"; cp '@CODE@/xkeen' "$stage/xkeen"; chmod 600 "$stage/xkeen"
/bin/sh '@CODE@/stage' || exit $?
exec /bin/sh '@CODE@/xkeen' -uk_post_update
`)
  put('wrapper', `${libraries}
native_gate_acquire '@RAM@' update-xkeen || exit $?
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@RAM@/operation.lock.d/call.update'
printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@RAM@/operation.lock.d/call.update/context'
chmod 600 '@RAM@/operation.lock.d/call.update/context'
${before}
/bin/sh '@CODE@/body' -uk
`)
  try {
    const r = spawnSync('/bin/sh', [path('wrapper')], { encoding: 'utf8', timeout: 5000 })
    assert.ifError(r.error)
    return { ...r, effects: existsSync(path('effects')) ? readFileSync(path('effects'), 'utf8') : '', held: existsSync(join(ram, 'operation.lock.d/owner')), initial: existsSync(join(ram, 'operation.lock.d/call.update/packages.initial')), post: existsSync(join(ram, 'operation.lock.d/call.update/packages.post')), completed: existsSync(join(ram, 'operation.lock.d/call.update/completed')) }
  } finally { rmSync(ram, { recursive: true, force: true }); rmSync(code, { recursive: true, force: true }) }
}

test('actual native classifier and ensure preserve installed packages through real body exec', () => {
  const r = fixture(); assert.equal(r.status, 0, JSON.stringify(r))
  assert.equal(r.effects, 'QUERY\nENSURED\nQUERY\nENSURED\n')
  assert.equal(r.held && r.initial && r.post && r.completed, true)
})
test('failed, timed-out, truncated, duplicate and missing reload never reach native ensure', () => {
  for (const options of [{ output: 'exit 3' }, { timeout: 'exit 124' },
    { output: `printf '${query.trimEnd()}'` }, { output: `printf '${query}curl - 1.0\n'` },
    { output: `printf '${query.replace('curl - 1.0\n', '')}'` }, { output: 'head -c 1048576 /dev/zero' },
  ]) {
    const r = fixture(options); assert.equal(r.status, 1, JSON.stringify(r))
    assert.equal(r.effects.includes('ENSURED') || r.effects.includes('INSTALL') || r.completed, false); assert.equal(r.held && r.initial, true)
  }
})
test('deeper child, wrong phase, replay and malformed update hints cannot borrow cache authority', () => {
  for (const initial of ["/bin/sh '@CODE@/deeper' || exit $?", "mkdir -m 700 '@RAM@/operation.lock.d/call.update/exec.used'",
    `IFS= read -r body < '@RAM@/operation.lock.d/call.update/body'
hash=$(sha256sum '@CODE@/xkeen'); hash=\${hash%% *}
printf '%s %s\\n' "$body" "$hash" > '@RAM@/operation.lock.d/call.update/staged'
mkdir -m 700 '@RAM@/operation.lock.d/call.update/exec.used'
printf '/bin/%s\\000@CODE@/%s\\000-uk_post_update\\000' sh xkeen > '@RAM@/operation.lock.d/call.update/exec.argv'
chmod 600 '@RAM@/operation.lock.d/call.update/exec.argv'`,
    'native_update_packages_cache || exit $?', 'XKEEN_ADMISSION_ACTION=wrong; export XKEEN_ADMISSION_ACTION']) {
    const r = fixture({ initial }); assert.notEqual(r.status, 0, JSON.stringify(r))
    assert.equal(r.effects.includes('ENSURED') || r.effects.includes('INSTALL') || r.completed, false); assert.equal(r.held, true)
  }
})
test('owner or status/config/query protection drift retains evidence and refuses ensure', () => {
  for (const change of ["printf 'v1 changed\\n' > '@RAM@/operation.lock.d/owner'",
    `. '@CODE@/gate'
IFS= read -r saved < '@RAM@/operation.lock.d/owner'; set -- $saved
boot=$2; owner=$3; token=$5; action=$6
_native_gate_proc "$owner" || exit 90
ancestor=$_ng_parent; _native_gate_proc "$ancestor" || exit 91
printf 'v1 %s %s %s %s %s\\n' "$boot" "$ancestor" "$_ng_proc_start" "$token" "$action" > '@RAM@/operation.lock.d/owner'`,
    "printf '# drift\\n' >> '@CODE@/opkg.conf'", "chmod 666 '@CODE@/status'",
    "chmod 644 '@RAM@/operation.lock.d/call.update/packages.initial/packages'",
  ]) {
    const r = fixture({ output: `${change}\nprintf '${query}'` }); assert.equal(r.status, 1, JSON.stringify(r))
    assert.equal(r.held && r.initial, true); assert.equal(r.effects.includes('ENSURED') || r.effects.includes('INSTALL') || r.completed, false)
  }
})

test('late drift while copying the accepted cache and failed post-exec reload block completion', () => {
  const late = fixture({ initial: `cat() {
command cat "$@" || return $?
chmod 644 '@RAM@/operation.lock.d/call.update/packages.initial/packages'
}` })
  assert.equal(late.status, 1, JSON.stringify(late)); assert.equal(late.effects, 'QUERY\n'); assert.equal(late.completed, false)
  const failedPost = fixture({ output: `[ ! -d '@RAM@/operation.lock.d/call.update/packages.post' ] || exit 3
printf '${query}'` })
  assert.equal(failedPost.status, 1, JSON.stringify(failedPost)); assert.equal(failedPost.effects, 'QUERY\nENSURED\nQUERY\n')
  assert.equal(failedPost.held && failedPost.initial && failedPost.post, true); assert.equal(failedPost.completed, false)
  const failedArgv = fixture({ post: 'dd() { command dd "$@"; return 5; }' })
  assert.equal(failedArgv.status, 1, JSON.stringify(failedArgv)); assert.equal(failedArgv.effects, 'QUERY\nENSURED\n')
  assert.equal(failedArgv.held && failedArgv.post, true); assert.equal(failedArgv.completed, false)
})

test('drift after the final dependency parser refuses before native ensure', () => {
  for (const change of ["chmod 666 '@CODE@/status'", "printf '# drift\\n' >> '@CODE@/opkg.conf'",
    "chmod 644 '@CODE@/opkg.conf'", "chmod 644 '@RAM@/operation.lock.d/call.update/packages.initial/packages'",
    "printf 'fake - 1.0\\n' >> '@RAM@/operation.lock.d/call.update/packages.initial/packages'",
  ]) {
    const r = fixture({ initial: `awk() {
command awk "$@" || return $?
case "\${2-}" in query=*)
  dep_reads=$((\${dep_reads:-0}+1))
  if [ "$dep_reads" = 2 ]; then ${change}; fi
esac
}` })
    assert.equal(r.status, 1, JSON.stringify(r)); assert.equal(r.effects, 'QUERY\n')
    assert.equal(r.held && r.initial, true); assert.equal(r.completed, false)
  }
})

test('actual admitted native script update uses GitHub and leaves Entware feed calls untouched elsewhere', () => {
  const original = source.get('xkeen').toString('utf8')
  assert.equal(original.match(/^            test_entware$/gm).length, 3)
  assert.equal(dispatcher.match(/^            test_entware$/gm).length, 2)
  const first = dispatcher.indexOf('        -uk)    # Обновление XKeen\n')
  const last = dispatcher.indexOf('        -uk_post_update)\n', first)
  const r = spawnSync('/bin/sh', ['-c', `
test_connection() { echo CONNECTION; }; check_health() { echo HEALTH; }
test_entware() { echo FORBIDDEN_FEED; exit 97; }; opkg() { echo FORBIDDEN_OPKG; exit 98; }
test_github() { echo GITHUB; }; sleep() { :; }; smart_clear() { :; }
xkeen_info() { :; }; xkeen_set_info() { :; }; backup_xkeen() { echo BACKUP; }
download_xkeen_dev() { echo DOWNLOAD; }; install_xkeen() { echo ARCHIVE_INSTALL; return 1; }
delete_tmp() { echo CLEANUP; }; xkeen_build=Dev
case -uk in
${dispatcher.slice(first, last)}
esac
`], { encoding: 'utf8', timeout: 1000 })
  assert.ifError(r.error); assert.equal(r.status, 1, r.stderr)
  assert.ok(r.stdout.includes('CONNECTION\nHEALTH\nGITHUB\n'))
  assert.ok(r.stdout.includes('BACKUP\nDOWNLOAD\nARCHIVE_INSTALL\n'))
  assert.equal(r.stdout.includes('FORBIDDEN'), false)
})
test('native cron registration reuses post-body cache without any third package query', () => {
  for (const cronPackage of [false, true]) {
    const r = fixture({ cron: true, cronPackage })
    assert.equal(r.status, 0, JSON.stringify(r))
    assert.equal(r.effects, 'QUERY\nENSURED\nQUERY\nENSURED\nCRON-NOOP\n')
    assert.equal(r.held && r.completed, true)
  }
  const absent = fixture({ cron: true, cronPackage: true, cronBefore: "rm '@CODE@/cron.init'" })
  assert.equal(absent.status, 0, JSON.stringify(absent))
})
test('missing or changed no-package cron script, body/cache/input drift cannot complete', () => {
  for (const cronBefore of [
    "rm '@CODE@/cron.init'", "printf '# version=0.6\\n' > '@CODE@/cron.init'", "chmod 666 '@CODE@/cron.init'",
    "mv '@CODE@/cron.init' '@CODE@/cron.real'; ln -s cron.real '@CODE@/cron.init'",
    "chmod 644 '@RAM@/operation.lock.d/call.update/packages.post/packages'",
    "printf 'foreign - 1.0\\n' >> '@RAM@/operation.lock.d/call.update/packages.post/packages'",
    "printf '# drift\\n' >> '@CODE@/opkg.conf'", 'unset _nu_pkg_query_hash',
    'XKEEN_ADMISSION_ACTION=wrong; export XKEEN_ADMISSION_ACTION',
    "printf 'v1 changed\\n' > '@RAM@/operation.lock.d/call.update/body'",
    "/bin/sh '@CODE@/deeper-cron' || exit $?",
  ]) {
    const r = fixture({ cron: true, cronBefore })
    assert.notEqual(r.status, 0, JSON.stringify(r))
    assert.equal(r.effects, 'QUERY\nENSURED\nQUERY\nENSURED\n')
    assert.equal(r.held && r.post, true); assert.equal(r.completed, false)
  }
})
test('late cron count/hash reader failures and protection/content drift retain admission', () => {
  for (const cronBefore of [
    `awk() { case "$1" in *'count++'*) return 7;; esac; command awk "$@"; }`,
    `awk() { command awk "$@" || return $?; case "$1" in *'count++'*) chmod 666 '@CODE@/cron.init';; esac; }`,
    `awk() { command awk "$@" || return $?; case "$1" in *'count++'*) printf 'v1 changed\\n' > '@RAM@/operation.lock.d/call.update/body';; esac; }`,
    `sha256sum() { command sha256sum "$@" || return $?; if [ "$1" = '@RAM@/operation.lock.d/call.update/packages.post/packages' ]; then
      count=0; [ ! -f '@CODE@/cron-hash-count' ] || read -r count < '@CODE@/cron-hash-count'
      count=$((count+1)); printf '%s\\n' "$count" > '@CODE@/cron-hash-count'
      if [ "$count" = 2 ]; then chmod 644 "$1"; fi
    fi; }`,
    `sha256sum() { command sha256sum "$@" || return $?; if [ "$1" = '@RAM@/operation.lock.d/call.update/packages.post/packages' ]; then
      count=0; [ ! -f '@CODE@/cron-hash-count' ] || read -r count < '@CODE@/cron-hash-count'
      count=$((count+1)); printf '%s\\n' "$count" > '@CODE@/cron-hash-count'
      if [ "$count" = 2 ]; then sed -i 's/curl - 1.0/curl - 2.0/' "$1"; fi
    fi; }`,
    `cat() { command cat "$@" || return $?; if [ "$1" = '@RAM@/operation.lock.d/call.update/packages.post/packages' ]; then
      count=0; [ ! -f '@CODE@/cron-cat-count' ] || read -r count < '@CODE@/cron-cat-count'
      count=$((count+1)); printf '%s\\n' "$count" > '@CODE@/cron-cat-count'
      if [ "$count" = 2 ]; then sed -i 's/curl - 1.0/curl - 2.0/' "$1"; fi
    fi; }`,
  ]) {
    const r = fixture({ cron: true, cronBefore })
    assert.notEqual(r.status, 0, JSON.stringify(r))
    assert.equal(r.effects.includes('CRON-NOOP') || r.completed, false)
    assert.equal(r.held && r.post, true)
  }
})
test('initial body cannot use post-phase cron no-op proof', () => {
  const r = fixture({ cron: true, initial: 'native_update_cron_noop || exit $?' })
  assert.notEqual(r.status, 0, JSON.stringify(r))
  assert.equal(r.effects, '')
  assert.equal(r.held, true); assert.equal(r.post || r.completed, false)
})
