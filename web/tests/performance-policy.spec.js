import { expect, test } from '@playwright/test'
import path from 'node:path'

const origin = 'http://127.0.0.1:4173'
const csrfToken = 'synthetic-performance-policy-csrf'
const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })

const defaultPolicy = () => ({
  schemaVersion: 1,
  probeIntervalSeconds: 60,
  failureThreshold: 2,
  adaptiveCadenceMinutes: 180,
  adaptiveChallengerLimit: 5,
  minimumDwellMinutes: 30,
  qualityHysteresisPercent: 10,
})

const projection = (overrides = {}) => ({
  policy: defaultPolicy(),
  source: 'default',
  hardCeilings: {
    maxCandidates: 6,
    candidateDownloadMiB: 16,
    candidateUploadMiB: 8,
    candidateMaxSeconds: 30,
    generationMaxMiB: 144,
    generationMaxSeconds: 180,
    transportIdentity: 'source-owned',
    rttGuard: 'source-owned',
    scoring: 'source-owned',
  },
  adaptive: { state: 'waiting', nextRunAt: new Date(Date.now() + 180 * 60_000).toISOString(), generation: 3 },
  ...overrides,
})

const status = (lifecycle = { maintenance: false, applying: false }, benchmarkRunning = false) => ({
  controlPlane: { version: 'synthetic' },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  balancer: {}, observatory: { healthy: 0, total: 0, apiReachable: true },
  benchmark: { controlPlane: { running: benchmarkRunning } }, selection: {}, setup: {}, lifecycle,
})

const dashboardPerformance = (overrides = {}) => ({
  nodes: [], manual: { state: 'idle', phase: 'done' }, adaptive: { state: 'waiting' }, ...overrides,
})

const changesFor = (before, after) => Object.keys(before)
  .filter((field) => field !== 'schemaVersion' && before[field] !== after[field])
  .map((field) => ({ field, before: before[field], after: after[field] }))

async function prepare(page, options = {}) {
  const state = {
    projection: projection(options.projection),
    lifecycle: options.lifecycle || { maintenance: false, applying: false },
    performance: dashboardPerformance(options.performance),
    benchmarkRunning: Boolean(options.benchmarkRunning),
    requests: [], previews: new Map(), previewNumber: 0, writes: 0,
    applyMode: options.applyMode || 'success', issues: [], handle: null,
  }
  page.on('pageerror', (error) => state.issues.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (['error', 'warning'].includes(message.type()) && !message.text().startsWith('Failed to load resource:')) state.issues.push(`${message.type()}: ${message.text()}`)
  })
  await page.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== origin) return route.abort('blockedbyclient')
    if (!url.pathname.startsWith('/api/')) return route.continue()
    let body = null
    if (request.postData()) {
      try { body = request.postDataJSON() } catch { body = request.postData() }
    }
    const entry = { path: url.pathname, method: request.method(), body, csrf: request.headers()['x-csrf-token'] || '' }
    state.requests.push(entry)
    if (state.handle && await state.handle({ route, entry, state })) return
    switch (entry.path) {
      case '/api/v1/session': return json(route, { csrfToken })
      case '/api/v1/session/logout': return json(route, {})
      case '/api/v1/status': return json(route, status(state.lifecycle, state.benchmarkRunning))
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, state.performance)
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/performance/policy': return json(route, state.projection)
      case '/api/v1/performance/policy/preview': {
        const token = `synthetic-performance-preview-${++state.previewNumber}`
        const before = state.projection.policy
        const after = entry.body
        const changes = changesFor(before, after)
        const noop = changes.length === 0 && !state.projection.reasonCode
        state.previews.set(token, { after, changes, noop })
        return json(route, {
          previewToken: token,
          expiresAt: new Date(Date.now() + 300_000).toISOString(),
          before, after, changes, noop, restartRequired: false, nextRunTimeChanges: !noop,
        })
      }
      case '/api/v1/performance/policy/cancel':
        state.previews.delete(entry.body.previewToken)
        return json(route, { canceled: true })
      case '/api/v1/performance/policy/apply': {
        const pending = state.previews.get(entry.body.previewToken)
        state.previews.delete(entry.body.previewToken)
        const failures = { busy: [409, 'busy'], stale: [409, 'preview-stale'], expired: [409, 'preview-expired'], unavailable: [503, 'unavailable'] }
        if (state.applyMode === 'network') return route.abort('connectionfailed')
        if (failures[state.applyMode]) return json(route, { error: 'synthetic safe error', code: failures[state.applyMode][1] }, failures[state.applyMode][0])
        if (pending && !pending.noop) {
          state.writes++
          state.projection = { ...state.projection, policy: pending.after, source: 'persisted', reasonCode: undefined, adaptive: { ...state.projection.adaptive, nextRunAt: new Date(Date.now() + pending.after.adaptiveCadenceMinutes * 60_000).toISOString() } }
        }
        return json(route, { policy: pending?.after || state.projection.policy, source: pending?.noop ? state.projection.source : 'persisted', changes: pending?.changes || [], noop: Boolean(pending?.noop), restartRequired: false, nextRunTimeChanged: !pending?.noop })
      }
      default: return json(route, { error: `unexpected synthetic route: ${entry.path}` }, 404)
    }
  })
  return state
}

