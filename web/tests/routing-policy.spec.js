import { expect, test } from '@playwright/test'

const origin = 'http://127.0.0.1:4173'
const csrfToken = 'synthetic-routing-csrf'

const json = (route, value, status = 200) => route.fulfill({
  status,
  contentType: 'application/json',
  body: JSON.stringify(value),
})

const deferred = () => {
  let resolve
  const promise = new Promise((done) => { resolve = done })
  return { promise, resolve }
}

const deferredPreviewHandler = (release) => async ({ route, entry, state }) => {
  if (entry.path !== '/api/v1/appliance/policy/preview') return false
  await release.promise
  const rules = entry.body.rules
  const previewToken = `synthetic-routing-preview-${++state.previewNumber}`
  state.previews.set(previewToken, { rules, noop: false })
  await json(route, { previewToken, expiresAt: new Date(Date.now() + 300_000).toISOString(), noop: false, diff: diff(rules) })
  return true
}

const rule = (name, action = 'proxy') => ({
  name,
  domains: [`domain:${name.toLowerCase()}.example`],
  ips: [],
  protocols: [],
  networks: [],
  ports: [],
  action,
})

const diff = (rules, { noop = false } = {}) => ({
  added: noop ? [] : rules.map(({ name, action }) => ({ name, action })),
  removed: [],
  changed: [],
  reordered: [],
  beforeMatches: { rules: 0, domains: 0, ips: 0, protocols: 0, networks: 0, ports: 0 },
  afterMatches: { rules: rules.length, domains: rules.length, ips: 0, protocols: 0, networks: 0, ports: 0 },
  dnsDerivedDomainCountBefore: 1,
  dnsDerivedDomainCountAfter: 1 + rules.filter((item) => item.action === 'proxy').length,
  dnsDerivedDomainCountDelta: rules.filter((item) => item.action === 'proxy').length,
  restartRequired: !noop,
})

const projectionFor = (editability = 'editable', rules = []) => ({
  editability,
  schemaVersion: 1,
  rules,
  protected: {
    totalRuleCount: 5,
    prefixRuleCount: 4,
    customRegionRuleCount: rules.length,
    regionPlacement: 'immediately-before-final-catch-all',
    finalCatchAllPresent: true,
  },
  dns: { proxyResolverCount: 2, baselineDomainCount: 14, derivedDomainCount: rules.filter((item) => item.action === 'proxy').length },
  observatory: { probeInterval: '60s' },
})

const statusFor = (lifecycle = { maintenance: false, applying: false }) => ({
  controlPlane: { version: 'synthetic' },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  balancer: {},
  observatory: { healthy: 0, total: 0, apiReachable: true },
  benchmark: { controlPlane: { running: false } },
  selection: {},
  setup: {},
  lifecycle,
})

