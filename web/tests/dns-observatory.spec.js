import { expect, test } from '@playwright/test'

const origin = 'http://127.0.0.1:4173'
const csrfToken = 'synthetic-dns-csrf'

const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
const deferred = () => {
  let resolve
  const promise = new Promise((done) => { resolve = done })
  return { promise, resolve }
}

const projectionFor = (editability = 'editable') => ({
  editability,
  schemaVersion: 1,
  dns: {
    proxyResolverIds: editability === 'editable' ? ['synthetic-resolver-a', 'synthetic-resolver-b'] : [],
    fallbackMode: 'system',
    cacheEnabled: true,
    serveStale: true,
    staleTTLSeconds: 3600,
    parallelQueries: true,
    resolverCatalog: [
      { id: 'synthetic-resolver-a', label: 'Proxy resolver 1' },
      { id: 'synthetic-resolver-b', label: 'Proxy resolver 2' },
      { id: 'synthetic-resolver-c', label: 'Proxy resolver 3' },
    ],
    locked: { queryStrategy: 'UseIPv4', leakPreventionEnabled: true, systemFallbackPresent: true },
    proxyDomainCounts: { baseline: 14, derived: 2 },
  },
  observatory: { probeIntervalMinutes: editability === 'editable' ? 5 : 0, minIntervalMinutes: 1, maxIntervalMinutes: 5 },
})

const statusFor = (lifecycle = { maintenance: false, applying: false }) => ({
  controlPlane: { version: 'synthetic' }, xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true }, balancer: {}, observatory: { healthy: 0, total: 0, apiReachable: true },
  benchmark: { controlPlane: { running: false } }, selection: {}, setup: {}, lifecycle,
})

const routingProjection = () => ({
  editability: 'editable', schemaVersion: 1, rules: [],
  protected: { totalRuleCount: 5, prefixRuleCount: 4, customRegionRuleCount: 0, regionPlacement: 'immediately-before-final-catch-all', finalCatchAllPresent: true },
  dns: { proxyResolverCount: 2, baselineDomainCount: 14, derivedDomainCount: 2 }, observatory: { probeInterval: '5m' },
})

const routingDiff = (rules) => ({
  added: rules.map(({ name, action }) => ({ name, action })), removed: [], changed: [], reordered: [],
  beforeMatches: { rules: 0, domains: 0, ips: 0, protocols: 0, networks: 0, ports: 0 },
  afterMatches: { rules: rules.length, domains: 0, ips: 0, protocols: 0, networks: 0, ports: 0 },
  dnsDerivedDomainCountBefore: 2, dnsDerivedDomainCountAfter: 2, dnsDerivedDomainCountDelta: 0, restartRequired: rules.length > 0,
})

const dnsDiff = (candidate, current, overrides = {}) => ({
  dns: {
    resolverIdsAdded: candidate.dns.proxyResolverIds.filter((id) => !current.dns.proxyResolverIds.includes(id)),
    resolverIdsRemoved: current.dns.proxyResolverIds.filter((id) => !candidate.dns.proxyResolverIds.includes(id)),
    resolverIdsReordered: candidate.dns.proxyResolverIds,
    fallbackModeBefore: current.dns.fallbackMode, fallbackModeAfter: candidate.dns.fallbackMode,
    cacheEnabledBefore: current.dns.cacheEnabled, cacheEnabledAfter: candidate.dns.cacheEnabled,
    serveStaleBefore: current.dns.serveStale, serveStaleAfter: candidate.dns.serveStale,
    staleTTLSecondsBefore: current.dns.staleTTLSeconds, staleTTLSecondsAfter: candidate.dns.staleTTLSeconds,
    parallelQueriesBefore: current.dns.parallelQueries, parallelQueriesAfter: candidate.dns.parallelQueries,
  },
  observatory: { probeIntervalMinutesBefore: current.observatory.probeIntervalMinutes, probeIntervalMinutesAfter: candidate.observatory.probeIntervalMinutes },
  derivedProxyDomainCountBefore: 2, derivedProxyDomainCountAfter: 3, derivedProxyDomainCountDelta: 1,
  restartRequired: true,
  ...overrides,
})

