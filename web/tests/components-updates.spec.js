import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-csrf-token'
const browserIssues = new WeakMap()

test.beforeEach(async ({ page }) => {
  const issues = []
  browserIssues.set(page, issues)
  page.on('console', (message) => {
    // Chromium reports the deliberately intercepted non-2xx/network-loss
    // fixtures as generic resource errors. Those outcomes are asserted by the
    // individual tests; retain every other console warning/error as a failure.
    if ((message.type() === 'error' || message.type() === 'warning') && !message.text().startsWith('Failed to load resource:')) {
      issues.push(`${message.type()}: ${message.text()}`)
    }
  })
  page.on('pageerror', (error) => issues.push(`pageerror: ${error.message}`))
})

test.afterEach(async ({ page }) => {
  expect(browserIssues.get(page) || []).toEqual([])
})

const component = (kind, overrides = {}) => ({
  kind,
  state: 'present',
  present: true,
  version: `${kind}-1.0.0`,
  versionUnknown: false,
  capability: ['xray', 'geodata', 'xkeen'].includes(kind) ? 'supported' : 'informational',
  ...overrides,
})

const inventoryFixture = () => ({
  schemaVersion: 1,
  panel: component('panel', { version: '0.2.0', channel: 'stable' }),
  xkeen: component('xkeen', { version: '', versionUnknown: true }),
  xray: component('xray', { version: '25.9.1', architecture: 'arm64' }),
  geodata: {
    ...component('geodata', { version: '', versionUnknown: true }),
    items: [
      ['geoip', 'geoip.dat'], ['geosite', 'geosite.dat'], ['geoip-ir', 'geoip_IR.dat'],
      ['geosite-ir', 'geosite_IR.dat'], ['geoip-ru', 'geoip_RU.dat'], ['geosite-ru', 'geosite_RU.dat'],
    ].map(([id, name], index) => ({ id, name, source: 'fixed', state: index === 5 ? 'unknown' : 'present', present: index !== 5, sizeBytes: 1_048_576, sha256: `${index}`.repeat(64) })),
  },
  keeneticos: component('keeneticos', { state: 'unknown', present: false, version: '', reasonCode: 'signal-unavailable' }),
  entware: component('entware', { state: 'missing', present: false, version: '', reasonCode: 'binary-missing', capability: 'unsupported' }),
})

const statusFixture = () => ({
  controlPlane: { version: 'dev', uptimeSeconds: 120 },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  balancer: { effective: '' },
  observatory: { healthy: 0, total: 0, apiReachable: true },
  benchmark: { controlPlane: { running: false, state: 'idle' } },
  selection: { state: 'stable' },
  setup: { runtime: 'running', credential: 'ready', xkeen: 'ready', xray: 'ready', configuration: 'ready' },
  lifecycle: { maintenance: false, applying: false },
})

const jsonResponse = (route, body, status = 200) => route.fulfill({
  status,
  contentType: 'application/json',
  body: JSON.stringify(body),
})

