import test from 'node:test'
import assert from 'node:assert/strict'
import { nativeXkeenState } from '../src/native-xkeen-state.js'

const unsure = 'Check XKeen status'

test('missing or unknown native facts never read as a usable installation', () => {
  for (const facts of [undefined, null, {}, { installation: 'unknown', panelIntegration: 'missing' }, { installation: 'unsupported', panelIntegration: 'missing' }, { installation: 'available', panelIntegration: 'unknown' }, { installation: 'available', panelIntegration: 'unsupported' }]) {
    const state = nativeXkeenState(facts)
    assert.equal(state.title, unsure, JSON.stringify(facts))
    assert.equal(state.canManageProfiles, false)
    assert.notEqual(state.description, 'Use the official XKeen installer, then refresh this page.')
  }
  assert.equal(nativeXkeenState({}).description, 'The native installation state is unavailable. Inspect XKeen and refresh status.')
  assert.equal(nativeXkeenState({ installation: 'available', panelIntegration: 'unknown' }).description, 'XKeen is installed. Its configuration access needs inspection; refresh status.')
})

test('running is reported only for an available installation with xrayRunning true', () => {
  assert.equal(nativeXkeenState({ installation: 'available', panelIntegration: 'unknown', xrayRunning: true }).title, 'XKeen is running')
  assert.equal(nativeXkeenState({ installation: 'unknown', xrayRunning: true }).title, unsure)
  assert.equal(nativeXkeenState({ installation: 'available', panelIntegration: 'unknown', xrayRunning: 'true' }).title, unsure)
})

test('explicit installation states choose their own guidance', () => {
  assert.equal(nativeXkeenState({ installation: 'missing' }).title, 'Install XKeen first')
  assert.equal(nativeXkeenState({ installation: 'missing' }).description, 'Use the official XKeen installer, then refresh this page.')
  assert.equal(nativeXkeenState({ installation: 'available', panelIntegration: 'missing' }).title, 'Connect the panel to XKeen')
  const attached = nativeXkeenState({ installation: 'available', panelIntegration: 'available' })
  assert.equal(attached.title, 'Add your VPN profiles')
  assert.equal(attached.canManageProfiles, true)
})