async function prepare(page, options = {}) {
  const state = {
    policy: projectionFor(options.editability), requests: [], previews: new Map(), previewNumber: 0,
    routingPreviews: new Map(), routingNumber: 0, issues: [], handle: null,
    applyMode: options.applyMode || 'success', lifecycle: options.lifecycle || { maintenance: false, applying: false },
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
      case '/api/v1/session/login': return json(route, { csrfToken: `${csrfToken}-new` })
      case '/api/v1/session/logout': return json(route, {})
      case '/api/v1/status': return json(route, statusFor(state.lifecycle))
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/appliance/dns-observatory': return json(route, state.policy)
      case '/api/v1/appliance/dns-observatory/preview': {
        const token = `synthetic-dns-preview-${++state.previewNumber}`
        const candidate = entry.body
        const current = { dns: state.policy.dns, observatory: state.policy.observatory }
        const noop = JSON.stringify(candidate) === JSON.stringify({
          dns: { proxyResolverIds: current.dns.proxyResolverIds, fallbackMode: current.dns.fallbackMode, cacheEnabled: current.dns.cacheEnabled, serveStale: current.dns.serveStale, staleTTLSeconds: current.dns.staleTTLSeconds, parallelQueries: current.dns.parallelQueries },
          observatory: { probeIntervalMinutes: current.observatory.probeIntervalMinutes },
        })
        state.previews.set(token, { candidate, noop })
        return json(route, { previewToken: token, expiresAt: new Date(Date.now() + 300_000).toISOString(), noop, diff: dnsDiff(candidate, current, noop ? { derivedProxyDomainCountAfter: 2, derivedProxyDomainCountDelta: 0, restartRequired: false } : {}) })
      }
      case '/api/v1/appliance/dns-observatory/cancel': state.previews.delete(entry.body.previewToken); return json(route, { canceled: true })
      case '/api/v1/appliance/dns-observatory/apply': {
        const pending = state.previews.get(entry.body.previewToken)
        state.previews.delete(entry.body.previewToken)
        const errors = {
          busy: [409, 'busy'], stale: [409, 'preview-stale'], expired: [409, 'preview-expired'],
          drift: [409, 'drift-detected'], rejected: [502, 'candidate-rejected'], restored: [500, 'transaction-restored'], unknown: [503, 'transaction-unproven'],
        }
        if (state.applyMode === 'network') return route.abort('connectionfailed')
        if (errors[state.applyMode]) return json(route, { error: 'synthetic safe error', code: errors[state.applyMode][1] }, errors[state.applyMode][0])
        if (pending) state.policy = { ...state.policy, dns: { ...state.policy.dns, ...pending.candidate.dns }, observatory: { ...state.policy.observatory, ...pending.candidate.observatory } }
        const current = { dns: state.policy.dns, observatory: state.policy.observatory }
        return json(route, { noop: Boolean(pending?.noop), classification: pending?.noop ? 'no-op' : 'applied', diff: dnsDiff(pending?.candidate || current, current) })
      }
      case '/api/v1/appliance/policy': return json(route, routingProjection())
      case '/api/v1/appliance/policy/preview': {
        const token = `synthetic-routing-preview-${++state.routingNumber}`
        state.routingPreviews.set(token, entry.body.rules)
        return json(route, { previewToken: token, expiresAt: new Date(Date.now() + 300_000).toISOString(), noop: false, diff: routingDiff(entry.body.rules) })
      }
      case '/api/v1/appliance/policy/cancel': state.routingPreviews.delete(entry.body.previewToken); return json(route, { canceled: true })
      case '/api/v1/appliance/policy/apply': {
        const rules = state.routingPreviews.get(entry.body.previewToken) || []
        state.routingPreviews.delete(entry.body.previewToken)
        return json(route, { noop: false, classification: 'applied', diff: routingDiff(rules) })
      }
      default: return json(route, { error: `unexpected synthetic route: ${entry.path}` }, 404)
    }
  })
  return state
}

