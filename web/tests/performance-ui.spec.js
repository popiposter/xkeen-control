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
  setup: { runtime: 'running', credential: 'ready', xkeen: 'ready', xray: 'ready', configuration: 'ready' },
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
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  return scenario
}

const performanceRequests = (scenario) => scenario.requests.filter((request) => request.path === '/api/v1/performance')

test('Overview presents automatic quality and never mounts the legacy benchmark trigger', async ({ page }) => {
  const scenario = await openApplication(page)
  const overview = page.getByTestId('automatic-quality-overview')
  await expect(overview).toContainText('Automatic quality')
  await expect(overview).toContainText('Automatic stable selection')
  await expect(page.getByRole('button', { name: /Run full benchmark/i })).toHaveCount(0)
  await expect(page.getByText('Selection & benchmark', { exact: true })).toHaveCount(0)
  expect(scenario.requests.some((request) => request.path === '/api/v1/benchmark/run' && request.method === 'POST')).toBe(false)
})

test('waiting adaptive state shows its due time without fabricated candidate metrics', async ({ page }) => {
  const scenario = await openApplication(page, { performance: performanceFixture(waitingAdaptive({ reasonCode: 'no-current-target' })) })
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Next adaptive check')
  await page.getByRole('button', { name: 'Nodes' }).click()
  const card = page.getByTestId('adaptive-performance')
  await expect(card).toContainText('Waiting for next check')
  await expect(card).toContainText('next scheduled adaptive check')
  await expect(card.getByTestId('adaptive-candidate')).toHaveCount(0)
  await expect(card).not.toContainText('Download')
  expect(performanceRequests(scenario).length).toBeGreaterThanOrEqual(1)
})

test('running adaptive quality uses one-second performance-only polling and gates manual diagnostics', async ({ page }) => {
  const performance = performanceFixture({
    state: 'running',
    generation: 4,
    currentTarget: 'proxy-alpha',
    shortlistCount: 2,
    validCount: 1,
    candidates: [
      { tag: 'proxy-alpha', rttMs: 42, downloadBps: 25_000_000, uploadBps: 8_000_000, score: 0.72, valid: true },
      { tag: 'proxy-beta', rttMs: 58, downloadBps: 20_000_000, uploadBps: 7_000_000, score: 0, valid: false },
    ],
  })
  const scenario = await openApplication(page, { performance })
  const initialStatus = scenario.counts['/api/v1/status']
  const initialNodes = scenario.counts['/api/v1/nodes']
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Measuring adaptive quality')
  await page.waitForTimeout(1_250)
  expect(performanceRequests(scenario).length).toBeGreaterThan(1)
  expect(scenario.counts['/api/v1/status']).toBe(initialStatus)
  expect(scenario.counts['/api/v1/nodes']).toBe(initialNodes)

  await page.getByRole('button', { name: 'Nodes' }).click()
  await page.getByRole('checkbox', { name: 'Select Alpha' }).check()
  await expect(page.getByRole('button', { name: 'Full speed test' })).toBeDisabled()
  const nodesStatus = scenario.counts['/api/v1/status']
  const nodesList = scenario.counts['/api/v1/nodes']
  await page.waitForTimeout(1_150)
  expect(performanceRequests(scenario).length).toBeGreaterThan(2)
  expect(scenario.counts['/api/v1/status']).toBe(nodesStatus)
  expect(scenario.counts['/api/v1/nodes']).toBe(nodesList)
})

test('terminal adaptive state stops active polling and navigation stops it immediately', async ({ page }) => {
  const running = performanceFixture({ state: 'running', currentTarget: 'proxy-alpha', shortlistCount: 1, candidates: [{ tag: 'proxy-alpha', rttMs: 40, downloadBps: 1, uploadBps: 1, score: 1, valid: true }] })
  const completed = performanceFixture({ state: 'completed', completedAt: new Date().toISOString(), reasonCode: 'no-switch', shortlistCount: 1, validCount: 1, currentTarget: 'proxy-alpha', candidates: [{ tag: 'proxy-alpha', rttMs: 40, downloadBps: 1, uploadBps: 1, score: 1, valid: true }] })
  const scenario = await openApplication(page, { performanceSequence: [running, completed] })
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Completed')
  const terminalCount = performanceRequests(scenario).length
  await page.waitForTimeout(1_200)
  expect(performanceRequests(scenario).length).toBe(terminalCount)

  scenario.performance = running
  scenario.performanceSequence = null
  await page.reload()
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Measuring adaptive quality')
  const beforeNavigation = performanceRequests(scenario).length
  await page.getByRole('button', { name: 'Components / Updates' }).click()
  await page.waitForTimeout(1_150)
  expect(performanceRequests(scenario).length).toBe(beforeNavigation)
})

test('completed switch exposes only the actual switched target and bounded candidate evidence', async ({ page }) => {
  const performance = performanceFixture({
    state: 'completed',
    completedAt: new Date().toISOString(),
    generation: 7,
    currentTarget: 'proxy-alpha',
    selectedTarget: 'proxy-beta',
    switchApplied: true,
    reasonCode: 'adaptive-quality',
    shortlistCount: 2,
    validCount: 2,
    candidates: [
      { tag: 'proxy-alpha', rttMs: 44, downloadBps: 30_000_000, uploadBps: 10_000_000, score: 0.61, valid: true },
      { tag: 'proxy-beta', rttMs: 48, downloadBps: 48_000_000, uploadBps: 14_000_000, score: 0.94, valid: true },
    ],
  })
  const scenario = await openApplication(page, { performance })
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Actual switched target')
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Beta')
  await page.getByRole('button', { name: 'Nodes' }).click()
  const card = page.getByTestId('adaptive-performance')
  await expect(card).toContainText('Switched target')
  await expect(card.getByTestId('adaptive-candidate')).toHaveCount(2)
  await expect(card).toContainText('384.0 Mbps')
  expect(scenario.requests.some((request) => request.path === '/api/v1/benchmark/run')).toBe(false)
})

