import { revealDetails, revealSystemSettings } from './fixtures/disclosures.js'
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
  balancer: { nativeSelected: nodes[0].outboundTag, effective: nodes[0].outboundTag, override: nodes[1].outboundTag },
  observatory: { healthy: 50, total: 51, apiReachable: true },
  benchmark: { controlPlane: { running: false, state: 'idle' } },
  selection: { state: 'stable', manualOverride: nodes[1].outboundTag },
  native: { installation: 'available', panelIntegration: 'available', version: '2.0.1', channel: 'beta', core: 'xray', xrayRunning: true },
  lifecycle: { maintenance: false, applying: false },
})

const json = (route, body, status = 200) => route.fulfill({
  status,
  contentType: 'application/json',
  body: JSON.stringify(body),
})

test('node confirmation contains keyboard focus and restores it on Escape', async ({ page }) => {
  await prepare(page)
  await openNodes(page)
  await page.evaluate(() => {
    window.modalCSPViolations = []
    document.addEventListener('securitypolicyviolation', (event) => window.modalCSPViolations.push(event.violatedDirective))
    const policy = document.createElement('meta')
    policy.httpEquiv = 'Content-Security-Policy'
    policy.content = "script-src 'self'; style-src 'self'"
    document.head.append(policy)
  })
  const trigger = page.getByRole('button', { name: 'Refresh Provider', exact: true })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: 'Preview node change' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Close preview' })).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(dialog.getByRole('button', { name: 'Apply and validate' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Close preview' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).not.toBeVisible()
  await expect(trigger).toBeFocused()
  expect(await page.evaluate(() => window.modalCSPViolations)).toEqual([])
})

for (const body of ['not json', '{}', '{"unrelated":true}']) {
  test(`malformed Apply reply consumes preview: ${body}`, async ({ page }) => {
    await prepare(page)
    await openNodes(page)
    let calls = 0
    await page.route('**/api/v1/node-changes/apply', async (route) => {
      calls++
      await route.fulfill({ status: 200, contentType: 'application/json', body })
    })
    await page.getByRole('button', { name: 'Refresh Provider', exact: true }).click()
    await page.getByRole('button', { name: 'Apply and validate', exact: true }).click()
    await expect(page.getByText('The change outcome could not be confirmed.', { exact: false })).toBeVisible()
    await expect(page.getByRole('dialog', { name: 'Preview node change' })).toHaveCount(0)
    await expect(page.getByText('Change applied;', { exact: false })).toHaveCount(0)
    expect(calls).toBe(1)
  })
}

test('late logout response cannot clear a newly established session', async ({ page }) => {
  await page.clock.install()
  const prepared = await prepare(page)
  await openNodes(page)
  let release, finished
  const pending = new Promise((resolve) => { release = resolve })
  const done = new Promise((resolve) => { finished = resolve })
  await page.route('**/api/v1/session/logout', async (route) => {
    await pending
    await json(route, { loggedOut: true })
    finished()
  })
  let expired = true
  await page.route('**/api/v1/status', (route) => json(route, expired ? { error: 'unauthorized' } : prepared.state.status, expired ? 401 : 200))
  await page.getByRole('button', { name: 'Sign out' }).click()
  await page.clock.fastForward(5000)
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible()
  expired = false
  await page.route('**/api/v1/session/login', (route) => json(route, { csrfToken: 'synthetic-new-session' }))
  await page.getByLabel('Panel password', { exact: true }).fill('synthetic-test-password')
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  release()
  await done
  await page.clock.fastForward(5000)
  await expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toHaveCount(0)
})

test('Base UI confirmation stays open during an outstanding Apply', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)
  await page.getByRole('button', { name: 'Refresh Provider', exact: true }).click()
  let release
  const pending = new Promise((resolve) => { release = resolve })
  await page.route('**/api/v1/node-changes/apply', async (route) => {
    await pending
    await json(route, { operation: 'subscription-refresh', nodes: [], changes: [] })
  })
  const dialog = page.getByRole('dialog', { name: 'Preview node change' })
  await dialog.getByRole('button', { name: 'Apply and validate' }).click()
  await expect(dialog.getByRole('button', { name: 'Applying…' })).toBeDisabled()
  await page.keyboard.press('Escape')
  await page.mouse.click(3, 3)
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled()
  release()
  await expect(dialog).not.toBeVisible()
})

