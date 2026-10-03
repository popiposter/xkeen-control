// Complete update verifier composition on protected disposable files/RAM.
// Real gate, ancestry, init/package/environment readers. Synthetic core/proc,
// profile and kernel callbacks; no complete native updater or router executes.
import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'
import { buildCandidates } from './native-admission-patch.mjs'

const built = buildCandidates({ init: readFileSync(process.env.XKEEN_ADMISSION_INIT), dispatcher: readFileSync(process.env.XKEEN_ADMISSION_DISPATCHER) })
const init = built.registrationTemplate.toString('utf8')
const status = 'Package: cron\nVersion: 1.0\nArchitecture: aarch64-fixture\nStatus: install ok installed\n\nPackage: xkeen\nVersion: 2.0.1\nDepends: jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack\nStatus: install user installed\nArchitecture: aarch64-fixture\nInstalled-Time: 1720000000\n'
const control = 'Package: xkeen\nVersion: 2.0.1\nDepends: jq, curl, coreutils-uname, coreutils-nohup, iptables, ipset, ip-full, conntrack\nSource: Skrill\nSourceName: xkeen\nSection: net\nSourceDateEpoch: 1720000000\nMaintainer: Skrill / jameszero\nArchitecture: aarch64-fixture\nInstalled-Size: 1234\nDescription: The platform that makes Xray work.\n'