test('completed no-switch does not label an intermediate winner as selected', async ({ page }) => {
  const performance = performanceFixture({
    state: 'completed',
    completedAt: new Date().toISOString(),
    currentTarget: 'proxy-alpha',
    selectedTarget: 'proxy-beta',
    switchApplied: false,
    reasonCode: 'hysteresis',
    shortlistCount: 2,
    validCount: 2,
    candidates: [
      { tag: 'proxy-alpha', rttMs: 44, downloadBps: 30_000_000, uploadBps: 10_000_000, score: 0.61, valid: true },
      { tag: 'proxy-beta', rttMs: 48, downloadBps: 48_000_000, uploadBps: 14_000_000, score: 0.94, valid: true },
    ],
  })
  await openApplication(page, { performance })
  const overview = page.getByTestId('automatic-quality-overview')
  await expect(overview).toContainText('No target switch')
  await expect(overview).not.toContainText('Actual switched target')
  await page.getByRole('button', { name: 'Nodes' }).click()
  await expect(page.getByTestId('adaptive-performance')).not.toContainText('Switched target')
})

test('manual override and every unrecognized adaptive input degrade to safe labels', async ({ page }) => {
  const status = statusFixture({
    balancer: { nativeSelected: 'proxy-alpha', effective: 'proxy-beta', override: 'proxy-beta' },
    selection: { state: 'manual', effectiveTarget: 'proxy-beta', manualOverride: 'proxy-beta', lastSwitchReason: 'manual-override' },
  })
  const performance = performanceFixture({ state: 'skipped', reasonCode: 'manual-override', nextRunAt: new Date(Date.now() + 3_600_000).toISOString() })
  const scenario = await openApplication(page, { status, performance })
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('Explicit manual override')
  await expect(page.getByTestId('automatic-quality-overview')).toContainText('paused by the explicit manual override')
  await page.getByRole('button', { name: 'Nodes' }).click()
  await expect(page.getByTestId('adaptive-performance')).toContainText('Adaptive generation skipped')

  const hostile = performanceFixture({
    state: 'mystery-state',
    reasonCode: 'provider material https://secret.invalid/token',
    candidates: [
      { tag: 'proxy-alpha', rttMs: -4, downloadBps: null, uploadBps: -2, score: null, valid: false },
      { tag: 'https://secret.invalid', rttMs: 1, downloadBps: 1, uploadBps: 1, score: 1, valid: true },
      ...Array.from({ length: 7 }, (_, index) => ({ tag: `proxy-extra-${index}`, rttMs: 10 + index, downloadBps: 10_000_000, uploadBps: 2_000_000, score: 0.5, valid: true })),
    ],
  })
  scenario.performance = hostile
  await page.reload()
  await page.getByRole('button', { name: 'Nodes' }).click()
  const card = page.getByTestId('adaptive-performance')
  await expect(card).toContainText('Adaptive state unavailable')
  await expect(card).toContainText('Adaptive result unavailable')
  await expect(card.getByTestId('adaptive-candidate')).toHaveCount(6)
  await expect(card).not.toContainText('secret.invalid')
  await expect(card).not.toContainText('NaN')
  await expect(card).not.toContainText('Infinity')
  await expect(card).not.toContainText('-4 ms')
})

test('missing nodes retain only the safe canonical tag and browser storage stays empty on mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const performance = performanceFixture({
    state: 'completed',
    reasonCode: 'no-switch',
    currentTarget: 'proxy-missing',
    shortlistCount: 1,
    validCount: 1,
    candidates: [{ tag: 'proxy-missing', rttMs: 55, downloadBps: 12_000_000, uploadBps: 3_000_000, score: 0.5, valid: true }],
  })
  await openApplication(page, { performance })
  await page.getByRole('button', { name: 'Nodes' }).click()
  const card = page.getByTestId('adaptive-performance')
  await expect(card).toContainText('proxy-missing')
  await expect(card).not.toContainText('Unnamed node')
  await expect(card.getByTestId('adaptive-candidate')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Full speed test' })).toBeVisible()
  await expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
})

test('manual diagnostic polling remains Nodes-only', async ({ page }) => {
  const performance = performanceFixture(waitingAdaptive(), { manual: { state: 'running', phase: 'download' } })
  const scenario = await openApplication(page, { performance })
  const overviewCount = performanceRequests(scenario).length
  await page.waitForTimeout(1_150)
  expect(performanceRequests(scenario).length).toBe(overviewCount)
  await page.getByRole('button', { name: 'Nodes' }).click()
  await page.waitForTimeout(1_150)
  const nodesCount = performanceRequests(scenario).length
  expect(nodesCount).toBeGreaterThan(overviewCount)
  await page.getByRole('button', { name: 'Overview' }).click()
  await page.waitForTimeout(1_150)
  expect(performanceRequests(scenario).length).toBe(nodesCount)
})
