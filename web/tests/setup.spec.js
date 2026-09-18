import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-setup-csrf'

const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })

const baseStatus = (setup) => ({
  controlPlane: { version: 'test', uptimeSeconds: 12 },
  xray: {},
  xkeen: {},
  balancer: {},
  observatory: {},
  benchmark: { controlPlane: {} },
  selection: {},
  setup,
  lifecycle: { maintenance: false, applying: false },
})

async function prepare(page, setup) {
  const state = { setup, requests: [] }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = null
    if (request.postData()) {
      try { body = request.postDataJSON() } catch { body = request.postData() }
    }
    state.requests.push({ path, method: request.method(), body, csrf: request.headers()['x-csrf-token'] })
    switch (path) {
      case '/api/v1/session': return json(route, { csrfToken })
      case '/api/v1/status': return json(route, baseStatus(state.setup))
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: 'test' } })
      case '/api/v1/setup/preview': return json(route, {
        schemaVersion: 1,
        operation: 'setup',
        previewToken: 'synthetic-setup-token',
        expiresAt: new Date(Date.now() + 300_000).toISOString(),
        plan: {
          schemaVersion: 1,
          productDefault: true,
          emptyRegistry: true,
          xray: { version: '25.9.1', sha256: 'a'.repeat(64) },
          geodata: { generation: 'geo-generation', items: [{ id: 'geoip' }, { id: 'geosite' }, { id: 'geoip-ir' }, { id: 'geosite-ir' }, { id: 'geoip-ru' }, { id: 'geosite-ru' }] },
          xkeen: { version: 'dev-test', generationSha256: 'b'.repeat(64) },
          lifecycle: { name: 'S05xkeen', sha256: 'c'.repeat(64) },
        },
      })
      case '/api/v1/setup/cancel': return json(route, { canceled: true })
      case '/api/v1/setup/apply':
        state.setup = { ...state.setup, state: 'ready', eligible: false, reasonCode: 'already-configured' }
        return json(route, { schemaVersion: 1, operation: 'setup', state: 'ready', completedAt: new Date().toISOString() })
      default: return json(route, { error: 'unexpected synthetic route' }, 404)
    }
  })
  await page.goto('/')
  return state
}

test('exposes one fixed in-memory setup flow and applies only its session token', async ({ page }) => {
  const state = await prepare(page, { state: 'fresh', eligible: true, reasonCode: 'fresh', runtime: 'setup' })
  await expect(page.getByRole('button', { name: 'Prepare setup' })).toBeVisible()
  await page.getByRole('button', { name: 'Prepare setup' }).click()
  await expect(page.getByText('Fixed setup plan')).toBeVisible()
  await expect(page.getByText('Product default', { exact: true })).toBeVisible()
  await expect(page.getByText('Empty canonical registry', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Apply setup' }).click()
  await expect(page.getByRole('button', { name: 'Confirm setup' })).toBeVisible()
  await page.getByRole('button', { name: 'Confirm setup' }).click()
  await expect(page.getByRole('button', { name: 'Prepare setup' })).toHaveCount(0)

  const preview = state.requests.find((request) => request.path === '/api/v1/setup/preview')
  const apply = state.requests.find((request) => request.path === '/api/v1/setup/apply')
  expect(preview).toEqual(expect.objectContaining({ method: 'POST', body: {}, csrf: csrfToken }))
  expect(apply).toEqual(expect.objectContaining({ method: 'POST', body: { previewToken: 'synthetic-setup-token' }, csrf: csrfToken }))
  expect(state.requests.some((request) => request.path.startsWith('/api/v1/components/'))).toBe(false)
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
})

test('shows a closed reason and no Apply action for a blocked layout', async ({ page }) => {
  const state = await prepare(page, { state: 'blocked', eligible: false, reasonCode: 'layout-partial', runtime: 'setup' })
  await expect(page.getByText('A partial managed layout was found')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Prepare setup' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Apply setup' })).toHaveCount(0)
  expect(state.requests.filter((request) => request.path.startsWith('/api/v1/setup/'))).toHaveLength(0)
})