async function prepare(page, options = {}) {
  const state = {
    policy: projectionFor(options.editability || 'editable', options.rules || []),
    requests: [],
    previewNumber: 0,
    previews: new Map(),
    issues: [],
    handle: null,
    applyMode: options.applyMode || 'success',
    lifecycle: options.lifecycle || { maintenance: false, applying: false },
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
      case '/api/v1/session/login': return json(route, { csrfToken: 'synthetic-routing-csrf-new' })
      case '/api/v1/session/logout': return json(route, {})
      case '/api/v1/status': return json(route, statusFor(state.lifecycle))
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/appliance/policy': return json(route, state.policy)
      case '/api/v1/appliance/policy/preview': {
        const rules = entry.body.rules
        const noop = JSON.stringify(rules) === JSON.stringify(state.policy.rules)
        const previewToken = `synthetic-routing-preview-${++state.previewNumber}`
        state.previews.set(previewToken, { rules, noop })
        return json(route, { previewToken, expiresAt: new Date(Date.now() + 300_000).toISOString(), noop, diff: diff(rules, { noop }) })
      }
      case '/api/v1/appliance/policy/cancel':
        state.previews.delete(entry.body.previewToken)
        return json(route, { canceled: true })
      case '/api/v1/appliance/policy/apply': {
        const pending = state.previews.get(entry.body.previewToken)
        state.previews.delete(entry.body.previewToken)
        if (state.applyMode === 'busy') return json(route, { error: 'routing policy is busy', code: 'busy' }, 409)
        if (state.applyMode === 'stale') return json(route, { error: 'routing policy preview is stale', code: 'preview-stale' }, 409)
        if (state.applyMode === 'expired') return json(route, { error: 'routing policy preview has expired', code: 'preview-expired' }, 409)
        if (state.applyMode === 'drift') {
          state.policy = projectionFor('drift-detected')
          return json(route, { error: 'routing policy drift detected', code: 'drift-detected' }, 409)
        }
        if (state.applyMode === 'candidate-rejected') return json(route, { error: 'routing policy candidate rejected', code: 'candidate-rejected' }, 422)
        if (state.applyMode === 'restored') return json(route, { error: 'routing policy failed; previous generation restored', code: 'transaction-restored' }, 500)
        if (state.applyMode === 'unknown') return json(route, { error: 'routing policy outcome is not proven', code: 'transaction-unproven' }, 503)
        if (pending && !pending.noop) state.policy = projectionFor('editable', pending.rules)
        return json(route, { noop: Boolean(pending?.noop), classification: pending?.noop ? 'noop' : 'applied', diff: diff(pending?.rules || [], { noop: Boolean(pending?.noop) }) })
      }
      default: return json(route, { error: `unexpected synthetic route: ${entry.path}` }, 404)
    }
  })
  return state
}

async function openRouting(page) {
  await page.goto('/')
  await expect(page).toHaveTitle('XKeen Control')
  await expect(page.getByRole('button', { name: 'Routing', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Typed custom rules' })).toBeVisible()
}

async function addNamedRule(page, name) {
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill(name)
}

async function signInAfterSessionChange(page) {
  await expect(page.getByLabel('Panel password')).toBeVisible()
  await page.getByLabel('Panel password').fill('synthetic-password')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Routing', exact: true })).toBeVisible()
}

const requestsFor = (state, path, method) => state.requests.filter((request) => request.path === path && (!method || request.method === method))

test.afterEach(async ({ page }) => {
  if (page.__routingIssues) expect(page.__routingIssues).toEqual([])
})

test('adds Routing after Nodes, loads policy lazily, and never adds policy to dashboard polling', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await page.goto('/')
  expect(requestsFor(state, '/api/v1/appliance/policy')).toHaveLength(0)
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Typed custom rules' })).toBeVisible()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy').length).toBe(1)
  const policyReads = requestsFor(state, '/api/v1/appliance/policy').length
  await page.waitForTimeout(5_300)
  expect(requestsFor(state, '/api/v1/appliance/policy')).toHaveLength(policyReads)
  expect(state.requests.findIndex((request) => request.path === '/api/v1/nodes')).toBeLessThan(state.requests.findIndex((request) => request.path === '/api/v1/appliance/policy'))
})

test('serializes only the complete typed rule DTO and exposes keyboard ordering', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await expect(page.getByText('No custom rules. Add a rule')).toBeVisible()
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Proxy work')
  await page.getByLabel('Rule 1 domain expressions').fill('domain:work.example\nfull:portal.example')
  await page.getByLabel('Rule 1 IP and geodata expressions').fill('10.10.0.0/16')
  await page.getByLabel('Rule 1 action').selectOption('proxy')
  await page.getByLabel('Rule 1 protocol tls').check()
  await page.getByLabel('Rule 1 network tcp').check()
  await page.getByRole('button', { name: 'Add port range' }).click()
  await page.getByLabel('Rule 1 port 1 from').fill('443')
  await page.getByLabel('Rule 1 port 1 to').fill('')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Review routing changes' })).toBeVisible()
  const preview = requestsFor(state, '/api/v1/appliance/policy/preview', 'POST')
  expect(preview).toHaveLength(1)
  expect(preview[0].body).toEqual({ rules: [{ name: 'Proxy work', domains: ['domain:work.example', 'full:portal.example'], ips: ['10.10.0.0/16'], protocols: ['tls'], networks: ['tcp'], ports: [{ from: 443, to: 0 }], action: 'proxy' }] })
  expect(JSON.stringify(preview[0].body)).not.toMatch(/ruleTag|outboundTag|inboundTag|balancerTag|generated|protected/i)
  await page.getByRole('button', { name: 'Cancel Preview', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0].body).toEqual({ previewToken: 'synthetic-routing-preview-1' })
})

