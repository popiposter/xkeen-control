import { expect, test } from '@playwright/test'

async function mountNativeCommands(page, { output = 'Native result\r\n', bootstrap } = {}) {
  const model = { starts: [], reads: [], inputs: [], resolves: [], job: null }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const body = request.method() === 'POST' ? request.postDataJSON() : null
    const json = (value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
    if (path === '/api/v1/xkeen/commands') return json([
      { action: 'status', label: 'Check status', interactive: false },
      { action: 'geodata-schedule', label: 'Set geodata schedule', interactive: true },
    ])
    if (path === '/api/v1/xkeen/jobs/start') {
      model.starts.push(body)
      model.job = { id: 'c'.repeat(32), action: body.action, state: body.action === 'status' ? 'completed' : 'running', interactive: body.action !== 'status', output: '', cursor: 0, truncated: false }
      return json(model.job, 202)
    }
    if (path === '/api/v1/xkeen/jobs/read') {
      model.reads.push(body)
      if (!body.id && bootstrap) await bootstrap()
      if (!model.job) return json({}, 409)
      const start = Math.min(body.cursor, output.length)
      const chunk = output.slice(start, start + 32768)
      return json({ ...model.job, output: Buffer.from(chunk).toString('base64'), cursor: start + chunk.length })
    }
    if (path === '/api/v1/xkeen/jobs/input') { model.inputs.push(body); return json({}) }
    if (path === '/api/v1/xkeen/jobs/resize') return json({})
    if (path === '/api/v1/xkeen/jobs/cancel') { model.job.state = 'unknown'; return json({}) }
    if (path === '/api/v1/xkeen/jobs/resolve') { model.resolves.push(body); model.job.state = 'inspected'; return json({ ...model.job, output: '', cursor: 0, truncated: false }) }
    const value = {
      '/api/v1/session': { csrfToken: 'synthetic-native-csrf' },
      '/api/v1/status': { controlPlane: {}, xray: {}, xkeen: {}, balancer: {}, observatory: {}, benchmark: { controlPlane: {} }, selection: {}, lifecycle: { applying: false, maintenance: false }, native: { installation: 'available', version: '2.0.1', channel: 'beta', core: 'xray', panelIntegration: 'available', xrayRunning: true } },
      '/api/v1/nodes': { total: 0, nodes: [], subscriptions: [] },
      '/api/v1/performance': { nodes: [] },
    }[path]
    return json(value || {}, value ? 200 : 404)
  })
  await page.goto('/')
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.getByText('Native XKeen commands', { exact: true })).toBeVisible()
  return model
}

test('configured start waits through preparation without replaying', async ({ page }) => {
  await mountNativeCommands(page)
  await page.route('**/api/v1/xkeen/commands', (route) => route.fulfill({ json: [{ action: 'start', label: 'Start service', interactive: false }] }))
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Start service', exact: true })).toBeEnabled()
  await page.clock.install()
  let pendingRoute
  let starts = 0
  await page.route('**/api/v1/xkeen/jobs/start', (route) => { starts++; pendingRoute = route })
  await page.getByRole('button', { name: 'Start service', exact: true }).click()
  await page.getByRole('button', { name: 'Run native command', exact: true }).click()
  await expect.poll(() => starts).toBe(1)
  await page.clock.fastForward(70_000)
  await expect(page.getByRole('button', { name: 'Run native command', exact: true })).toBeDisabled()
  await pendingRoute.fulfill({ status: 202, json: { id: 'c'.repeat(32), action: 'start', state: 'completed', interactive: false, output: '', cursor: 0, truncated: false } })
  await expect(page.getByText('start: completed', { exact: true })).toBeVisible()
  expect(starts).toBe(1)
})