function fixture({ running = true, auto = 'on', before = '', between = '', profile = ':', hook = ':', validator = ':', terminal = true, nested = false, publicationChange = '' } = {}) {
  const code = mkdtempSync('/root/native-update-verify-'), ram = mkdtempSync('/tmp/native-update-verify-')
  chmodSync(code, 0o700); chmodSync(ram, 0o700)
  const path = name => join(code, name)
  for (const name of ['settings', 'settings/ipset', 'init.d', 'crontabs', 'configs', 'bin', 'proc', 'proc/123']) mkdirSync(path(name), { mode: 0o700 })
  const rewrite = text => text.replaceAll('/tmp/.xkeen-admission', ram)
    .replaceAll('/opt/lib/xkeen/native-profile-v1/overlay-1.disabled.sh', path('template'))
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', path('gate'))
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', path('entry'))
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', path('verify'))
    .replaceAll('/opt/lib/xkeen/native-update-context.sh', path('context'))
    .replaceAll('/opt/lib/xkeen/native-admission-hook-verify.sh', path('hook'))
    .replaceAll('/opt/lib/xkeen/native-update-profile-check.sh', path('profile'))
    .replaceAll('/opt/lib/xkeen/native-update-init.awk', path('init-parser'))
    .replaceAll('/opt/lib/xkeen/native-update-packages.awk', path('package-parser'))
    .replaceAll('/opt/etc/xkeen-control/secrets/previous/.pending', path('legacy-pending'))
    .replaceAll('/opt/etc/xkeen-control/previous/.pending', path('pending'))
    .replaceAll('/opt/etc/xkeen_exclude.lst', path('legacy-exclude'))
    .replaceAll('/opt/etc/init.d', path('init.d'))
    .replaceAll('/opt/etc/xkeen', path('settings'))
    .replaceAll('/opt/etc/xray/configs', path('configs'))
    .replaceAll('/opt/etc/xray/dat', path('assets'))
    .replaceAll('/opt/lib/opkg/info/xkeen.control', path('control'))
    .replaceAll('/opt/lib/opkg/status', path('status'))
    .replaceAll('/opt/var/spool/cron/crontabs', path('crontabs'))
    .replaceAll('/opt/libexec/timeout-coreutils', path('bin/timeout'))
    .replaceAll('/opt/bin/opkg', path('bin/opkg'))
    .replaceAll('/opt/bin/pidof', path('bin/pidof'))
    .replaceAll('/opt/sbin/xray', path('core'))
    .replaceAll('/opt/sbin/xkeen', path('dispatcher'))
    .replaceAll('/opt/bin/sh', '/bin/sh')
    .replaceAll('@CODE@', code).replaceAll('@RAM@', ram)
  const put = (name, text, mode = 0o600) => writeFileSync(path(name), rewrite(text), { mode })
  put('gate', readFileSync('scripts/native-operation-gate.sh', 'utf8'))
  put('entry', readFileSync('scripts/native-admission-entry.sh', 'utf8'))
  put('context', readFileSync('scripts/native-update-context.sh', 'utf8'))
  // Only core /proc is synthetic; gate/context ancestry still uses real /proc.
  let verifier = readFileSync('scripts/native-admission-verify.sh', 'utf8').replaceAll('/proc/', path('proc/'))
  if (publicationChange) verifier = verifier.replace('\n_nv_main "$@"', `
printf() {
  command printf "$@" || return $?
  case "\${2-}" in v2*) ${publicationChange};; esac
}
_nv_main "$@"`)
  put('verify', verifier)
  writeFileSync(path('template'), init, { mode: 0o600 })
  writeFileSync(path('init.d/S05xkeen'), init.replace(/^start_auto=.*$/m, `start_auto="${auto}"`), { mode: 0o600 })
  put('init-parser', readFileSync('scripts/native-update-init.awk', 'utf8'))
  put('package-parser', readFileSync('scripts/native-update-packages.awk', 'utf8'))
  put('status', status); put('control', control)
  put('settings/xkeen.json', '{}\n'); put('configs/01.json', '{}\n')
  for (const name of ['port_proxying.lst', 'port_exclude.lst', 'ip_exclude.lst']) put(`settings/${name}`, '# fixture\n')
  put('crontabs/root', '# unrelated cron\n')
  put('pids', running ? '123\n' : '')
  put('proc/123/stat', `123 (xray) S ${['1', ...Array(17).fill('0')].join(' ')} 42 0\n`)
  writeFileSync(path('proc/123/cmdline'), Buffer.from('xray\0run\0'), { mode: 0o600 })
  writeFileSync(path('proc/123/environ'), Buffer.from(`XRAY_LOCATION_CONFDIR=${path('configs')}\0XRAY_LOCATION_ASSET=${path('assets')}\0`), { mode: 0o600 })
  symlinkSync(path('core'), path('proc/123/exe'))
  put('core', `#!/bin/sh
[ "$#:$1:$2:$3" = 4:run:-test:-confdir ] || exit 90
[ -z "\${XKEEN_GATE_TOKEN+x}" ] && [ -z "\${XKEEN_ADMISSION_CALL+x}" ] || exit 91
echo VALIDATE >> '@CODE@/validation-calls'
${validator}
`, 0o700)
  put('bin/pidof', "#!/bin/sh\n[ -s '@CODE@/pids' ] || exit 1\ncat '@CODE@/pids'\n", 0o700)
  put('bin/opkg', "#!/bin/sh\n[ \"$*\" = list-installed ] || exit 90\nprintf 'cron - 1.0\\nfixture - 1.0\\n'\n", 0o700)
  put('bin/timeout', '#!/bin/sh\n[ "$1 $2 $3" = "-s KILL 15" ] || exit 90\nshift 3\nexec "$@"\n', 0o700)
  const authenticated = `. '@CODE@/gate'; . '@CODE@/entry'; . '@CODE@/context'
native_update_observer_context "$1" || exit $?
`
  put('profile', `${authenticated}echo "$*" >> '@CODE@/profile-calls'
${profile}
`)
  put('hook', `${authenticated}echo "$*" >> '@CODE@/hook-calls'
${hook}
`)
  put('dispatcher', '# synthetic dispatcher identity, never executed\n')
  put('wrapper', `. '@CODE@/gate'
native_gate_acquire '@RAM@' update-xkeen || exit $?
XKEEN_ADMISSION_ROLE=update; XKEEN_ADMISSION_ACTION=update-xkeen; XKEEN_ADMISSION_CALL=0123456789abcdef0123456789abcdef
export XKEEN_ADMISSION_ROLE XKEEN_ADMISSION_ACTION XKEEN_ADMISSION_CALL
mkdir -m 700 '@RAM@/operation.lock.d/call.update'
printf 'v1 %s %s %s update update-xkeen forced %s\\n' "$_ng_self_pid" "$_ng_self_start" "$XKEEN_ADMISSION_CALL" "$XKEEN_GATE_TOKEN" > '@RAM@/operation.lock.d/call.update/context'
chmod 600 '@RAM@/operation.lock.d/call.update/context'
${before}
${nested ? "/bin/sh -c '/bin/sh @CODE@/verify pre update update-xkeen forced'" : '/bin/sh @CODE@/verify pre update update-xkeen forced'} || exit $?
${between}
${terminal ? `# Synthetic stored completion; actual producer/exec tested separately.
body='v1 999999 1 0123456789abcdef0123456789abcdef'
hash=$(sha256sum '@CODE@/dispatcher'); hash=\${hash%% *}
printf '%s\\n' "$body" > '@RAM@/operation.lock.d/call.update/body'
printf '%s %s\\n' "$body" "$hash" > '@RAM@/operation.lock.d/call.update/staged'
printf '%s updated %s\\n' "$body" "$hash" > '@RAM@/operation.lock.d/call.update/completed'
mkdir -m 700 '@RAM@/operation.lock.d/call.update/exec.used'
printf '/opt/bin/%s\\000/opt/sbin/%s\\000-uk_post_update\\000' sh xkeen > '@RAM@/operation.lock.d/call.update/exec.argv'
chmod 600 '@RAM@/operation.lock.d/call.update/'body '@RAM@/operation.lock.d/call.update/'staged '@RAM@/operation.lock.d/call.update/'completed '@RAM@/operation.lock.d/call.update/'exec.argv` : ''}
/bin/sh '@CODE@/verify' post update update-xkeen forced
`)
  try {
    const result = spawnSync('/bin/sh', [path('wrapper')], { encoding: 'utf8', timeout: 8000 })
    assert.ifError(result.error)
    const read = name => existsSync(path(name)) ? readFileSync(path(name), 'utf8') : ''
    return { ...result, held: existsSync(join(ram, 'operation.lock.d/owner')), baseline: existsSync(join(ram, 'operation.lock.d/call.update/update-preflight')), profiles: read('profile-calls'), hooks: read('hook-calls'), validation: read('validation-calls') }
  } finally { rmSync(code, { recursive: true, force: true }); rmSync(ram, { recursive: true, force: true }) }
}