test('moves rules without sorting and supports Add, Remove, and keyboard Move controls', async ({ page }) => {
  const state = await prepare(page, { rules: [rule('First', 'direct'), rule('Second', 'block')] })
  page.__routingIssues = state.issues
  await openRouting(page)
  const moveDown = page.getByRole('button', { name: 'Move rule down: First' })
  await moveDown.focus()
  await moveDown.press('Enter')
  await expect(page.locator('.routing-rule legend strong').nth(0)).toHaveText('Second')
  await expect(page.locator('.routing-rule legend strong').nth(1)).toHaveText('First')
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await expect(page.getByLabel('Rule 3 display name')).toHaveValue('New routing rule')
  await page.getByRole('button', { name: 'Remove rule: New routing rule' }).click()
  await expect(page.getByLabel('Rule 3 display name')).toHaveCount(0)
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  expect(requestsFor(state, '/api/v1/appliance/policy/preview', 'POST')[0].body.rules.map(({ name }) => name)).toEqual(['Second', 'First'])
})

test('requires explicit discard for dirty Refresh and rebases immediately when clean', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByRole('button', { name: 'Refresh policy', exact: true }).click()
  expect(requestsFor(state, '/api/v1/appliance/policy')).toHaveLength(1)
  await expect(page.getByRole('alert')).toContainText('Unsaved routing changes')
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('New routing rule')
  await page.getByRole('button', { name: 'Refresh policy', exact: true }).click()
  await page.getByRole('button', { name: 'Discard changes and refresh', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy').length).toBe(2)
  await expect(page.getByText('No custom rules. Add a rule')).toBeVisible()
  await page.getByRole('button', { name: 'Refresh policy', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy').length).toBe(3)
})

test('invalidates a completed Preview when dirty changes are discarded and refreshed', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Discarded rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Review routing changes' })).toBeVisible()
  await page.getByRole('button', { name: 'Refresh policy', exact: true }).click()
  await page.getByRole('button', { name: 'Discard changes and refresh', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy').length).toBe(2)
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0].body).toEqual({ previewToken: 'synthetic-routing-preview-1' })
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(0)
})

test('invalidates an in-flight Preview when dirty changes are discarded and refreshed', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  const release = deferred()
  state.handle = deferredPreviewHandler(release)
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('In-flight discarded rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.locator('.notice.neutral')).toContainText('Preparing a fresh semantic Preview…')
  await page.getByRole('button', { name: 'Refresh policy', exact: true }).click()
  await page.getByRole('button', { name: 'Discard changes and refresh', exact: true }).click()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy').length).toBe(2)
  release.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  await expect(page.locator('.notice.neutral')).toHaveCount(0)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0].body).toEqual({ previewToken: 'synthetic-routing-preview-1' })
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(0)
})

test('invalidates a completed Preview when the session changes', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await addNamedRule(page, 'Session discarded rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Review routing changes' })).toBeVisible()
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page.getByLabel('Panel password')).toBeVisible()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0]).toMatchObject({
    body: { previewToken: 'synthetic-routing-preview-1' },
    csrf: csrfToken,
  })
  await signInAfterSessionChange(page)
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(0)
})

test('invalidates an in-flight Preview when the session changes and cancels its late token', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  const release = deferred()
  state.handle = deferredPreviewHandler(release)
  await openRouting(page)
  await addNamedRule(page, 'Late session rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.locator('.notice.neutral')).toContainText('Preparing a fresh semantic Preview…')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await expect(page.getByLabel('Panel password')).toBeVisible()
  release.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0]).toMatchObject({
    body: { previewToken: 'synthetic-routing-preview-1' },
    csrf: csrfToken,
  })
  await signInAfterSessionChange(page)
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(0)
})