async function prepare(page) {
  const state = {
    nodes: makeNodes(),
    requests: [],
    pending: new Map(),
    previewNumber: 0,
    subscriptionEnabled: true,
    missingNextRefresh: false,
    exactRefreshRemovalIDs: [nodeID(3)],
    manualPolls: 0,
    manual: { mode: 'manual-node', state: 'idle', phase: 'done', plannedStages: 11, completedStages: 0, bytesPlanned: 48 * 1024 * 1024, bytesTransferred: 0 },
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
      case '/api/v1/performance/quality': return json(route, { state: 'idle', progress: { candidates: [] } })
      case '/api/v1/nodes': {
        const nodes = state.missingNextRefresh ? state.nodes.filter((node) => node.id !== nodeID(1)) : state.nodes
        state.missingNextRefresh = false
        const autoRefresh = state.subscriptionEnabled
          ? { state: 'deferred', nextRunAt: new Date(Date.now() + 300000).toISOString(), lastSuccessAt: new Date(Date.now() - 60000).toISOString(), lastResult: 'noop', errorCode: 'authority-busy' }
          : { state: 'disabled' }
        return json(route, { total: nodes.length, nodes, subscriptions: [{ id: 'sub-12345678', name: 'Provider', enabled: state.subscriptionEnabled, nodeCount: nodes.filter((node) => node.sourceType === 'subscription').length, staleCount: 0, autoRefresh }] })
      }
      case '/api/v1/performance': {
        if (state.manual.state === 'running' && state.manualPolls > 0) {
          state.manual = { ...state.manual, state: 'completed', phase: 'done', completedStages: 11, bytesTransferred: 48 * 1024 * 1024, latencyMs: 42, downloadBps: 1250000, uploadBps: 625000 }
        }
        if (state.manual.state === 'running') state.manualPolls++
        return json(route, { nodes: [], manual: state.manual })
      }
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/performance/manual-node': {
        state.manualPolls = 0
        state.manual = { mode: 'manual-node', state: 'running', phase: 'latency', targetNodeId: body.nodeId, targetTag: `proxy-${body.nodeId}`, startedAt: new Date().toISOString(), elapsedMs: 0, plannedStages: 11, completedStages: 0, bytesPlanned: 48 * 1024 * 1024, bytesTransferred: 0 }
        return json(route, { accepted: true, state: 'accepted' }, 202)
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
        state.pending.set(previewToken, { operation, remove, ids: [...body.nodeIds], enabled: body.enabled })
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
      case '/api/v1/subscriptions/state/preview': {
        const previewToken = `synthetic-subscription-state-${++state.previewNumber}`
        state.pending.set(previewToken, { subscriptionState: true, enabled: body.enabled })
        return json(route, { previewToken, operation: 'subscription-enable-disable', expiresAt: new Date(Date.now() + 300_000).toISOString(), changes: [{ action: 'subscription-enable-disable', id: 'sub-12345678', name: 'Provider', outboundTag: '', sourceType: 'subscription', before: state.subscriptionEnabled ? 'enabled' : 'disabled', after: body.enabled ? 'enabled' : 'disabled' }], requiresAcceptance: false, noop: state.subscriptionEnabled === body.enabled })
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
          else if (pending.subscriptionState) state.subscriptionEnabled = pending.enabled
          else state.nodes = state.nodes.map((node) => pending.ids.includes(node.id) ? { ...node, enabled: pending.enabled } : node)
          state.pending.delete(body.previewToken)
        }
        return json(route, { operation: pending?.refresh ? 'subscription-refresh' : pending?.subscriptionState ? 'subscription-enable-disable' : pending?.operation, nodes: state.nodes, changes: [] })
      }
      case '/api/v1/node-changes/cancel': return json(route, { canceled: true })
      default: return json(route, { error: `unexpected synthetic route: ${path}` }, 404)
    }
  })
  return { state, issues }
}