async function mockApplication(page, options = {}) {
  const scenario = {
    status: statusFixture(),
    inventory: inventoryFixture(),
    requests: [],
    counts: {},
    previewDelay: 0,
    applyDelay: 0,
    inventoryRefreshFailure: false,
    ...options,
  }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let requestBody = null
    if (request.postData()) {
      try { requestBody = request.postDataJSON() } catch { requestBody = request.postData() }
    }
    scenario.requests.push({ path, method: request.method(), headers: request.headers(), body: requestBody })
    scenario.counts[path] = (scenario.counts[path] || 0) + 1

    if (typeof scenario.handle === 'function') {
      const handled = await scenario.handle({ route, request, path, scenario })
      if (handled) return
    }
    switch (path) {
      case '/api/v1/session': return jsonResponse(route, { csrfToken })
      case '/api/v1/session/logout': return jsonResponse(route, {})
      case '/api/v1/status': return jsonResponse(route, scenario.status)
      case '/api/v1/nodes': return jsonResponse(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return jsonResponse(route, { nodes: [] })
      case '/api/v1/config-summary': return jsonResponse(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return jsonResponse(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/components':
        if (scenario.inventoryRefreshFailure && scenario.counts[path] > 1) return jsonResponse(route, { error: 'component inventory unavailable' }, 503)
        return jsonResponse(route, scenario.inventory)
      case '/api/v1/components/check': {
        const body = request.postDataJSON()
        return jsonResponse(route, {
          schemaVersion: 1, component: body.component, channel: body.channel, checkedAt: new Date().toISOString(),
          candidate: { version: '1.0.0', generation: 'checked-generation' }, installedState: 'present', eligible: true, mutationAvailable: true,
        })
      }
      case '/api/v1/components/preview': {
        if (scenario.previewDelay) await new Promise((resolve) => setTimeout(resolve, scenario.previewDelay))
        const body = request.postDataJSON()
        const expiresAt = scenario.previewExpiresAt || new Date(Date.now() + 5 * 60_000).toISOString()
        const base = { schemaVersion: 1, previewToken: `token-${scenario.counts[path]}`, component: body.component, operation: body.operation, expiresAt }
        if (body.operation === 'rollback') return jsonResponse(route, { ...base, previous: { generation: 'retained-generation', sizeBytes: 2_097_152, sha256: 'a'.repeat(64) } })
        return jsonResponse(route, { ...base, channel: body.channel, candidate: { version: '2.0.0', generation: 'preview-generation', sizeBytes: 3_145_728, sha256: 'b'.repeat(64), items: body.component === 'geodata' ? [{ id: 'geoip', name: 'geoip.dat', sizeBytes: 1024, sha256: 'c'.repeat(64) }] : [] } })
      }
      case '/api/v1/components/apply':
      case '/api/v1/components/rollback': {
        if (scenario.applyDelay) await new Promise((resolve) => setTimeout(resolve, scenario.applyDelay))
        const operation = path.endsWith('/rollback') ? 'rollback' : 'update'
        return jsonResponse(route, { schemaVersion: 1, component: scenario.operationComponent || 'xray', operation, state: operation === 'rollback' ? 'rolled-back' : 'applied', version: '2.0.0', generation: 'result-generation' })
      }
      case '/api/v1/components/cancel': return jsonResponse(route, {})
      default: return jsonResponse(route, { error: 'not found' }, 404)
    }
  })
  return scenario
}

const openComponents = async (page) => {
  await page.goto('/')
  await expect(page).toHaveURL('http://127.0.0.1:4173/')
  await expect(page).toHaveTitle('XKeen Control')
  await expect(page.locator('#root')).toContainText('Overview')
  await page.getByRole('button', { name: 'Components / Updates' }).click()
  await expect(page.getByRole('heading', { name: 'Manual, one component at a time' })).toBeVisible()
}

test('loads all six classes lazily and never adds inventory to dashboard polling', async ({ page }, testInfo) => {
  const scenario = await mockApplication(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Manual, one component at a time' })).toHaveCount(0)
  expect(scenario.counts['/api/v1/components'] || 0).toBe(0)

  await page.getByRole('button', { name: 'Components / Updates' }).click()
  await expect(page.locator('[data-component]')).toHaveCount(6)
  await expect(page.locator('[data-component="xray"]')).toContainText('25.9.1')
  await expect(page.locator('[data-component="xkeen"]')).toContainText('Present · version unknown')
  await expect(page.locator('[data-component="keeneticos"]')).toContainText('Unknown')
  await expect(page.locator('[data-component="entware"]')).toContainText('Missing')
  await expect(page.locator('[data-component="xkeen"]')).toContainText('Check dev')
  await expect(page.locator('[data-component="xray"]')).toContainText('Check stable')
  await expect(page.locator('[data-component="panel"]')).not.toContainText('Preview update')
  await expect(page.locator('[data-component="entware"]')).not.toContainText(/Install|Repair/)
  await page.locator('[data-component="geodata"] summary').click()
  await expect(page.locator('[data-component="geodata"] li')).toHaveCount(6)

  await page.waitForTimeout(5_200)
  expect(scenario.counts['/api/v1/components']).toBe(1)
  expect(scenario.counts['/api/v1/status']).toBeGreaterThan(1)
  for (const path of ['/api/v1/components/check', '/api/v1/components/preview', '/api/v1/components/apply', '/api/v1/components/rollback', '/api/v1/components/cancel']) {
    expect(scenario.counts[path] || 0).toBe(0)
  }
  await page.screenshot({ path: testInfo.outputPath('components-desktop.png'), fullPage: true })
})

test('uses exact CSRF-bound bodies, shows fresh Preview, and rolls back without Check', async ({ page }) => {
  const scenario = await mockApplication(page, { operationComponent: 'geodata' })
  await openComponents(page)

  const xray = page.locator('[data-component="xray"]')
  await xray.getByRole('button', { name: 'Check stable' }).click()
  await expect(xray).toContainText('1.0.0')
  await xray.getByRole('button', { name: 'Preview update' }).click()
  await expect(page.getByLabel('Component operation confirmation')).toContainText('2.0.0')
  await page.getByLabel('Component operation confirmation').getByRole('button', { name: 'Cancel' }).click()

  const geodata = page.locator('[data-component="geodata"]')
  await geodata.getByRole('button', { name: 'Preview rollback' }).click()
  await expect(page.getByLabel('Component operation confirmation')).toContainText('retained-generation')
  await page.getByRole('button', { name: 'Confirm rollback' }).click()
  await expect(page.getByText('Geodata rollback completed')).toBeVisible()

  const check = scenario.requests.find((request) => request.path === '/api/v1/components/check')
  expect(check.body).toEqual({ component: 'xray', channel: 'stable' })
  expect(check.headers['x-csrf-token']).toBe(csrfToken)
  const previews = scenario.requests.filter((request) => request.path === '/api/v1/components/preview')
  expect(previews[0].body).toEqual({ component: 'xray', operation: 'update', channel: 'stable' })
  expect(previews[1].body).toEqual({ component: 'geodata', operation: 'rollback' })
  const rollback = scenario.requests.find((request) => request.path === '/api/v1/components/rollback')
  expect(rollback.body).toEqual({ previewToken: 'token-2' })
  expect(Object.keys(rollback.body)).toEqual(['previewToken'])
  expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
})

test('preserves one delayed mutation across navigation and dashboard refresh', async ({ page }) => {
  const scenario = await mockApplication(page, { applyDelay: 5_300 })
  await openComponents(page)
  await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
  const confirm = page.getByRole('button', { name: 'Confirm update' })
  await confirm.evaluate((button) => { button.click(); button.click() })
  await page.getByRole('button', { name: 'Overview' }).click()
  await expect(page.getByText('Xray update is running')).toBeVisible()
  await page.getByRole('button', { name: 'View operation' }).click()
  await expect(page.getByText('No progress percentage is available.')).toBeVisible()
  await expect(page.getByText('Xray update completed')).toBeVisible({ timeout: 8_000 })
  expect(scenario.counts['/api/v1/components/apply']).toBe(1)
  expect(scenario.counts['/api/v1/status']).toBeGreaterThan(1)
  const apply = scenario.requests.find((request) => request.path === '/api/v1/components/apply')
  expect(apply.body).toEqual({ previewToken: 'token-1' })
  expect(Object.keys(apply.body)).toEqual(['previewToken'])
  expect(apply.headers['x-csrf-token']).toBe(csrfToken)
})

test('discards canceled and expired Preview tokens including late responses', async ({ page }) => {
  const scenario = await mockApplication(page, { previewDelay: 350 })
  await openComponents(page)
  await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
  await page.getByRole('button', { name: 'Cancel Preview' }).click()
  await page.waitForTimeout(450)
  await expect(page.getByLabel('Component operation confirmation')).toHaveCount(0)
  expect(scenario.counts['/api/v1/components/cancel']).toBe(1)

  scenario.previewDelay = 0
  scenario.previewExpiresAt = new Date(Date.now() + 120).toISOString()
  await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
  await expect(page.getByText('The unsent token was discarded.')).toBeVisible({ timeout: 2_000 })
  expect(scenario.counts['/api/v1/components/cancel']).toBe(2)
})

test('clears private Preview state on logout and ignores a late response', async ({ page }) => {
  const scenario = await mockApplication(page, { previewDelay: 300 })
  await openComponents(page)
  await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page.getByRole('heading', { name: 'XKeen Control' })).toBeVisible()
  await page.waitForTimeout(400)
  expect(scenario.counts['/api/v1/components/apply'] || 0).toBe(0)
  expect(scenario.counts['/api/v1/components/cancel']).toBe(1)
})

const codedCases = [
  ['busy', 409, 'Lifecycle is busy'],
  ['preview-stale', 409, 'Preview is stale'],
  ['no-previous', 409, 'No retained generation'],
  ['transaction-restored', 500, 'Xray update failed'],
  ['transaction-unproven', 500, 'Xray outcome is not proven'],
  ['rollback-unproven', 503, 'Xray outcome is not proven'],
  ['maintenance', 503, 'Lifecycle maintenance is active'],
]

for (const [code, status, expected] of codedCases) {
  test(`renders the stable ${code} outcome without replay`, async ({ page }) => {
    const scenario = await mockApplication(page, {
      handle: async ({ route, path }) => {
        if (path !== '/api/v1/components/apply') return false
        await jsonResponse(route, { code, error: 'safe component failure' }, status)
        return true
      },
    })
    await openComponents(page)
    await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
    await page.getByRole('button', { name: 'Confirm update' }).click()
    await expect(page.getByText(expected, { exact: true }).first()).toBeVisible()
    expect(scenario.counts['/api/v1/components/apply']).toBe(1)
  })
}

test('treats network loss and malformed 2xx as unknown rather than success', async ({ page }) => {
  let mode = 'network'
  const scenario = await mockApplication(page, {
    handle: async ({ route, path }) => {
      if (path !== '/api/v1/components/apply') return false
      if (mode === 'network') await route.abort('connectionrefused')
      else await route.fulfill({ status: 200, contentType: 'application/json', body: '' })
      return true
    },
  })
  await openComponents(page)
  const preview = page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })
  await preview.click()
  await page.getByRole('button', { name: 'Confirm update' }).click()
  await expect(page.getByText('Xray outcome is unknown', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Dismiss' }).click()
  mode = 'malformed'
  await preview.click()
  await page.getByRole('button', { name: 'Confirm update' }).click()
  await expect(page.getByText('Xray outcome is unknown', { exact: true }).first()).toBeVisible()
  expect(scenario.counts['/api/v1/components/apply']).toBe(2)
})

for (const status of [401, 403]) {
  test(`does not replay an HTTP ${status} mutation response`, async ({ page }) => {
    const scenario = await mockApplication(page, {
      handle: async ({ route, path }) => {
        if (path !== '/api/v1/components/apply') return false
        await jsonResponse(route, { error: status === 401 ? 'unauthorized' : 'forbidden' }, status)
        return true
      },
    })
    await openComponents(page)
    await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
    await page.getByRole('button', { name: 'Confirm update' }).click()
    if (status === 401) await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
    else await expect(page.getByText('Authorization was not accepted')).toBeVisible()
    expect(scenario.counts['/api/v1/components/apply']).toBe(1)
  })
}

test('preserves a confirmed result when post-success inventory refresh fails', async ({ page }) => {
  await mockApplication(page, { inventoryRefreshFailure: true })
  await openComponents(page)
  await page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' }).click()
  await page.getByRole('button', { name: 'Confirm update' }).click()
  await expect(page.getByText('Xray update completed')).toBeVisible()
  await expect(page.getByText('operation result is preserved')).toBeVisible()
})

test('shows maintenance across sections and never confuses a benchmark with lifecycle Apply', async ({ page }) => {
  const status = statusFixture()
  status.lifecycle.maintenance = true
  status.benchmark.controlPlane.running = true
  const scenario = await mockApplication(page, { status })
  await page.goto('/')
  await expect(page.getByText('Lifecycle maintenance', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'System' }).click()
  await expect(page.getByText('Lifecycle maintenance', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Components / Updates' }).click()
  await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })).toBeDisabled()
  await expect(page.locator('[data-component="xray"]')).toContainText('25.9.1')

  scenario.status.lifecycle.maintenance = false
  await page.reload()
  await page.getByRole('button', { name: 'Components / Updates' }).click()
  await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })).toBeEnabled()
})

test('fails closed when the lifecycle projection is unavailable', async ({ page }) => {
  const status = statusFixture()
  delete status.lifecycle
  await mockApplication(page, { status })
  await openComponents(page)
  await expect(page.getByText('Lifecycle readiness unavailable', { exact: true })).toBeVisible()
  await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })).toBeDisabled()
  await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Check stable' })).toBeEnabled()
})

test('supports keyboard confirmation focus restoration and a readable mobile layout', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockApplication(page)
  await openComponents(page)
  const button = page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })
  await button.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByLabel('Component operation confirmation')).toBeVisible()
  await page.getByLabel('Component operation confirmation').getByRole('button', { name: 'Cancel' }).click()
  await expect(button).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('components-mobile.png'), fullPage: true })
})