test('invalidates a completed Preview when navigating away from Routing', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await addNamedRule(page, 'Navigation discarded rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Review routing changes' })).toBeVisible()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.locator('.components-heading')).toBeVisible()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0].body).toEqual({ previewToken: 'synthetic-routing-preview-1' })
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Typed custom rules' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(0)
})

test('invalidates an in-flight Preview when navigating away from Routing and cancels its late token', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  const release = deferred()
  state.handle = deferredPreviewHandler(release)
  await openRouting(page)
  await addNamedRule(page, 'Late navigation rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.locator('.notice.neutral')).toContainText('Preparing a fresh semantic Preview…')
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.locator('.components-heading')).toBeVisible()
  release.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST').length).toBe(1)
  expect(requestsFor(state, '/api/v1/appliance/policy/cancel', 'POST')[0].body).toEqual({ previewToken: 'synthetic-routing-preview-1' })
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Typed custom rules' })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(0)
})

test('keeps the name editor focused during sequential keyboard editing', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  const input = page.getByLabel('Rule 1 display name')
  await input.click()
  await input.selectText()
  for (const character of 'Keyboard name') {
    await input.press(character === ' ' ? 'Space' : character)
    await expect(input).toBeFocused()
  }
  await expect(input).toHaveValue('Keyboard name')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  expect(requestsFor(state, '/api/v1/appliance/policy/preview', 'POST')[0].body.rules[0].name).toBe('Keyboard name')
})

for (const editability of ['drift-detected', 'unavailable']) {
  test(`fails closed for ${editability} projections`, async ({ page }) => {
    const state = await prepare(page, { editability })
    page.__routingIssues = state.issues
    await openRouting(page)
    await expect(page.getByRole('alert')).toContainText(editability === 'drift-detected' ? 'drift' : 'unavailable')
    await expect(page.getByRole('button', { name: 'Add rule', exact: true })).toHaveCount(0)
    expect(requestsFor(state, '/api/v1/appliance/policy/preview', 'POST')).toHaveLength(0)
  })
}

test('renders semantic Preview facts and makes no-op Preview non-applicable', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Proxy work')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByText('Added', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' }).getByText('Proxy work', { exact: true })).toBeVisible()
  await expect(page.getByText('Rules before → after', { exact: true })).toBeVisible()
  await expect(page.getByText('Runtime restart', { exact: true })).toBeVisible()
  await expect(page.getByText('Required', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel Preview', exact: true }).click()

  await page.getByRole('button', { name: 'Remove rule: Proxy work', exact: true }).click()
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'No effective changes' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply routing changes', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Cancel Preview', exact: true }).click()
})

test('applies exactly once with a token-only body and refreshes/rebases the projection', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Applied rule')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  const apply = page.getByRole('button', { name: 'Apply routing changes', exact: true })
  await apply.evaluate((element) => { element.click(); element.click() })
  await expect(page.getByTestId('routing-result')).toContainText('Routing changes applied')
  const applyRequests = requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')
  expect(applyRequests).toHaveLength(1)
  expect(applyRequests[0].body).toEqual({ previewToken: 'synthetic-routing-preview-1' })
  expect(Object.keys(applyRequests[0].body)).toEqual(['previewToken'])
  expect(requestsFor(state, '/api/v1/appliance/policy')).toHaveLength(2)
  expect(await page.getByLabel('Rule 1 display name').inputValue()).toBe('Applied rule')
})