const requestsFor = (state, path, method) => state.requests.filter((request) => request.path === path && (!method || request.method === method))
async function openDNS(page) {
  await page.goto('/')
  await expect(page).toHaveTitle('XKeen Control')
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Typed resolver policy' })).toBeVisible()
}
async function makeChange(page) { await page.getByLabel('Parallel queries').uncheck() }

test.afterEach(async ({ page }) => {
  if (page.__dnsIssues) expect(page.__dnsIssues).toEqual([])
})

test('places DNS immediately after Routing, loads lazily, and never polls it', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await page.goto('/')
  await expect(page.locator('.section-nav button')).toHaveCount(8)
  expect(await page.locator('.section-nav button').allTextContents()).toEqual(['Overview', 'Nodes 0', 'Routing', 'DNS', 'Performance', 'Components / Updates', 'System', 'Backup & Restore'])
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory')).toHaveLength(0)
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory').length).toBe(1)
  await page.waitForTimeout(5_300)
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory')).toHaveLength(1)
})

test('renders only safe labels, locked facts, counts, and derived cadence choices', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await expect(page.getByText('Proxy resolver 1', { exact: true })).toBeVisible()
  await expect(page.getByText('UseIPv4', { exact: true })).toBeVisible()
  await expect(page.getByText('Present / locked', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Observatory cadence').locator('option')).toHaveCount(5)
  await expect(page.locator('body')).not.toContainText(/https?:\/\/|dns-query|domain:|subjectSelector|probeURL/i)
  await expect(page.locator('pre, textarea')).toHaveCount(0)
})

test('adds, removes, and moves resolvers while serializing exact ordered opaque IDs', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await page.getByRole('button', { name: 'Add Proxy resolver 3' }).click()
  await page.getByRole('button', { name: 'Move resolver up: Proxy resolver 3' }).click()
  await page.getByRole('button', { name: 'Remove resolver: Proxy resolver 1' }).click()
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/preview', 'POST')[0]).toMatchObject({
    csrf: csrfToken,
    body: { dns: { proxyResolverIds: ['synthetic-resolver-c', 'synthetic-resolver-b'] } },
  })
  expect(Object.keys(requestsFor(state, '/api/v1/appliance/dns-observatory/preview', 'POST')[0].body).sort()).toEqual(['dns', 'observatory'])
})

test('retains keyboard focus on a resolver reorder control across repeated moves', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await page.getByRole('button', { name: 'Add Proxy resolver 3' }).click()
  const moveUp = page.getByRole('button', { name: 'Move resolver up: Proxy resolver 3' })
  await moveUp.focus()
  await moveUp.press('Enter')
  await expect(moveUp).toBeFocused()
  await moveUp.press('Enter')
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/preview', 'POST')[0].body.dns.proxyResolverIds).toEqual([
    'synthetic-resolver-c', 'synthetic-resolver-a', 'synthetic-resolver-b',
  ])
})

test('prevents an empty resolver selection', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await page.getByRole('button', { name: 'Remove resolver: Proxy resolver 1' }).click()
  await expect(page.getByRole('button', { name: 'Remove resolver: Proxy resolver 2' })).toBeDisabled()
})

test('canonicalizes cache and stale controls and serializes fallback, parallel queries, and cadence', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await page.getByLabel('DNS fallback mode').selectOption('disabled')
  await page.getByLabel('Cache enabled').uncheck()
  await page.getByLabel('Parallel queries').uncheck()
  await page.getByLabel('Observatory cadence').selectOption('2')
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/preview', 'POST')[0].body).toEqual({
    dns: { proxyResolverIds: ['synthetic-resolver-a', 'synthetic-resolver-b'], fallbackMode: 'disabled', cacheEnabled: false, serveStale: false, staleTTLSeconds: 0, parallelQueries: false },
    observatory: { probeIntervalMinutes: 2 },
  })
})