const requestsFor = (state, path, method) => state.requests.filter((request) => request.path === path && (!method || request.method === method))

async function openPerformance(page) {
  await page.goto('/')
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await page.getByRole('button', { name: 'Performance', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Bounded selection policy' })).toBeVisible()
}

test.afterEach(async ({ page }) => {
  if (page.__performancePolicyIssues) expect(page.__performancePolicyIssues).toEqual([])
})

test('places Performance immediately after DNS and reads its policy lazily without dashboard polling', async ({ page }) => {
  const state = await prepare(page); page.__performancePolicyIssues = state.issues
  await page.goto('/')
  await expect(page.locator('.section-nav button')).toHaveCount(8)
  expect(await page.locator('.section-nav button').allTextContents()).toEqual(['Overview', 'Nodes 0', 'Routing', 'DNS', 'Performance', 'Components / Updates', 'System', 'Backup & Restore'])
  expect(requestsFor(state, '/api/v1/performance/policy')).toHaveLength(0)
  await page.getByRole('button', { name: 'Performance', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/performance/policy').length).toBe(1)
  await page.waitForTimeout(5_300)
  expect(requestsFor(state, '/api/v1/performance/policy')).toHaveLength(1)
})

test('renders only six bounded fields and separate source-owned hard ceilings', async ({ page }) => {
  const state = await prepare(page); page.__performancePolicyIssues = state.issues
  await openPerformance(page)
  for (const label of ['Active probe interval', 'Failure threshold', 'Adaptive cadence', 'Adaptive challengers', 'Minimum dwell', 'Quality hysteresis']) {
    await expect(page.getByLabel(label)).toBeVisible()
  }
  await expect(page.getByRole('region', { name: 'Source-owned performance ceilings' })).toContainText('16 MiB')
  await expect(page.getByRole('region', { name: 'Source-owned performance ceilings' })).toContainText('144 MiB')
  await expect(page.getByRole('region', { name: 'Source-owned performance ceilings' })).toContainText('Source-owned')
  await expect(page.locator('input')).toHaveCount(6)
  await expect(page.locator('input[aria-label*="URL" i], input[aria-label*="schedule" i], input[aria-label*="payload" i], input[aria-label*="budget" i], input[aria-label*="timeout" i]')).toHaveCount(0)
  await expect(page.locator('textarea, pre')).toHaveCount(0)
  if (process.env.PERFORMANCE_POLICY_SCREENSHOT_DIR) {
    await page.screenshot({ path: path.join(process.env.PERFORMANCE_POLICY_SCREENSHOT_DIR, 'performance-policy-desktop.png'), fullPage: true })
    await page.setViewportSize({ width: 390, height: 844 })
    await page.screenshot({ path: path.join(process.env.PERFORMANCE_POLICY_SCREENSHOT_DIR, 'performance-policy-mobile.png'), fullPage: true })
  }
})

test('sends the exact policy DTO, previews semantics, and applies only a one-shot token without restart or benchmark', async ({ page }) => {
  const state = await prepare(page); page.__performancePolicyIssues = state.issues
  await openPerformance(page)
  await page.getByLabel('Active probe interval').fill('120')
  await page.getByLabel('Failure threshold').fill('4')
  await page.getByRole('button', { name: 'Preview performance changes' }).click()
  await expect(page.getByRole('heading', { name: 'Review performance policy changes' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Performance policy Preview' })).toContainText('60 → 120')
  await expect(page.getByRole('region', { name: 'Performance policy Preview' })).toContainText('Runtime restart: No')
  const preview = requestsFor(state, '/api/v1/performance/policy/preview', 'POST').at(-1)
  expect(preview.csrf).toBe(csrfToken)
  expect(preview.body).toEqual({ schemaVersion: 1, probeIntervalSeconds: 120, failureThreshold: 4, adaptiveCadenceMinutes: 180, adaptiveChallengerLimit: 5, minimumDwellMinutes: 30, qualityHysteresisPercent: 10 })

  await page.getByRole('button', { name: 'Apply performance policy' }).click()
  await expect(page.getByTestId('performance-policy-result')).toContainText('Performance policy applied')
  const apply = requestsFor(state, '/api/v1/performance/policy/apply', 'POST').at(-1)
  expect(Object.keys(apply.body)).toEqual(['previewToken'])
  expect(apply.csrf).toBe(csrfToken)
  expect(state.writes).toBe(1)
  expect(requestsFor(state, '/api/v1/benchmark/run', 'POST')).toHaveLength(0)
  expect(requestsFor(state, '/api/v1/selection/override', 'POST')).toHaveLength(0)
})

test('requires explicit dirty Refresh confirmation and preserves the draft when canceled', async ({ page }) => {
  const state = await prepare(page); page.__performancePolicyIssues = state.issues
  await openPerformance(page)
  await page.getByLabel('Minimum dwell').fill('90')
  await page.getByRole('button', { name: 'Refresh policy' }).click()
  await expect(page.getByText('Unsaved performance changes')).toBeVisible()
  expect(requestsFor(state, '/api/v1/performance/policy')).toHaveLength(1)
  await page.getByRole('button', { name: 'Keep editing' }).click()
  await expect(page.getByLabel('Minimum dwell')).toHaveValue('90')
  await page.getByRole('button', { name: 'Refresh policy' }).click()
  await page.getByRole('button', { name: 'Discard changes and refresh' }).click()
  await expect(page.getByLabel('Minimum dwell')).toHaveValue('30')
  expect(requestsFor(state, '/api/v1/performance/policy')).toHaveLength(2)
})

test('visibly fails closed on invalid persistence and explicitly replaces it through Preview and Apply', async ({ page }) => {
  const state = await prepare(page, { projection: { source: 'default', reasonCode: 'policy-invalid' } }); page.__performancePolicyIssues = state.issues
  await openPerformance(page)
  await expect(page.getByText('Persisted policy failed closed to source defaults.')).toBeVisible()
  await page.getByRole('button', { name: 'Preview performance changes' }).click()
  await expect(page.getByRole('heading', { name: 'Review performance policy changes' })).toBeVisible()
  await page.getByRole('button', { name: 'Apply performance policy' }).click()
  await expect(page.getByTestId('performance-policy-result')).toContainText('Performance policy applied')
  expect(state.writes).toBe(1)
  await expect(page.getByText('Persisted policy failed closed to source defaults.')).toHaveCount(0)
})

test('no-op Apply writes nothing and busy/lifecycle states gate mutation', async ({ page }) => {
  const state = await prepare(page); page.__performancePolicyIssues = state.issues
  await openPerformance(page)
  await page.getByRole('button', { name: 'Preview performance changes' }).click()
  await expect(page.getByRole('heading', { name: 'No effective policy change' })).toBeVisible()
  await page.getByRole('button', { name: 'Confirm no-op' }).click()
  await expect(page.getByTestId('performance-policy-result')).toContainText('Policy already effective')
  expect(state.writes).toBe(0)

  state.performance = dashboardPerformance({ manual: { state: 'running', phase: 'download' } })
  await page.reload()
  await page.getByRole('button', { name: 'Performance', exact: true }).click()
  await expect(page.getByText('A manual, adaptive or legacy performance owner is active.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview performance changes' })).toBeDisabled()
})

test('consumes failed Apply tokens without replay and keeps browser storage empty on desktop and mobile', async ({ page }) => {
  const state = await prepare(page, { applyMode: 'busy' }); page.__performancePolicyIssues = state.issues
  await openPerformance(page)
  await page.getByLabel('Adaptive cadence').fill('360')
  await page.getByRole('button', { name: 'Preview performance changes' }).click()
  await page.getByRole('button', { name: 'Apply performance policy' }).click()
  await expect(page.getByTestId('performance-policy-result')).toContainText('one-shot token was consumed')
  expect(requestsFor(state, '/api/v1/performance/policy/apply', 'POST')).toHaveLength(1)
  await page.waitForTimeout(100)
  expect(requestsFor(state, '/api/v1/performance/policy/apply', 'POST')).toHaveLength(1)
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
  expect((await page.screenshot()).byteLength).toBeGreaterThan(1_000)

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: 'Bounded selection policy' })).toBeVisible()
  expect((await page.screenshot({ fullPage: true })).byteLength).toBeGreaterThan(1_000)
})