test('complete verifier preserves running/stopped state independently of autostart', () => {
  for (const running of [true, false]) for (const auto of ['on', 'off']) {
    const r = fixture({ running, auto }); assert.equal(r.status, 0, JSON.stringify(r)); assert.equal(r.baseline, false); assert.equal(r.held, true)
    const expect = running ? 'running' : 'unchanged'
    assert.equal(r.profiles, 'pre\npre\npost\npost\n')
    assert.equal(r.hooks, `pre update update-xkeen forced ${expect}\npost update update-xkeen forced ${expect}\n`)
    assert.equal(r.validation, 'VALIDATE\nVALIDATE\n'); assert.equal(r.stdout, '')
  }
})
test('native timestamp/control size rewrite and running process replacement are permitted', () => {
  const r = fixture({ between: "sed -i 's/1720000000/1720000001/g' '@CODE@/status' '@CODE@/control'; sed -i 's/Installed-Size: 1234/Installed-Size: 5678/' '@CODE@/control'; sed -i 's/ 42 0$/ 43 0/' '@CODE@/proc/123/stat'" })
  assert.equal(r.status, 0, JSON.stringify(r)); assert.equal(r.baseline, false)
})
test('post refuses policy/config/binary/native settings or foreign metadata drift', () => {
  for (const between of [
    "printf 'changed\\n' >> '@CODE@/settings/ip_exclude.lst'",
    "printf '{}\\n' > '@CODE@/configs/02.json'",
    "printf '# drift\\n' >> '@CODE@/core'",
    "printf '# drift\\n' >> '@CODE@/crontabs/root'",
    "sed -i 's/start_auto=\"on\"/start_auto=\"off\"/' '@CODE@/init.d/S05xkeen'",
    "sed -i 's/Version: 1.0/Version: 1.1/' '@CODE@/status'",
    "printf '# drift\\n' >> '@CODE@/profile'",
    "printf '# drift\\n' >> '@CODE@/verify'",
  ]) { const r = fixture({ between }); assert.notEqual(r.status, 0, JSON.stringify(r)); assert.equal(r.baseline && r.held, true) }
})
test('post refuses disappeared or unexpectedly started Xray', () => {
  for (const running of [true, false]) {
    const r = fixture({ running, between: running ? ": > '@CODE@/pids'" : "printf '123\\n' > '@CODE@/pids'" })
    assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.baseline && r.held, true)
  }
})
test('no terminal evidence, replayed query/baseline or non-direct observer cannot settle', () => {
  for (const options of [{ terminal: false }, { nested: true },
    { before: "touch '@RAM@/operation.lock.d/call.update/update-preflight'; chmod 600 '@RAM@/operation.lock.d/call.update/update-preflight'" },
    { between: "mkdir -m 700 '@RAM@/operation.lock.d/call.update/package-identity.post'" },
  ]) { const r = fixture(options); assert.notEqual(r.status, 0, JSON.stringify(r)); assert.equal(r.held, true) }
})
test('pending intent, fenced profile, validator or hook failure refuses and retains admission', () => {
  for (const options of [{ before: "touch '@CODE@/pending'" }, { profile: 'exit 76' }, { validator: 'exit 3' }, { hook: 'exit 3' }]) {
    const r = fixture(options); assert.notEqual(r.status, 0, JSON.stringify(r)); assert.equal(r.held, true); assert.equal(r.baseline, false)
  }
})
test('late source/permission/generation/process drift fails without consuming baseline', () => {
  for (const change of [
    "printf 'changed\\n' >> '@CODE@/settings/ip_exclude.lst'", "chmod 666 '@CODE@/control'",
    "chmod 666 '@CODE@/init.d/S05xkeen'", "printf 'v1 changed\\n' > '@RAM@/operation.lock.d/owner'",
    "sed -i 's/ 42 0$/ 43 0/' '@CODE@/proc/123/stat'",
  ]) {
    const r = fixture({ hook: `[ "$1" != post ] || { ${change}; }` })
    assert.equal(r.status, 77, JSON.stringify(r)); assert.equal(r.baseline && r.held, true)
  }
})
test('owner replacement during exclusive baseline publication is refused, including a valid ancestor', () => {
  for (const publicationChange of [
    "command printf 'v1 changed\\n' >| '@RAM@/operation.lock.d/owner'",
    `IFS= read -r saved < '@RAM@/operation.lock.d/owner'
set -- $saved
boot=$2; owner=$3; token=$5; action=$6
_native_gate_proc "$owner" || return 90
ancestor=$_ng_parent
_native_gate_proc "$ancestor" || return 91
command printf 'v1 %s %s %s %s %s\\n' "$boot" "$ancestor" "$_ng_proc_start" "$token" "$action" >| '@RAM@/operation.lock.d/owner'`,
  ]) {
    const r = fixture({ publicationChange }); assert.equal(r.status, 77, JSON.stringify(r))
    assert.equal(r.baseline && r.held, true); assert.equal(r.profiles, 'pre\npre\n')
  }
})