test('quality columns, hidden source and disabled action hints are usable', async ({ page }) => {
  const prepared=await prepare(page); page.__nodesIssues=prepared.issues
  await page.route('**/api/v1/performance/quality',(route)=>json(route,{state:'completed',canStage:true,ranking:[{tag:prepared.state.nodes[0].outboundTag,rank:1,cost:1}],progress:{candidates:[{tag:prepared.state.nodes[0].outboundTag,valid:true,downloadBps:10e6,uploadBps:2e6}]}}))
  await openNodes(page)
  await expect(page.getByRole('columnheader',{name:'Source',exact:true})).toHaveCount(0)
  await expect(page.getByRole('columnheader',{name:'Quality rank',exact:true})).toBeVisible()
  await expect(page.getByRole('cell',{name:'80.0 Mbps',exact:true})).toBeVisible()
  await page.getByRole('button',{name:'Columns',exact:true}).click()
  await page.getByRole('menuitemcheckbox',{name:'Source',exact:true}).click()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('columnheader',{name:'Source',exact:true})).toBeVisible()
  const pin=page.getByRole('button',{name:'Set manual override',exact:true})
  await expect(pin).toBeDisabled()
  await pin.locator('..').hover()
  await expect(page.locator('[data-slot=tooltip-content]')).toBeVisible()
  await expect(page.locator('[data-slot=tooltip-content]')).toContainText('Select one enabled node first')
  await page.mouse.move(0, 0)
  await pin.locator('..').focus()
  await expect(page.locator('[data-slot=tooltip-content]')).toBeVisible()
  await page.getByRole('checkbox',{name:'Select Node 001',exact:true}).check()
  await expect(pin).toBeEnabled()
})

async function openNodes(page) {
  await page.goto('/')
  await expect(page).toHaveTitle('XKeen Control')
  await page.getByRole('button', { name: /^Nodes/ }).click()
  await expect(page.getByRole('heading', { name: /^Nodes/ })).toBeVisible()
  if (await page.locator('.subscriptions-disclosure').count()) await revealDetails(page, 'Subscriptions')
}

test.afterEach(async ({ page }) => {
  const issues = page.__nodesIssues
  if (issues) expect(issues).toEqual([])
})

test('renders and filters RU/BY country projections without changing enabled state', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  prepared.state.nodes[0] = { ...prepared.state.nodes[0], countryCode: 'RU', enabled: false, alive: false, displayName: 'Russia fixture' }
  prepared.state.nodes[1] = { ...prepared.state.nodes[1], countryCode: 'BY', enabled: false, alive: false, displayName: 'Belarus fixture' }
  for (const [offset, name] of ['Hosted by Provider', 'Powered by Example'].entries()) {
    prepared.state.nodes[offset + 2] = { ...prepared.state.nodes[offset + 2], countryCode: '', enabled: true, alive: true, displayName: name }
  }
  await openNodes(page)
  for (const [code, name] of [['RU', 'Russia fixture'], ['BY', 'Belarus fixture']]) {
    await page.getByLabel('Filter by country').selectOption(code)
    const row = page.locator('.nodes-table tbody tr').filter({ hasText: name })
    await expect(row).toContainText('Disabled')
    await expect(row.locator('.country-flag')).toBeVisible()
    await expect(page.locator('.nodes-table tbody tr')).toHaveCount(1)
  }
  await page.getByLabel('Filter by country').selectOption('all')
  for (const name of ['Hosted by Provider', 'Powered by Example']) {
    await page.getByLabel('Search nodes').fill(name)
    const row = page.locator('.nodes-table tbody tr').filter({ hasText: name })
    await expect(row).toContainText('Alive')
    await expect(row.locator('.country-flag')).toHaveCount(0)
  }
  expect(prepared.state.requests.filter((request) => request.method === 'POST')).toEqual([])
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

test('projects disabled subscription as non-participating before scheduler rescan', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await page.getByRole('button', { name: 'Disable Provider', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Preview node change' })).toBeVisible()
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/subscriptions/state/preview')).toEqual([
    { path: '/api/v1/subscriptions/state/preview', method: 'POST', body: { subscriptionId: 'sub-12345678', enabled: false } },
  ])
  await page.getByRole('button', { name: 'Apply and validate', exact: true }).click()

  const status = page.getByTestId('subscription-auto-refresh-sub-12345678')
  await expect(status).toContainText('Automatic refresh disabled')
  await expect(status).toContainText('Disabled subscriptions do not participate')
  await expect(status).not.toContainText('Next:')
  await expect(status).not.toContainText('Refresh deferred')
  await expect(page.getByTestId('subscription-card-sub-12345678')).toHaveAttribute('data-enabled', 'false')
})

