import { test } from 'node:test'
import assert from 'node:assert/strict'
import { configRequestTimeout, commandRequestTimeout } from '../src/native-request-budget.js'

test('only synchronous validation writes and configured lifecycle starts wait 150s', () => {
  for (const path of ['text', 'save-set', 'restore-saved', 'restore-previous', 'apply']) {
    assert.equal(configRequestTimeout(path, {}), 150_000)
    assert.equal(configRequestTimeout(path), 10_000)
  }
  for (const path of ['document', 'workspace', 'draft', 'geodata/query', 'jobs/read']) assert.equal(configRequestTimeout(path, {}), 10_000)
  for (const action of ['start', 'restart']) assert.equal(commandRequestTimeout('jobs/start', { action }), 150_000)
  for (const action of ['status', 'stop', 'update-xkeen', 'update-xray']) assert.equal(commandRequestTimeout('jobs/start', { action }), 10_000)
  for (const path of ['commands', 'jobs/read', 'jobs/input', 'jobs/cancel', 'jobs/resize']) assert.equal(commandRequestTimeout(path, { action: 'start' }), 10_000)
})