test('waits for existing-job discovery and never starts a command on navigation', async ({ page }) => {
  let release
  const bootstrap = new Promise((resolve) => { release = resolve })
  const model = await mountNativeCommands(page, { bootstrap: () => bootstrap })
  await expect(page.getByRole('button', { name: 'Check status', exact: true })).toBeDisabled()
  expect(model.starts).toEqual([])
  release()
  await expect(page.getByRole('button', { name: 'Check status', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Check status', exact: true }).focus()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Shift+Tab')
  await expect(page.locator('[data-slot=tooltip-content][data-open]')).toContainText('Inspect the current native service state')
  await page.getByRole('button', { name: 'Check status', exact: true }).click()
  await page.getByRole('button', { name: 'Run native command', exact: true }).click()
  await expect.poll(() => model.starts).toEqual([{ action: 'status' }])
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.getByText('status: completed', { exact: true })).toBeVisible()
  expect(model.starts).toHaveLength(1)
})

test('delayed lazy console preserves every output chunk and is read-only for status', async ({ page }) => {
  let release
  const loaded = new Promise((resolve) => { release = resolve })
  await page.route('**/src/native-console.jsx*', async (route) => { await loaded; await route.continue() })
  const output = 'BEGIN_SENTINEL\r\n' + 'x'.repeat(40000) + '\r\nEND_SENTINEL\r\n'
  const model = await mountNativeCommands(page, { output })
  await page.getByRole('button', { name: 'Check status', exact: true }).click()
  await page.getByRole('button', { name: 'Run native command', exact: true }).click()
  await expect(page.getByText('status: completed', { exact: true })).toBeVisible()
  await expect(page.getByText('Loading console…', { exact: true })).toBeVisible()
  expect(model.reads.filter((read) => read.id && read.cursor < Number.MAX_SAFE_INTEGER)).toEqual([])
  release()
  await expect.poll(() => model.reads.some((read) => read.cursor === 32768)).toBe(true)
  await expect(page.getByText('END_SENTINEL', { exact: true })).toBeVisible()
  expect(model.starts).toHaveLength(1)
  await page.getByLabel('Native XKeen console').click()
  await page.keyboard.type('no input')
  expect(model.inputs).toEqual([])
})

test('a lost Start response locks another attempt instead of replaying', async ({ page }) => {
  const model = await mountNativeCommands(page)
  await page.route('**/api/v1/xkeen/jobs/start', (route) => route.abort())
  await page.getByRole('button', { name: 'Check status', exact: true }).click()
  await page.getByRole('button', { name: 'Run native command', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Check status', exact: true })).toBeDisabled()
  await expect(page.getByRole('status').filter({ hasText: /Failed to fetch/ })).toBeVisible()
  expect(model.starts).toHaveLength(0)
})

test('late console 401 from an old session cannot sign out the new session', async ({ page }) => {
  let release
  const delayed = new Promise((resolve) => { release = resolve })
  const model = await mountNativeCommands(page)
  let seen = false
  let delivered
  const finished = new Promise((resolve) => { delivered = resolve })
  await page.route('**/api/v1/xkeen/jobs/read', async (route) => {
    if (seen) return route.fallback()
    seen = true
    await delayed
    try { await route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"old session"}' }) } catch { /* The old request was aborted by cleanup. */ } finally { delivered() }
  })
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page.getByLabel('Panel password')).toBeVisible()
  await page.route('**/api/v1/session/login', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: '{"csrfToken":"synthetic-new-session"}' }))
  await page.getByLabel('Panel password').fill('synthetic-login-password')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible()
  release()
  await finished
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Check status', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Sign out', exact: true })).toBeVisible()
  expect(model.starts).toEqual([])
})

test('stalled terminal input has a bounded queue and never sends queued answers after leaving', async ({ page }) => {
  const model = await mountNativeCommands(page)
  let release
  const stalled = new Promise((resolve) => { release = resolve })
  let sent = 0
  await page.route('**/api/v1/xkeen/jobs/input', async (route) => { sent += 1; await stalled; try { await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' }) } catch {} })
  await page.getByRole('button', { name: 'Set geodata schedule', exact: true }).click()
  await page.getByRole('button', { name: 'Run native command', exact: true }).click()
  await page.getByLabel('Native XKeen console').locator('textarea').focus()
  await page.keyboard.type('y'.repeat(80))
  await expect(page.getByRole('status').filter({ hasText: 'Input queue is full' })).toBeVisible()
  expect(sent).toBe(1)
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  release()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.getByText('geodata-schedule: running', { exact: true })).toBeVisible()
  expect(sent).toBe(1)
  expect(model.starts).toHaveLength(1)
})


test('inspects an interrupted command without replaying it before enabling new actions', async ({ page }) => {
  const model = await mountNativeCommands(page)
  await page.getByRole('button', { name: 'Set geodata schedule', exact: true }).click()
  await page.getByRole('button', { name: 'Run native command', exact: true }).click()
  await expect.poll(() => model.starts.length).toBe(1)
  await page.getByRole('button', { name: 'Interrupt command', exact: true }).click()
  await expect(page.getByRole('button', { name: 'I inspected XKeen; allow new actions', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Check status', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'I inspected XKeen; allow new actions', exact: true }).click()
  await expect.poll(() => model.resolves).toEqual([{ id: 'c'.repeat(32), inspected: true }])
  await expect(page.getByRole('button', { name: 'Check status', exact: true })).toBeEnabled()
  expect(model.starts).toHaveLength(1)
})