test('selects one, many and all filtered nodes across pages and reconciles selection', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  await expect(page.getByRole('checkbox', { name: 'Select all filtered nodes', exact: true })).toHaveAttribute('aria-checked', 'mixed')
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('checkbox', { name: 'Select Node 026', exact: true })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Select Node 026', exact: true }).check()
  await expect(page.getByTestId('selected-count')).toHaveText('2 selected')

  await page.getByRole('button', { name: 'Select all 51 filtered' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('51 selected')
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('checkbox', { name: 'Select Node 051', exact: true })).toBeVisible()
  await expect(page.getByTestId('selected-count')).toHaveText('51 selected')
  await page.locator('.sort-button').filter({ hasText: 'Name' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('51 selected')

  await page.getByLabel('Search nodes').fill('Node 026')
  await expect(page.getByTestId('selected-count')).toHaveText('1 selected')
  await page.getByLabel('Search nodes').fill('no-match')
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')

  await page.getByRole('button', { name: 'Clear', exact: true }).click()
  await page.getByLabel('Search nodes').fill('Node 001')
  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  prepared.state.missingNextRefresh = true
  await page.getByRole('button', { name: 'Refresh dashboard' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  await expect(page.getByRole('checkbox', { name: 'Select Node 001', exact: true })).toHaveCount(0)
  await expect(page.locator('tbody .row-actions')).toHaveCount(0)
  await expect(page.locator('tbody tr button')).toHaveCount(0)
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
})

test('gates toolbar actions and sends one exact batch state preview', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  const button = (name) => page.getByRole('button', { name, exact: true })
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toBeVisible()

  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  await expect(button('Enable')).toBeDisabled()
  await expect(button('Disable')).toBeEnabled()
  await page.getByRole('checkbox', { name: 'Select Node 002', exact: true }).check()
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

test('allows Full speed test beside another manual override and polls only while active', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  const speedTest = page.getByRole('button', { name: 'Full speed test', exact: true })
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  expect(prepared.state.status.selection.manualOverride).toBe(`proxy-${nodeID(2)}`)
  await expect(speedTest).toBeEnabled()
  await speedTest.click()
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/performance/manual-node')).toEqual([
    { path: '/api/v1/performance/manual-node', method: 'POST', body: { nodeId: nodeID(1) } },
  ])
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/selection/override')).toHaveLength(0)
  await expect(page.getByTestId('manual-performance')).toContainText('Running')
  await expect(page.getByTestId('manual-performance')).toContainText('Target')
  await expect(page.getByTestId('manual-performance')).toContainText('Node 001')
  await expect(page.getByTestId('manual-performance')).toContainText('Completed')
  await expect(page.getByTestId('manual-performance')).toContainText('10.0 Mbps')
  const performanceRequestsAfterCompletion = prepared.state.requests.filter((request) => request.path === '/api/v1/performance').length
  await page.waitForTimeout(1200)
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/performance')).toHaveLength(performanceRequestsAfterCompletion)

  await page.getByRole('button', { name: 'Clear selection', exact: true }).click()
  await page.getByRole('checkbox', { name: 'Select Node 002', exact: true }).check()
  await expect(speedTest).toBeDisabled()
  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  await expect(speedTest).toBeDisabled()
})

test('stops manual performance polling when the Nodes workspace unmounts', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  await page.getByRole('button', { name: 'Full speed test', exact: true }).click()
  await expect(page.getByTestId('manual-performance')).toContainText('Running')
  const performanceRequestsBeforeUnmount = prepared.state.requests.filter((request) => request.path === '/api/v1/performance').length

  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await revealDetails(page, 'Selection details')
  await expect(page.getByText('Native selection', { exact: true })).toBeVisible()
  await page.waitForTimeout(1200)
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/performance')).toHaveLength(performanceRequestsBeforeUnmount)
})

test('sends one batch remove preview, renders warnings, and reconciles after Apply', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)
  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  await page.getByRole('checkbox', { name: 'Select Node 002', exact: true }).check()
  await page.getByRole('checkbox', { name: 'Select Node 003', exact: true }).check()

  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Preview node change' })).toBeVisible()
  const previews = prepared.state.requests.filter((request) => request.path === '/api/v1/nodes/batch/remove/preview')
  expect(previews).toHaveLength(1)
  expect(previews[0].body).toEqual({ nodeIds: [nodeID(1), nodeID(2), nodeID(3)] })
  await expect(page.getByRole('dialog')).toContainText('The currently effective node changes in this preview.')
  await expect(page.getByRole('dialog')).toContainText('The current manual-override node changes in this preview.')
  await expect(page.getByRole('dialog')).toContainText('may return on a later subscription refresh')
  await expect(page.getByRole('list', { name: 'Node changes' }).getByRole('listitem')).toHaveCount(3)

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
  await expect(page.getByRole('list', { name: 'Node changes' }).getByRole('listitem')).toHaveCount(1)
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/subscriptions/refresh/preview')).toEqual([
    { path: '/api/v1/subscriptions/refresh/preview', method: 'POST', body: { subscriptionId: 'sub-12345678' } },
  ])
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
  await expect(page.locator('body')).not.toContainText('subscription.example')

  await page.getByRole('button', { name: 'Apply and validate' }).click()
  await expect(page.getByTestId('selected-count')).toHaveText('0 selected')
  await expect(page.getByRole('checkbox', { name: 'Select Node 003', exact: true })).toHaveCount(0)
  const applies = prepared.state.requests.filter((request) => request.path === '/api/v1/node-changes/apply')
  expect(applies).toHaveLength(1)
  expect(applies[0].body).toEqual({ previewToken: 'synthetic-subscription-1', acceptMissing: false })
})

