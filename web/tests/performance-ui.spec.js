import { revealDetails, revealSystemSettings, revealNavigation } from './fixtures/disclosures.js'
import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-csrf-token'

const jsonResponse = (route, body, status = 200) => route.fulfill({
  status,
  contentType: 'application/json',
  body: JSON.stringify(body),
})

const node = (id, tag, name, overrides = {}) => ({
  id,
  tag,
  outboundTag: tag,
  name,
  displayName: name,
  address: `${tag}.test:443`,
  countryCode: 'DE',
  enabled: true,
  alive: true,
  latencyMs: 42,
  sourceType: 'manual',
  isEffective: tag === 'proxy-alpha',
  ...overrides,
})

const statusFixture = (overrides = {}) => ({
  controlPlane: { version: 'dev', uptimeSeconds: 120 },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  balancer: { nativeSelected: 'proxy-alpha', effective: 'proxy-alpha', override: '' },
  observatory: { healthy: 2, total: 2, apiReachable: true },
  benchmark: { controlPlane: { running: false, state: 'idle', schedule: 'explicit-only' } },
  selection: {
    state: 'stable',
    effectiveTarget: 'proxy-alpha',
    manualOverride: '',
    lastSwitchReason: 'startup',
    latencyEvidence: 3,
  },
  native: { installation: 'available', panelIntegration: 'available', version: '2.0.1', channel: 'beta', core: 'xray', xrayRunning: true },
  lifecycle: { maintenance: false, applying: false },
  ...overrides,
})

const waitingAdaptive = (overrides = {}) => ({
  state: 'waiting',
  nextRunAt: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
  candidates: [],
  ...overrides,
})

const performanceFixture = (adaptive = waitingAdaptive(), overrides = {}) => ({
  nodes: [],
  manual: { state: 'idle', phase: 'done' },
  adaptive,
  ...overrides,
})

async function mockApplication(page, options = {}) {
  const scenario = {
    status: statusFixture(),
    nodes: [node('node-alpha', 'proxy-alpha', 'Alpha'), node('node-beta', 'proxy-beta', 'Beta', { isEffective: false })],
    performance: performanceFixture(),
    performanceSequence: null,
    requests: [],
    counts: {},
    ...options,
  }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = null
    if (request.postData()) {
      try { body = request.postDataJSON() } catch { body = request.postData() }
    }
    scenario.requests.push({ path, method: request.method(), body })
    scenario.counts[path] = (scenario.counts[path] || 0) + 1
    switch (path) {
      case '/api/v1/session': return jsonResponse(route, { csrfToken })
      case '/api/v1/session/logout': return jsonResponse(route, {})
      case '/api/v1/status': return jsonResponse(route, scenario.status)
      case '/api/v1/nodes': return jsonResponse(route, { total: scenario.nodes.length, nodes: scenario.nodes, subscriptions: [] })
      case '/api/v1/performance': {
        const sequence = scenario.performanceSequence
        const value = Array.isArray(sequence) && sequence.length
          ? sequence[Math.min(scenario.counts[path] - 1, sequence.length - 1)]
          : scenario.performance
        return jsonResponse(route, value)
      }
      case '/api/v1/config-summary': return jsonResponse(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return jsonResponse(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/performance/manual-node': return jsonResponse(route, { accepted: true, state: 'accepted' }, 202)
      case '/api/v1/benchmark/run': return jsonResponse(route, { accepted: true, state: 'accepted' }, 202)
      default: return jsonResponse(route, { error: 'not found' }, 404)
    }
  })
  return scenario
}

const openApplication = async (page, options = {}) => {
  const scenario = await mockApplication(page, options)
  await page.goto('/')
  await page.locator('.workspace').waitFor({ state: 'visible' })
  await revealDetails(page, 'Selection details')
  return scenario
}

const performanceRequests = (scenario) => scenario.requests.filter((request) => request.path === '/api/v1/performance')

test('Overview reflects native selection without obsolete adaptive supervisor status', async ({ page }) => {
  await openApplication(page)
  await expect(page.getByRole('region', { name: 'Active node' })).toContainText('Alpha')
  await expect(page.getByRole('region', { name: 'Active node' })).toContainText('Native automatic selection')
  await expect(page.getByText('Selection starting')).toHaveCount(0)
  await expect(page.getByText('Automatic quality', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Run benchmark' })).toHaveCount(0)
})

test('manual diagnostic polling remains Nodes-only', async ({ page }) => {
  const performance = performanceFixture(waitingAdaptive(), { manual: { state: 'running', phase: 'download' } })
  const scenario = await openApplication(page, { performance })
  const overviewCount = performanceRequests(scenario).length
  await page.waitForTimeout(1_150)
  expect(performanceRequests(scenario).length).toBe(overviewCount)
  await (await revealNavigation(page)).getByRole('button', { name: /^Nodes/ }).click()
  await page.waitForTimeout(1_150)
  const nodesCount = performanceRequests(scenario).length
  expect(nodesCount).toBeGreaterThan(overviewCount)
  await page.getByRole('button', { name: 'Overview' }).click()
  await page.waitForTimeout(1_150)
  expect(performanceRequests(scenario).length).toBe(nodesCount)
})
