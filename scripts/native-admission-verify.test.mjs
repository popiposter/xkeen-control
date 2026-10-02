import assert from 'node:assert/strict'
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

const verifier = readFileSync('scripts/native-admission-verify.sh', 'utf8')
const entry = readFileSync('scripts/native-admission-entry.sh', 'utf8')
const gate = readFileSync('scripts/native-operation-gate.sh', 'utf8')

// Real RAM gate/ancestry and protected native code. Core /proc records and
// timeout/Xray/hook readback are synthetic; no upstream router code executes.
function fixture({ action = 'start', mode = 'forced', auto = 'on', setup = '', body = '', config = '{}', hook = 'exit 0', validator = 'exit 0', pending = false, binding = '', event = false, direct = false } = {}) {
  const code = mkdtempSync('/opt/native-verify-fixture-')
  const root = mkdtempSync('/tmp/native-verify-gate-')
  chmodSync(code, 0o700); chmodSync(root, 0o700)
  mkdirSync(join(root, 'native-ready'), { mode: 0o700 })
  if (event && action === 'start') writeFileSync(join(root, 'native-ready/ready'), '', { mode: 0o600 })
  const path = name => join(code, name)
  const put = (name, data, mode = 0o600) => writeFileSync(path(name), data, { mode })
  mkdirSync(path('configs'), { mode: 0o700 }); mkdirSync(path('settings'), { mode: 0o700 })
  mkdirSync(path('proc'), { mode: 0o700 }); mkdirSync(path('proc/123'), { mode: 0o700 })
  put('configs/01.json', config); put('core', '#!/bin/sh\nexit 0\n', 0o700)
  put('proc/123/stat', `123 (xray) S ${['1', ...Array(17).fill('0')].join(' ')} 42 0\n`)
  put('proc/123/cmdline', Buffer.from('xray\0run\0'))
  put('proc/123/environ', Buffer.from(`XRAY_LOCATION_CONFDIR=${path('configs')}\0XRAY_LOCATION_ASSET=${path('assets')}\0`))
  symlinkSync(path('core'), path('proc/123/exe'))
  put('pids', '')
  if (event && action === 'start') put('pids', '123\n')
  put('pidof', `#!/bin/sh\n[ -s '${path('pids')}' ] || exit 1\ncat '${path('pids')}'\n`, 0o700)
  put('hook-verify', `#!/bin/sh\necho "$*" >> '${path('hook-calls')}'\n${hook.replaceAll('@CODE@', code).replaceAll('@ROOT@', root)}\n`, 0o700)
  put('timeout', `#!/bin/sh\n[ "$1 $2" = '-s KILL' ] && [ "$3" -ge 1 ] && [ "$3" -le 15 ] || exit 91\nshift 3\nif [ "$1" = /bin/sh ]; then exec "$@"; fi\n[ "$#" = 5 ] && [ "$2 $3 $4" = 'run -test -confdir' ] || exit 92\n[ -z "\${XKEEN_GATE_TOKEN+x}" ] || exit 93\necho VALIDATE >> '${path('validation-calls')}'\n${validator}\n`, 0o700)
  if (pending) put('pending', 'node-operation-pending\n')
  const replace = text => text.replaceAll('/tmp/.xkeen-admission', root)
    .replaceAll('/tmp/.xkeen', join(root, 'native-ready'))
    .replaceAll('/opt/lib/xkeen/native-operation-gate.sh', path('gate'))
    .replaceAll('/opt/lib/xkeen/native-admission-entry.sh', path('entry'))
    .replaceAll('/opt/lib/xkeen/native-admission-verify.sh', path('verify'))
    .replaceAll('/opt/lib/xkeen/native-admission-hook-verify.sh', path('hook-verify'))
    .replaceAll('/opt/etc/init.d/S05xkeen', path('init'))
    .replaceAll('/opt/etc/ndm/netfilter.d/proxy.sh', path('native-hook'))
    .replaceAll('/opt/lib/xkeen/native-event-reconcile.sh', path('event'))
    .replaceAll('/opt/lib/xkeen/native-event-notification.sh', path('notification'))
    .replaceAll('/opt/lib/xkeen/native-event-convergence.sh', path('convergence'))
    .replaceAll('/opt/etc/xkeen-control/previous/.pending', path('pending'))
    .replaceAll('/opt/etc/xray/configs', path('configs'))
    .replaceAll('/opt/etc/xray/dat', path('assets'))
    .replaceAll('/opt/etc/xkeen', path('settings'))
    .replaceAll('/opt/sbin/xray', path('core'))
    .replaceAll('/opt/libexec/timeout-coreutils', path('timeout'))
    .replaceAll('/opt/bin/pidof', path('pidof'))
    .replaceAll('/opt/bin/sh', '/bin/sh')
  put('gate', gate)
  put('entry', replace(entry))
  put('verify', replace(verifier).replaceAll('/proc/', path('proc/')))
  put('event', replace(readFileSync('scripts/native-event-reconcile.sh', 'utf8')))
  put('notification', replace(readFileSync('scripts/native-event-notification.sh', 'utf8')))
  put('convergence', replace(readFileSync('scripts/native-event-convergence.sh', 'utf8')))
  const defaultBody = action === 'stop' || (action === 'restart' && mode === 'automatic' && auto === 'off')
    ? `: > '${path('pids')}'` : mode === 'automatic' && auto === 'off' ? ':' : `echo 123 > '${path('pids')}'`
  put('native-hook', `#!/bin/sh\n. '${path('entry')}'\nnative_admission_hook_enter || exit $?\n[ "$_na_body" = 1 ] || exit 0\necho HOOKBODY >> '${path('calls')}'\nnative_admission_finish 0\nexit $?\n`)
  put('init', `#!/bin/sh\nname_client="xray"\nstart_auto="${auto}"\n. '${path('entry')}'\nnative_admission_enter init "$1" '${mode}' || exit $?\n[ "$_na_body" = 1 ] || exit 0\necho BODY >> '${path('calls')}'\n${defaultBody}\n${body.replaceAll('@CODE@', code)}\nnative_admission_finish 0\nexit $?\n`)
  try {
    const bind = binding ? `. '${path('gate')}'
native_gate_acquire '${root}' ${binding === 'ordinary' ? 'start' : 'config-change'} || exit $?
owner=$(sha256sum '${root}/operation.lock.d/owner'); owner=\${owner%% *}
meta=$(stat -t '${path('pending')}'); set -- $meta
size=$2; dev=$((0x$7)); ino=$8
digest=$(sha256sum '${path('pending')}'); digest=\${digest%% *}
(umask 077; printf 'v1 %s %s %s %s %s\\n' "$owner" "$dev" "$ino" "$size" "$digest" > '${root}/operation.lock.d/node-intent')
${binding === 'stale' ? `mv '${path('pending')}' '${path('old-pending')}'; printf 'node-operation-pending\\n' > '${path('pending')}'; chmod 600 '${path('pending')}'` : ''}
${binding === 'forged' ? `printf 'forged\\n' > '${root}/operation.lock.d/node-intent'` : ''}
` : ''
    const eventGate = event && !direct ? `. '${path('gate')}'; native_gate_acquire '${root}' reconcile || exit $?;` : ''
    const invoke = direct ? `/bin/sh '${path('native-hook')}'` : event ? `/bin/sh '${path('event')}'; rc=$?; [ "$rc" != 0 ] || native_gate_release; exit "$rc"` : `/bin/sh '${path('init')}' '${action}' ${mode === 'forced' ? 'on' : ''}`
    const r = spawnSync('/bin/sh', ['-c', `${bind}\n${eventGate}\n${setup.replaceAll('@CODE@', code).replaceAll('@ROOT@', root)}\n${invoke}`], { timeout: 4000, encoding: 'utf8' })
    assert.ifError(r.error)
    const read = name => existsSync(path(name)) ? readFileSync(path(name), 'utf8') : ''
    return { ...r, calls: read('calls'), hooks: read('hook-calls'), validation: read('validation-calls'), held: existsSync(join(root, 'operation.lock.d')), ready: existsSync(join(root, 'native-ready/ready')), leader: existsSync(join(root, 'events/leader')), dirty: existsSync(join(root, 'events/dirty')) }
  } finally { rmSync(code, { recursive: true, force: true }); rmSync(root, { recursive: true, force: true }) }
}