test('keeps entered subscription URL readable, clears it, and retains saved URL on blank edit', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)
  const url = page.getByLabel('Subscription URL', { exact: true })
  await page.getByRole('button', { name: 'Add subscription', exact: true }).click()
  await expect(url).toHaveAttribute('type', 'url')
  await url.fill('https://subscription.example/synthetic-token')
  await expect(url).toHaveValue('https://subscription.example/synthetic-token')
  await page.locator('.composer').getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('button', { name: 'Add subscription', exact: true }).click()
  await expect(url).toHaveValue('')
  await page.locator('.composer').getByRole('button', { name: 'Cancel', exact: true }).click()

  await page.getByRole('button', { name: 'Edit Provider', exact: true }).click()
  await expect(url).toHaveValue('')
  await page.getByRole('button', { name: 'Preview update', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Preview node change' })).toBeVisible()
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/subscriptions/refresh/preview')).toEqual([
    { path: '/api/v1/subscriptions/refresh/preview', method: 'POST', body: { subscriptionId: 'sub-12345678', name: 'Provider', url: '' } },
  ])
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
  await expect(page.locator('body')).not.toContainText('synthetic-token')
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

test('offers native manual pin and retains profile replacement without exposing secrets', async ({ page }) => {
  const prepared = await prepare(page)
  page.__nodesIssues = prepared.issues
  await openNodes(page)

  await page.getByRole('checkbox', { name: 'Select Node 002', exact: true }).check()
  await expect(page.getByRole('button', { name: 'Clear manual override', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Clear selection', exact: true }).click()
  await page.getByRole('checkbox', { name: 'Select Node 001', exact: true }).check()
  await expect(page.getByRole('button', { name: 'Set manual override', exact: true })).toBeEnabled()
  expect(prepared.state.requests.filter((request) => request.path === '/api/v1/selection/override')).toHaveLength(0)

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
