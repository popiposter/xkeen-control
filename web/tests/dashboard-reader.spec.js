import { test, expect } from '@playwright/test'
import { createDashboardReader } from '../src/dashboard-reader.js'

const deferred = () => {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const data = (path, version) => path.endsWith('/status') ? { controlPlane: { version } } : { nodes: [] }

test('post-mutation refresh runs after outstanding read; performance has one owner', async () => {
  const pending = deferred()
  const calls = [], values = []
  const reader = createDashboardReader({
    api: async (path) => { calls.push(path); await pending.promise; return data(path, calls.length) },
    onData: (value) => values.push(value), onError: (error) => { throw error }, onDone: () => {},
  })
  const first = reader.refresh()
  const second = reader.refresh()
  const diagnostic = reader.refresh(false)
  expect(calls).toHaveLength(3)
  pending.resolve()
  await Promise.all([first, second, diagnostic])
  expect(calls).toHaveLength(6)
  expect(values).toHaveLength(2)
  expect(calls.filter((path) => path.endsWith('/performance'))).toHaveLength(2)
})

test('old session success and late 401 cannot replace the new session', async () => {
  for (const failure of [false, true]) {
    const pending = deferred()
    let old = true
    const values = [], errors = []
    const reader = createDashboardReader({
      api: async (path) => { if (old) await pending.promise; return data(path, 'new') },
      onData: (value) => values.push(value), onError: (error) => errors.push(error), onDone: () => {},
    })
    const first = reader.refresh()
    reader.invalidate()
    old = false
    await reader.refresh()
    if (failure) pending.reject(Object.assign(new Error('old unauthorized'), { status: 401 }))
    else pending.resolve()
    await first
    expect(values).toHaveLength(1)
    expect(errors).toEqual([])
  }
})

test('malformed successful projections never reach rendering', async () => {
  const errors = [], values = []
  const reader = createDashboardReader({ api: async () => ({}), onData: (value) => values.push(value), onError: (error) => errors.push(error.message), onDone: () => {} })
  await reader.refresh()
  await reader.refresh(false)
  expect(values).toEqual([])
  expect(errors).toHaveLength(2)
})
