import test from 'node:test'
import assert from 'node:assert/strict'
import { lifecycleBlocked } from '../src/lifecycle.js'

test('only an explicit idle lifecycle admits mutations', () => {
  assert.equal(lifecycleBlocked({ maintenance: false, applying: false }), false)
  for (const lifecycle of [undefined, null, {}, { maintenance: false }, { applying: false }, { maintenance: 'false', applying: false }, { maintenance: true, applying: false }, { maintenance: false, applying: true }]) {
    assert.equal(lifecycleBlocked(lifecycle), true, JSON.stringify(lifecycle))
  }
})