test('event worker reads current running state and joins native hook without lifecycle command', () => {
  const r = fixture({ event: true })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.calls, 'HOOKBODY\n')
  assert.equal(r.held, false)
  assert.ok(r.hooks.includes('post event start forced running\n'))
  assert.ok(r.hooks.includes('post hook start forced running\n'))
})
test('direct NDM hook elects caller, verifies current native state and settles one operation', () => {
  for (const action of ['start', 'stop']) {
    const r = fixture({ event: true, direct: true, action, auto: 'off' })
    assert.equal(r.status, 0, r.stderr)
    assert.equal(r.calls, action === 'start' ? 'HOOKBODY\n' : '')
    assert.equal(r.held || r.leader || r.dirty, false)
    assert.ok(r.hooks.includes(`post event ${action} forced ${action === 'start' ? 'running' : 'stopped'}\n`))
  }
})
test('direct NDM failed hook proof preserves event and admission after one hook invocation', () => {
  const r = fixture({ event: true, direct: true, hook: '[ "$1:$2" != post:hook ]' })
  assert.equal(r.status, 77); assert.equal(r.calls, 'HOOKBODY\n')
  assert.equal(r.held && r.leader && r.dirty, true)
})
test('stopped event proves absence without invoking init or hook or requiring valid start config', () => {
  const r = fixture({ event: true, action: 'stop', config: 'invalid', validator: 'exit 1' })
  assert.equal(r.status, 0, r.stderr); assert.equal(r.calls, '')
  assert.equal(r.validation, ''); assert.equal(r.held, false)
  assert.equal(r.hooks, 'pre event stop forced stopped\npost event stop forced stopped\n')
})
test('event refuses unsafe ready state and retained config pending before native effects', () => {
  for (const options of [{ setup: 'chmod 777 @ROOT@/native-ready/ready' }, { pending: true }]) {
    const r = fixture({ event: true, ...options })
    assert.notEqual(r.status, 0); assert.equal(r.calls, ''); assert.equal(r.held, true)
  }
})
test('ready changing during native proof remains unresolved without lifecycle replay', () => {
  const r = fixture({ event: true, hook: '[ "$1:$2" != post:hook ] || rm "@ROOT@/native-ready/ready"' })
  assert.notEqual(r.status, 0); assert.equal(r.held, true); assert.equal(r.ready, false)
})
test('ready appearing during a stopped event proof cannot become a stale Start', () => {
  const r = fixture({ event: true, action: 'stop', hook: '[ "$1:$2" != post:event ] || touch "@ROOT@/native-ready/ready"' })
  assert.notEqual(r.status, 0); assert.equal(r.held, true); assert.equal(r.ready, true); assert.equal(r.calls, '')
})
test('fixed native preflight and process proof settle only after hook proof', () => {
  const r = fixture()
  assert.equal(r.status, 0, JSON.stringify(r))
  assert.equal(r.calls, 'BODY\n')
  assert.equal(r.validation, 'VALIDATE\n')
  assert.equal(r.hooks, 'pre init start forced running\npost init start forced running\n')
  assert.equal(r.held, false)
})