test('canonicalizes Serve stale off and validates the enabled TTL bounds', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await page.getByLabel('Serve stale').uncheck()
  await expect(page.getByLabel('Stale TTL seconds')).toHaveValue('0')
  await page.getByLabel('Serve stale').check()
  await expect(page.getByLabel('Stale TTL seconds')).toHaveValue('60')
  await page.getByLabel('Stale TTL seconds').fill('59')
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeDisabled()
  await page.getByLabel('Stale TTL seconds').fill('86400')
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeEnabled()
})

test('requires explicit discard for dirty Refresh and rebases clean Refresh directly', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page); await makeChange(page)
  await page.getByRole('button', { name: 'Refresh DNS policy' }).click()
  await expect(page.getByRole('alert')).toContainText('Unsaved DNS changes')
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory')).toHaveLength(1)
  await page.getByRole('button', { name: 'Keep editing' }).click()
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await page.getByRole('button', { name: 'Refresh DNS policy' }).click()
  await page.getByRole('button', { name: 'Discard changes and refresh' }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory').length).toBe(2)
  await expect(page.getByLabel('Parallel queries')).toBeChecked()
  await page.getByRole('button', { name: 'Refresh DNS policy' }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory').length).toBe(3)
})

for (const editability of ['drift-detected', 'unavailable']) {
  test(`${editability} is read-only and never issues Preview`, async ({ page }) => {
    const state = await prepare(page, { editability }); page.__dnsIssues = state.issues
    await openDNS(page)
    await expect(page.getByRole('alert')).toContainText(editability === 'drift-detected' ? 'blocked' : 'unavailable')
    await expect(page.getByRole('region', { name: 'DNS and Observatory editor' })).toHaveCount(0)
    expect(requestsFor(state, '/api/v1/appliance/dns-observatory/preview', 'POST')).toHaveLength(0)
  })
}

test('renders every semantic Preview field with safe labels and unknown fallback', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  state.handle = async ({ route, entry }) => {
    if (entry.path !== '/api/v1/appliance/dns-observatory/preview') return false
    const current = { dns: state.policy.dns, observatory: state.policy.observatory }
    const diff = dnsDiff(entry.body, current)
    diff.dns.resolverIdsAdded = ['synthetic-resolver-c', 'unmapped-resolver']
    await json(route, { previewToken: 'semantic-token', expiresAt: new Date(Date.now() + 300_000).toISOString(), noop: false, diff })
    return true
  }
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  const preview = page.getByRole('region', { name: 'DNS Preview confirmation' })
  await expect(preview).toBeFocused()
  await expect(preview).toContainText('Proxy resolver 3')
  await expect(preview).toContainText('Unknown resolver selection')
  for (const label of ['Fallback', 'Cache', 'Serve stale', 'Stale TTL seconds', 'Parallel queries', 'Observatory cadence', 'Derived proxy domains', 'Runtime restart']) await expect(preview).toContainText(label)
})

test('no-op Preview offers no Apply and Cancel consumes the token', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await expect(page.getByRole('heading', { name: 'No effective changes' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply DNS changes' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Cancel Preview' }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST')[0].body).toEqual({ previewToken: 'synthetic-dns-preview-1' })
})

test('Apply consumes the token once, sends token only, and refreshes on success', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await page.getByRole('button', { name: 'Apply DNS changes' }).dblclick()
  await expect(page.getByTestId('dns-result')).toContainText('changes applied')
  const apply = requestsFor(state, '/api/v1/appliance/dns-observatory/apply', 'POST')
  expect(apply).toHaveLength(1)
  expect(apply[0]).toMatchObject({ csrf: csrfToken, body: { previewToken: 'synthetic-dns-preview-1' } })
  expect(Object.keys(apply[0].body)).toEqual(['previewToken'])
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory').length).toBe(2)
})

test('navigation invalidates completed and in-flight Preview and late tokens cannot resurrect', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  const release = deferred()
  state.handle = async ({ route, entry }) => {
    if (entry.path !== '/api/v1/appliance/dns-observatory/preview') return false
    await release.promise
    const current = { dns: state.policy.dns, observatory: state.policy.observatory }
    await json(route, { previewToken: 'late-dns-token', expiresAt: new Date(Date.now() + 300_000).toISOString(), noop: false, diff: dnsDiff(entry.body, current) })
    return true
  }
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  release.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST').length).toBe(1)
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByRole('region', { name: 'DNS Preview confirmation' })).toHaveCount(0)
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/apply', 'POST')).toHaveLength(0)
})

