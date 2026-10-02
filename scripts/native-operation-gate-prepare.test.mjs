import assert from 'node:assert/strict'
import { chmodSync, existsSync, lstatSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { test } from 'node:test'

function fixture(body) {
  const parent = mkdtempSync('/tmp/native-prepare-')
  chmodSync(parent, 0o700)
  const root = `${parent}/admission`, gate = `${parent}/library`
  writeFileSync(gate, readFileSync('scripts/native-operation-gate.sh', 'utf8').replaceAll('/tmp/.xkeen-admission', root), { mode: 0o600 })
  try {
    const r = spawnSync('/bin/sh', ['-c', `. '${gate}'\n${body.replaceAll('@ROOT@', root).replaceAll('@GATE@', gate)}`], { encoding: 'utf8', timeout: 3000 })
    assert.ifError(r.error)
    return { ...r, root: existsSync(root), mode: existsSync(root) ? lstatSync(root).mode & 0o777 : null, owner: existsSync(`${root}/operation.lock.d/owner`), unknown: existsSync(`${root}/operation.lock.d/unknown`) }
  } finally { rmSync(parent, { recursive: true, force: true }) }
}
test('new boot creates only protected RAM root and permits a new native operation', () => {
  const r = fixture('native_gate_prepare && native_gate_acquire "@ROOT@" start && native_gate_release')
  assert.equal(r.status, 0, r.stderr); assert.equal(r.mode, 0o700); assert.equal(r.owner, false)
})
test('concurrent boot and NDM prepare tolerate one protected mkdir winner', () => {
  const r = fixture(`/bin/sh -c '. "@GATE@"; native_gate_prepare' & child=$!
native_gate_prepare || exit $?
wait "$child"`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.mode, 0o700)
})
test('existing unknown operation survives preparation without recovery or replay', () => {
  const r = fixture(`native_gate_prepare && native_gate_acquire '@ROOT@' restart || exit $?
touch '@ROOT@/operation.lock.d/unknown'
native_gate_prepare || exit $?
native_gate_acquire '@ROOT@' start; [ "$?" = 75 ]`)
  assert.equal(r.status, 0, r.stderr); assert.equal(r.owner && r.unknown, true)
})
test('unsafe existing root is refused without chmod, deletion or repair', () => {
  for (const setup of ['mkdir -m 777 "@ROOT@"', 'touch "@ROOT@"', 'ln -s /nonexistent "@ROOT@"']) {
    const r = fixture(`${setup}\nnative_gate_prepare; [ "$?" = 76 ]`)
    assert.equal(r.status, 0, r.stderr)
    if (setup.startsWith('mkdir')) assert.equal(r.mode, 0o777)
  }
})