test('real core verifier covers nested foreground hook in forced and automatic lifecycle', () => {
  for (const [action, mode] of [['start', 'forced'], ['restart', 'forced'], ['start', 'automatic'], ['restart', 'automatic']]) {
    const r = fixture({ action, mode, body: '/bin/sh @CODE@/native-hook || exit $?' })
    assert.equal(r.status, 0, JSON.stringify(r))
    assert.equal(r.calls, 'BODY\nHOOKBODY\n')
    assert.equal(r.held, false)
    assert.ok(r.hooks.includes(`pre hook ${action} ${mode} running\n`))
    assert.ok(r.hooks.includes(`post hook ${action} ${mode} running\n`))
  }
})

test('real nested hook postcondition failure cannot settle the init owner', () => {
  const r = fixture({ body: '/bin/sh @CODE@/native-hook || exit $?', hook: '[ "$1:$2" != post:hook ]' })
  assert.notEqual(r.status, 0)
  assert.equal(r.calls, 'BODY\nHOOKBODY\n')
  assert.equal(r.held, true)
})

test('missing hook capability, validation failure and retained config intent precede body', () => {
  for (const options of [{ setup: 'rm @CODE@/hook-verify' }, { setup: 'chmod 777 @CODE@/timeout' }, { validator: 'exit 1' }, { pending: true }]) {
    const r = fixture(options)
    assert.notEqual(r.status, 0)
    assert.equal(r.calls, '')
    assert.equal(r.held, true)
  }
})