test('session change clears DNS state and cancels a completed Preview under the old binding', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST')[0].csrf).toBe(csrfToken)
})

test('a delayed synchronous Apply survives navigation and global notice opens the same state', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  const release = deferred()
  state.handle = async ({ route, entry }) => {
    if (entry.path !== '/api/v1/appliance/dns-observatory/apply') return false
    await release.promise
    const current = { dns: state.policy.dns, observatory: state.policy.observatory }
    await json(route, { noop: false, classification: 'applied', diff: dnsDiff(current, current) })
    return true
  }
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click(); await page.getByRole('button', { name: 'Apply DNS changes' }).click()
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await expect(page.getByText('DNS Apply is running', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Open DNS' }).click()
  await expect(page.getByText('DNS Apply is running', { exact: true })).toBeVisible()
  release.resolve()
  await expect(page.getByTestId('dns-result')).toContainText('changes applied')
})

test('network loss after Apply becomes persistent unknown and is never replayed', async ({ page }) => {
  const state = await prepare(page, { applyMode: 'network' }); page.__dnsIssues = state.issues
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click(); await page.getByRole('button', { name: 'Apply DNS changes' }).click()
  await expect(page.getByTestId('dns-result')).toContainText('outcome is unknown')
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/apply', 'POST')).toHaveLength(1)
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Open DNS' })).toBeVisible()
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/apply', 'POST')).toHaveLength(1)
})

for (const [mode, expected] of [['busy', 'Lifecycle is busy'], ['stale', 'Preview is stale'], ['expired', 'Preview expired'], ['drift', 'drift detected'], ['rejected', 'candidate rejected'], ['restored', 'were restored'], ['unknown', 'outcome is unknown']]) {
  test(`maps ${mode} Apply conservatively`, async ({ page }) => {
    const state = await prepare(page, { applyMode: mode }); page.__dnsIssues = state.issues
    await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click(); await page.getByRole('button', { name: 'Apply DNS changes' }).click()
    await expect(page.getByTestId('dns-result')).toContainText(expected, { ignoreCase: true })
    expect(requestsFor(state, '/api/v1/appliance/dns-observatory/apply', 'POST')).toHaveLength(1)
  })
}

test('lifecycle maintenance disables new mutation without hiding the safe draft', async ({ page }) => {
  const state = await prepare(page, { lifecycle: { maintenance: true, applying: false } }); page.__dnsIssues = state.issues
  await openDNS(page)
  await expect(page.getByText('Proxy resolver 1', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeDisabled()
  expect(requestsFor(state, '/api/v1/appliance/dns-observatory/preview', 'POST')).toHaveLength(0)
})

test('DNS Apply cannot leave a Routing Preview actionable and preserves the Routing draft', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Preserved routing draft')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toBeVisible()
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await makeChange(page)
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await page.getByRole('button', { name: 'Apply DNS changes' }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('Preserved routing draft')
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
})

test('Routing Apply cannot leave a DNS Preview actionable and preserves the DNS draft', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page)
  await makeChange(page)
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await expect(page.getByRole('region', { name: 'DNS Preview confirmation' })).toBeVisible()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await page.getByRole('button', { name: 'Apply routing changes' }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST').length).toBe(1)
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByRole('region', { name: 'DNS Preview confirmation' })).toHaveCount(0)
})

test('Routing and DNS peer Preview invalidation cancels late peer tokens without discarding drafts', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  const release = deferred()
  state.handle = async ({ route, entry }) => {
    if (entry.path !== '/api/v1/appliance/dns-observatory/preview') return false
    await release.promise
    const current = { dns: state.policy.dns, observatory: state.policy.observatory }
    await json(route, { previewToken: 'peer-late-token', expiresAt: new Date(Date.now() + 300_000).toISOString(), noop: false, diff: dnsDiff(entry.body, current) })
    return true
  }
  await openDNS(page); await makeChange(page); await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await page.getByRole('button', { name: 'Add rule' }).click()
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await page.getByRole('button', { name: 'Apply routing changes' }).click()
  release.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST').length).toBe(1)
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByRole('region', { name: 'DNS Preview confirmation' })).toHaveCount(0)
})

