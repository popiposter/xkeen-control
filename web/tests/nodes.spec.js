import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-nodes-csrf'
const origin = 'http://127.0.0.1:4173'

const nodeID = (index) => `node-${String(index).padStart(8, '0')}`

const makeNodes = () => Array.from({ length: 51 }, (_, offset) => {
  const index = offset + 1
  const subscription = index === 3
  return {
    id: nodeID(index),
    name: `Node ${String(index).padStart(3, '0')}`,
    displayName: `Node ${String(index).padStart(3, '0')}`,
    address: `edge-${index}.example.com:443`,
    countryCode: index % 2 ? 'DE' : 'SE',
    outboundTag: `proxy-${nodeID(index)}`,
    enabled: index !== 2,
    sourceType: subscription ? 'subscription' : 'manual',
    subscriptionName: subscription ? 'Provider' : '',
    alive: index !== 4,
    latencyMs: index,
    lastError: '',
    lastThroughputKBps: 0,
    lastBenchmarkAt: '',
    isNativeSelected: index === 1,
    isOverride: index === 2,
    isEffective: index === 1,
    stale: false,
    missing: false,
  }
})

const statusFixture = (nodes) => ({
  controlPlane: { version: 'dev', uptimeSeconds: 120 },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  balancer: { nativeSelected: nodes[0].outboundTag, effective: nodes[0].outboundTag },
  observatory: { healthy: 50, total: 51, apiReachable: true },
  benchmark: { controlPlane: { running: false, state: 'idle' } },
  selection: { state: 'stable', manualOverride: nodes[1].outboundTag },
  setup: { runtime: 'running', credential: 'ready', xkeen: 'ready', xray: 'ready', configuration: 'ready' },
  lifecycle: { maintenance: false, applying: false },
})

const json = (route, body, status = 200) => route.fulfill({
  status,
  contentType: 'application/json',
  body: JSON.stringify(body),
})

async function prepare(page) {
  const state = {
    nodes: makeNodes(),
    requests: [],
    pending: new Map(),
    previewNumber: 0,
    missingNextRefresh: false,
    exactRefreshRemovalIDs: [nodeID(3)],
  }
  state.status = statusFixture(state.nodes)
  const issues = []
  page.on('pageerror', (error) => issues.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (['error', 'warning'].includes(message.type()) && !message.text().startsWith('Failed to load resource:')) {
      issues.push(`${message.type()}: ${message.text()}`)
    }
  })
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== origin) return route.abort('blockedbyclient')
    let body = null
    if (request.postData()) {
      try { body = request.postDataJSON() } catch { body = request.postData() }
    }
    const path = url.pathname
    state.requests.push({ path, method: request.method(), body })

    switch (path) {
      case '/api/v1/session': return json(route, { csrfToken })
      case '/api/v1/status': return json(route, state.status)
      case '/api/v1/nodes': {
        const nodes = state.missingNextRefresh ? state.nodes.filter((node) => node.id !== nodeID(1)) : state.nodes
        state.missingNextRefresh = false
        return json(route, { total: nodes.length, nodes, subscriptions: [{ id: 'sub-12345678', name: 'Provider', enabled: true, nodeCount: nodes.filter((node) => node.sourceType === 'subscription').length, staleCount: 0, autoRefresh: { state: 'deferred', nextRunAt: new Date(Date.now() + 300000).toISOString(), lastSuccessAt: new Date(Date.now() - 60000).toISOString(), lastResult: 'noop', errorCode: 'authority-busy' } }] })
      }
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/selection/override': {
        state.status.selection.manualOverride = body.target
        for (const node of state.nodes) node.isOverride = Boolean(body.target) && node.outboundTag === body.target
        return json(route, { manualOverride: body.target })
      }
      case '/api/v1/nodes/batch/state/preview':
      case '/api/v1/nodes/batch/remove/preview': {
        const remove = path.endsWith('/remove/preview')
        const selected = state.nodes.filter((node) => body.nodeIds.includes(node.id))
        const operation = remove ? 'batch-remove' : (body.enabled ? 'batch-enable' : 'batch-disable')
        const changes = selected
          .filter((node) => remove || node.enabled !== body.enabled)
          .map((node) => ({
            action: operation,
            id: node.id,
            name: node.name,
            outboundTag: node.outboundTag,
            sourceType: node.sourceType,
            before: node.enabled ? 'enabled' : 'disabled',
            after: remove ? 'removed' : (body.enabled ? 'enabled' : 'disabled'),
          }))
        const previewToken = `synthetic-batch-${++state.previewNumber}`
        state.pending.set(previewToken, { remove, ids: [...body.nodeIds], enabled: body.enabled })
        return json(route, { previewToken, operation, expiresAt: new Date(Date.now() + 300_000).toISOString(), changes, requiresAcceptance: false, noop: changes.length === 0 })
      }
      case '/api/v1/subscriptions/refresh/preview': {
        const selected = state.nodes.filter((node) => state.exactRefreshRemovalIDs.includes(node.id))
        const changes = selected.map((node) => ({
          action: 'subscription-refresh',
          id: node.id,
          name: node.name,
          outboundTag: node.outboundTag,
          sourceType: node.sourceType,
          before: node.enabled ? 'enabled' : 'disabled',
          after: 'removed',
        }))
        const previewToken = `synthetic-subscription-${++state.previewNumber}`
        state.pending.set(previewToken, { refresh: true, ids: [...state.exactRefreshRemovalIDs] })
        return json(route, { previewToken, operation: 'subscription-refresh', expiresAt: new Date(Date.now() + 300_000).toISOString(), changes, requiresAcceptance: false, noop: changes.length === 0 })
      }
      case '/api/v1/nodes/replace/preview': {
        const previewToken = `synthetic-replace-${++state.previewNumber}`
        return json(route, { previewToken, operation: 'replace', expiresAt: new Date(Date.now() + 300_000).toISOString(), changes: [{ action: 'replace', id: body.id, name: 'Node 001', outboundTag: `proxy-${body.id}`, sourceType: 'manual', before: 'enabled', after: 'enabled' }], requiresAcceptance: false, noop: false })
      }
      case '/api/v1/node-changes/apply': {
        const pending = state.pending.get(body.previewToken)
        if (pending) {
          if (pending.remove) state.nodes = state.nodes.filter((node) => !pending.ids.includes(node.id))
          else if (pending.refresh) state.nodes = state.nodes.filter((node) => !pending.ids.includes(node.id))
          else state.nodes = state.nodes.map((node) => pending.ids.includes(node.id) ? { ...node, enabled: pending.enabled } : node)
          state.pending.delete(body.previewToken)
        }
        return json(route, { operation: pending?.refresh ? 'subscription-refresh' : pending?.remove ? 'batch-remove' : 'batch-state', nodes: state.nodes, changes: [] })
      }
      case '/api/v1/node-changes/cancel': return json(route, { canceled: true })
      default: return json(route, { error: `unexpected synthetic route: ${path}` }, 404)
    }
  })
  return { state, issues }
}