test('keeps a delayed Apply alive across navigation and exposes a global notice', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  const release = deferred()
  state.handle = async ({ route, entry, state: current }) => {
    if (entry.path !== '/api/v1/appliance/policy/apply') return false
    await release.promise
    const pending = current.previews.get(entry.body.previewToken)
    if (pending && !pending.noop) current.policy = projectionFor('editable', pending.rules)
    await json(route, { noop: false, classification: 'applied', diff: diff(pending?.rules || []) })
    return true
  }
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await page.getByRole('button', { name: 'Apply routing changes', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Components / Updates' })).toBeVisible()
  await page.getByRole('button', { name: 'Components / Updates' }).click()
  await expect(page.getByText('Routing Apply is running', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Open Routing', exact: true })).toBeVisible()
  release.resolve()
  await expect.poll(() => requestsFor(state, '/api/v1/appliance/policy/apply', 'POST').length).toBe(1)
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByTestId('routing-result')).toContainText('Routing changes applied')
})

test('treats Apply transport loss as unknown and never replays the consumed token', async ({ page }) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  state.handle = async ({ route, entry }) => {
    if (entry.path !== '/api/v1/appliance/policy/apply') return false
    await route.abort('failed')
    return true
  }
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await page.getByRole('button', { name: 'Apply routing changes', exact: true }).click()
  await expect(page.getByTestId('routing-result')).toContainText('Routing outcome is unknown')
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(1)
  await page.waitForTimeout(200)
  expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(1)
  await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeEnabled()
})

const applyErrorCases = [
  { mode: 'busy', code: 'busy', title: 'Lifecycle is busy', refreshes: false, draft: 'retained' },
  { mode: 'stale', code: 'preview-stale', title: 'Preview is stale', refreshes: true, draft: 'rebased' },
  { mode: 'expired', code: 'preview-expired', title: 'Preview expired', refreshes: false, draft: 'retained' },
  { mode: 'drift', code: 'drift-detected', title: 'Routing policy drift detected', refreshes: true, draft: 'blocked' },
  { mode: 'candidate-rejected', code: 'candidate-rejected', title: 'Routing candidate rejected', refreshes: false, draft: 'retained' },
  { mode: 'restored', code: 'transaction-restored', title: 'Routing changes were restored', refreshes: true, draft: 'rebased' },
  { mode: 'unknown', code: 'transaction-unproven', title: 'Routing outcome is unknown', refreshes: true, draft: 'retained' },
]

for (const outcome of applyErrorCases) {
  test(`maps Apply ${outcome.code} conservatively without replay`, async ({ page }) => {
    const state = await prepare(page, { applyMode: outcome.mode })
    page.__routingIssues = state.issues
    await openRouting(page)
    await addNamedRule(page, `Matrix ${outcome.code}`)
    await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
    await page.getByRole('button', { name: 'Apply routing changes', exact: true }).click()
    await expect(page.getByTestId('routing-result')).toContainText(outcome.title)
    expect(requestsFor(state, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(1)
    expect(state.previews.has('synthetic-routing-preview-1')).toBe(false)
    expect(requestsFor(state, '/api/v1/appliance/policy')).toHaveLength(outcome.refreshes ? 2 : 1)
    await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)

    if (outcome.draft === 'retained') {
      await expect(page.getByLabel('Rule 1 display name')).toHaveValue(`Matrix ${outcome.code}`)
      await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeEnabled()
    } else if (outcome.draft === 'blocked') {
      await expect(page.getByText('Routing editing is blocked by protected-state drift.', { exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: 'Add rule', exact: true })).toHaveCount(0)
      await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toHaveCount(0)
    } else {
      await expect(page.getByLabel('Rule 1 display name')).toHaveCount(0)
      await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeEnabled()
    }
  })
}

test('lifecycle maintenance disables new mutation initiation while keeping read-only facts', async ({ page }) => {
  const state = await prepare(page, { lifecycle: { maintenance: true, applying: false } })
  page.__routingIssues = state.issues
  await openRouting(page)
  await expect(page.getByRole('button', { name: 'Add rule', exact: true })).toBeDisabled()
  await expect(page.getByText('Policy boundary', { exact: true })).toBeVisible()
  expect(requestsFor(state, '/api/v1/appliance/policy/preview', 'POST')).toHaveLength(0)
})

test('keeps preview and draft state out of browser storage and remains usable on mobile', async ({ page }, testInfo) => {
  const state = await prepare(page)
  page.__routingIssues = state.issues
  await openRouting(page)
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Mobile rule')
  expect(await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))).toEqual({ local: {}, session: {} })
  await page.screenshot({ path: testInfo.outputPath('routing-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: 'Typed custom rules' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('routing-mobile.png'), fullPage: true })
  expect(state.issues).toEqual([])
})