test('an unproven DNS Apply re-invalidates Routing and blocks both workspaces until the DNS fresh read succeeds', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  const releaseApply = deferred()
  const releaseFreshRead = deferred()
  let dnsReads = 0
  const freshDNSProjection = {
    ...state.policy,
    dns: { ...state.policy.dns, proxyDomainCounts: { ...state.policy.dns.proxyDomainCounts, derived: 7 } },
  }
  state.handle = async ({ route, entry }) => {
    if (entry.path === '/api/v1/appliance/dns-observatory') {
      dnsReads++
      if (dnsReads === 2) await releaseFreshRead.promise
      await json(route, dnsReads === 2 ? freshDNSProjection : state.policy)
      return true
    }
    if (entry.path === '/api/v1/appliance/dns-observatory/apply') {
      await releaseApply.promise
      await json(route, { error: 'synthetic safe error', code: 'transaction-unproven' }, 503)
      return true
    }
    return false
  }

  await openDNS(page)
  await makeChange(page)
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await page.getByRole('button', { name: 'Apply DNS changes' }).click()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Preserved after DNS uncertainty')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toBeVisible()

  releaseApply.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  await expect.poll(() => dnsReads).toBe(2)
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('Preserved after DNS uncertainty')
  await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeDisabled()

  releaseFreshRead.resolve()
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeEnabled()
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByRole('region', { name: 'DNS and Observatory editor' }).getByText('Unsaved')).toBeVisible()
  await expect(page.getByRole('region', { name: 'DNS source-owned facts' }).getByText('7', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('Preserved after DNS uncertainty')
  await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeEnabled()
})

test('an unknown Routing Apply re-invalidates DNS and blocks both workspaces until the Routing fresh read succeeds', async ({ page }) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  const releaseApply = deferred()
  const releaseFreshRead = deferred()
  let routingReads = 0
  const freshRoutingProjection = routingProjection()
  freshRoutingProjection.protected.prefixRuleCount = 6
  state.handle = async ({ route, entry }) => {
    if (entry.path === '/api/v1/appliance/policy') {
      routingReads++
      if (routingReads === 2) await releaseFreshRead.promise
      await json(route, routingReads === 2 ? freshRoutingProjection : routingProjection())
      return true
    }
    if (entry.path === '/api/v1/appliance/policy/apply') {
      await releaseApply.promise
      await route.abort('connectionfailed')
      return true
    }
    return false
  }

  await page.goto('/')
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Routing outcome pending')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await page.getByRole('button', { name: 'Apply routing changes' }).click()
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await makeChange(page)
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await expect(page.getByRole('region', { name: 'DNS Preview confirmation' })).toBeVisible()

  releaseApply.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/dns-observatory/cancel', 'POST').length).toBe(1)
  await expect.poll(() => routingReads).toBe(2)
  await expect(page.getByRole('region', { name: 'DNS Preview confirmation' })).toHaveCount(0)
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeDisabled()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('Routing outcome pending')
  await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeDisabled()

  releaseFreshRead.resolve()
  await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeEnabled()
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('Routing outcome pending')
  await expect(page.getByRole('region', { name: 'Custom routing rule editor' }).getByText('Unsaved')).toBeVisible()
  await expect(page.getByRole('region', { name: 'Routing source-owned facts' }).getByText('6', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeEnabled()
})

test('stores no DNS draft or token state and remains usable at desktop and mobile widths', async ({ page }, testInfo) => {
  const state = await prepare(page); page.__dnsIssues = state.issues
  await openDNS(page); await makeChange(page)
  expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
  await page.screenshot({ path: testInfo.outputPath('dns-desktop.png'), fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('region', { name: 'DNS and Observatory editor' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('dns-mobile.png'), fullPage: true })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
})