async function openNodes(page) {
  await page.goto('/')
  await expect(page).toHaveTitle('XKeen Control')
  await page.getByRole('button', { name: /^Nodes/ }).click()
  await expect(page.getByRole('heading', { name: /^Nodes/ })).toBeVisible()
}

test.afterEach(async ({ page }) => {
  const issues = page.__nodesIssues
  if (issues) expect(issues).toEqual([])
})

test('shows bounded automatic subscription status without scheduler controls', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  const status = page.getByTestId('subscription-auto-refresh-sub-12345678')
  await expect(status).toContainText('Refresh deferred')
  await expect(status).toContainText('Authority busy')
  await expect(status).toContainText('Last: No change')
  await expect(status).toContainText('Next:')
  await expect(page.getByText(/cadence/i)).toHaveCount(0)
})

test('selects one, many and all filtered nodes across pages and reconciles selection', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  await page.getByLabel('Select Node 001').check()
  await expect(page.getByLabel('Select all filtered nodes')).toHaveJSProperty('indeterminate', true)
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByLabel('Select Node 026')).toBeVisible()
  await page.getByLabel('Select Node 026').check()
  await expect(page.getByTestId('selected-count')).toHaveText('2 selected')

  await page.getByRole('button', { name: 'Select all 51 filtered' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('51 selected')
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByLabel('Select Node 051')).toBeVisible()
  await expect(page.getByTestId('selected-count')).toHaveText('51 selected')
  await page.locator('.sort-button').filter({ hasText: 'Name' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('51 selected')

  await page.getByLabel('Search nodes').fill('Node 026')
  await expect(page.getByTestId('selected-count')).toHaveText('1 selected')
  await page.getByLabel('Search nodes').fill('no-match')
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')

  await page.getByRole('button', { name: 'Clear', exact: true }).click()
  await page.getByLabel('Search nodes').fill('Node 001')
  await page.getByLabel('Select Node 001').check()
  prepared.state.missingNextRefresh = true
  await page.getByRole('button', { name: 'Refresh dashboard' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  await expect(page.getByLabel('Select Node 001')).toHaveCount(0)
  await expect(page.locator('tbody .row-actions')).toHaveCount(0)
  await expect(page.locator('tbody tr button')).toHaveCount(0)
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
})

test('gates toolbar actions and sends one exact batch state preview', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  const button = (name) => page.getByRole('button', { name, exact: true })
  await expect(button('Edit / replace profile')).toBeDisabled()
  await expect(button('Enable')).toBeDisabled()
  await expect(button('Disable')).toBeDisabled()
  await expect(button('Delete')).toBeDisabled()

  await page.getByLabel('Select Node 001').check()
  await expect(button('Enable')).toBeDisabled()
  await expect(button('Disable')).toBeEnabled()
  await page.getByLabel('Select Node 002').check()
  await expect(button('Enable')).toBeEnabled()
  await expect(button('Disable')).toBeEnabled()

  await button('Enable').click()
  await expect(page.getByRole('dialog', { name: 'Preview node change' })).toBeVisible()
  const previews = prepared.state.requests.filter((request) => request.path === '/api/v1/nodes/batch/state/preview')
  expect(previews).toHaveLength(1)
  expect(previews[0].body).toEqual({ nodeIds: [nodeID(1), nodeID(2)], enabled: true })
  expect(prepared.state.requests.filter((request) => /^\/api\/v1\/nodes\/node-[^/]+\/state\/preview$/.test(request.path))).toHaveLength(0)
  await expect(page.getByRole('dialog')).toContainText('1 node changes')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
})

test('sends one batch remove preview, renders warnings, and reconciles after Apply', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)
  await page.getByLabel('Select Node 001').check()
  await page.getByLabel('Select Node 002').check()
  await page.getByLabel('Select Node 003').check()

  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Preview node change' })).toBeVisible()
  const previews = prepared.state.requests.filter((request) => request.path === '/api/v1/nodes/batch/remove/preview')
  expect(previews).toHaveLength(1)
  expect(previews[0].body).toEqual({ nodeIds: [nodeID(1), nodeID(2), nodeID(3)] })
  await expect(page.getByRole('dialog')).toContainText('The currently effective node changes in this preview.')
  await expect(page.getByRole('dialog')).toContainText('The current manual-override node changes in this preview.')
  await expect(page.getByRole('dialog')).toContainText('may return on a later subscription refresh')
  await expect(page.locator('.diff-row')).toHaveCount(3)

  await page.getByRole('button', { name: 'Apply and validate' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/node-changes/apply')).toHaveLength(1)
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/nodes/batch/remove/preview')).toHaveLength(1)
  expect(prepared.state.nodes.some((node) => [nodeID(1), nodeID(2), nodeID(3)].includes(node.id))).toBe(false)
})

test('renders exact provider removals without stale or manual-reappearance warnings', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await page.getByRole('button', { name: 'Refresh Provider', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Preview node change' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText('Provider snapshot removes 1 node that is no longer present upstream.')
  await expect(dialog).not.toContainText('keeps them stale/missing')
  await expect(dialog).not.toContainText('may return on a later subscription refresh')
  await expect(page.locator('.diff-row')).toHaveCount(1)
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/subscriptions/refresh/preview')).toEqual([
    { path: '/api/v1/subscriptions/refresh/preview', method: 'POST', body: { subscriptionId: 'sub-12345678' } },
  ])
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
  await expect(page.locator('body')).not.toContainText('subscription.example')

  await page.getByRole('button', { name: 'Apply and validate' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  await expect(page.getByLabel('Select Node 003')).toHaveCount(0)
  const applies = prepared.state.requests.filter((request) => request.path === '/api/v1/node-changes/apply')
  expect(applies).toHaveLength(1)
  expect(applies[0].body).toEqual({ previewToken: 'synthetic-subscription-1', acceptMissing: false })
})

test('keeps effective and manual impact warnings for exact provider removals', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  for (const index of [0, 1]) {
    prepared.state.nodes[index] = { ...prepared.state.nodes[index], sourceType: 'subscription', subscriptionName: 'Provider' }
  }
  prepared.state.exactRefreshRemovalIDs = [nodeID(1), nodeID(2), nodeID(3)]
  await openNodes(page)

  await page.getByRole('button', { name: 'Refresh Provider', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Preview node change' })
  await expect(dialog).toContainText('Provider snapshot removes 3 nodes that are no longer present upstream.')
  await expect(dialog).toContainText('The currently effective node changes in this preview.')
  await expect(dialog).toContainText('The current manual-override node changes in this preview.')
  await expect(dialog).not.toContainText('may return on a later subscription refresh')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
})

test('keeps manual override and replacement as single-selection toolbar actions without exposing profile secrets', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await page.getByLabel('Select Node 002').check()
  await page.getByRole('button', { name: 'Clear manual override', exact: true }).click()
  const overrideRequests = prepared.state.requests.filter((request) => request.path === '/api/v1/selection/override')
  expect(overrideRequests).toHaveLength(1)
  expect(overrideRequests[0].body).toEqual({ target: '' })

  await page.getByRole('button', { name: 'Clear selection', exact: true }).click()
  await page.getByLabel('Select Node 001').check()
  await page.getByRole('button', { name: 'Set manual override', exact: true }).click()
  await expect.poll(() => prepared.state.requests.filter((request) => request.path === '/api/v1/selection/override').length).toBe(2)
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/selection/override')[1].body).toEqual({ target: `proxy-${nodeID(1)}` })

  await page.getByRole('button', { name: 'Edit / replace profile', exact: true }).click()
  const profile = ['vless:', '//', '11111111-1111-4111-8111-111111111111@secret.example.com:443?security=reality&sni=front.example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&type=tcp#Synthetic'].join('')
  await page.getByLabel('Replacement VLESS profile').fill(profile)
  await page.getByRole('button', { name: 'Preview replacement', exact: true }).click()
  const replacements = prepared.state.requests.filter((request) => request.path === '/api/v1/nodes/replace/preview')
  expect(replacements).toHaveLength(1)
  expect(replacements[0].body).toEqual({ id: nodeID(1), profile })
  await expect(page.locator('body')).not.toContainText('11111111-1111-4111-8111-111111111111')
  await expect(page.locator('body')).not.toContainText('secret.example.com')
})