test('postcondition rejects dead/wrong core, config drift and native hook failure', () => {
  for (const options of [
    { body: ': > @CODE@/pids' },
    { body: "printf 'core\\000run\\000-confdir\\000/other\\000' > @CODE@/proc/123/cmdline" },
    { body: 'echo changed > @CODE@/configs/01.json' },
    { hook: '[ "$1" != post ]' },
  ]) {
    const r = fixture(options)
    assert.notEqual(r.status, 0)
    assert.equal(r.calls, 'BODY\n')
    assert.equal(r.held, true)
  }
})

test('automatic disabled start preserves existing runtime and restart proves stopped', () => {
  const noop = fixture({ mode: 'automatic', auto: 'off', setup: 'echo 123 > @CODE@/pids' })
  assert.equal(noop.status, 0, noop.stderr)
  assert.equal(noop.hooks, 'pre init start automatic unchanged\npost init start automatic unchanged\n')
  const restart = fixture({ action: 'restart', mode: 'automatic', auto: 'off', setup: 'echo 123 > @CODE@/pids' })
  assert.equal(restart.status, 0, restart.stderr)
  assert.equal(restart.hooks, 'pre init restart automatic stopped\npost init restart automatic stopped\n')
})

test('stop proves core absent without requiring valid start configuration', () => {
  const r = fixture({ action: 'stop', validator: 'exit 1', setup: 'echo 123 > @CODE@/pids' })
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.validation, '')
})

test('only current config-change owner can borrow its exact pending identity', () => {
  const valid = fixture({ pending: true, binding: 'valid' })
  assert.equal(valid.status, 0, JSON.stringify(valid))
  assert.equal(valid.calls, 'BODY\n')
  assert.equal(valid.held, true, 'borrower cannot clear the configuration owner')
  for (const binding of ['ordinary', 'stale', 'forged']) {
    const r = fixture({ pending: true, binding })
    assert.notEqual(r.status, 0, binding)
    assert.equal(r.calls, '', binding)
    assert.equal(r.held, true, binding)
  }
})

test('proof rejects input or runtime change during hook readback', () => {
  for (const options of [
    { hook: '[ "$1" != pre ] || echo changed > @CODE@/configs/01.json', beforeBody: true },
    { hook: '[ "$1" != post ] || : > @CODE@/pids' },
    { hook: '[ "$1" != post ] || echo changed > @CODE@/configs/01.json' },
    { setup: "printf 'XRAY_LOCATION_CONFDIR=/wrong\\000' > @CODE@/proc/123/environ", body: ':' },
  ]) {
    const r = fixture(options)
    assert.notEqual(r.status, 0)
    assert.equal(r.calls, options.beforeBody ? '' : 'BODY\n')
    assert.equal(r.held, true)
  }
})
