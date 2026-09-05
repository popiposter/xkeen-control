import { expect, test } from '@playwright/test'

const origin = 'http://127.0.0.1:4173'
const tokenA = 'synthetic-session-A'
const tokenB = 'synthetic-session-B'
const kinds = ['panel', 'xkeen', 'xray', 'geodata', 'keeneticos', 'entware']
const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
const deferred = () => {
  let resolve
  const promise = new Promise((done) => { resolve = done })
  return { promise, resolve }
}
const operationPath = (operation) => `/api/v1/components/${operation === 'rollback' ? 'rollback' : 'apply'}`
const success = (operation) => ({ schemaVersion: 1, component: 'xray', operation, state: operation === 'rollback' ? 'rolled-back' : 'applied', version: '2.0.0', generation: 'synthetic-generation' })

async function prepare(page) {
  const issues = []
  page.on('pageerror', (error) => issues.push(error.message))
  page.on('console', (message) => {
    if (['error', 'warning'].includes(message.type()) && !message.text().startsWith('Failed to load resource:')) issues.push(message.text())
  })
  // A browser-side drain marker lets negative assertions run after response
  // consumption and React's render work, without racing route.fulfill().
  // It changes no body/status, and never cancels or replays a request.
  await page.addInitScript(() => {
    window.__componentReplies = 0
    window.__inventoryReplies = 0
    const fetchOriginal = window.fetch.bind(window)
    window.fetch = async (...args) => {
      const response = await fetchOriginal(...args)
      const mutation = /\/api\/v1\/components\/(apply|rollback)$/.test(String(args[0]))
      if (mutation || String(args[0]) === '/api/v1/components') {
        const textOriginal = response.text.bind(response)
        response.text = async () => {
          try { return await textOriginal() } finally {
            window.setTimeout(() => window.requestAnimationFrame(() => window.requestAnimationFrame(() => { if (mutation) window.__componentReplies++; else window.__inventoryReplies++ })), 0)
          }
        }
      }
      return response
    }
  })
  const state = {
    requests: [],
    handle: null,
    issues,
    inventory: {
      schemaVersion: 1,
      ...Object.fromEntries(kinds.map((kind) => [kind, { kind, state: 'present', present: true, version: '1.0.0', versionUnknown: false, capability: ['xkeen', 'xray', 'geodata'].includes(kind) ? 'supported' : 'informational', ...(kind === 'geodata' ? { items: [] } : {}) }])),
    },
  }
  let previewNumber = 0
  await page.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== origin) return route.abort('blockedbyclient')
    if (url.pathname === '/__component_session_fixture__') {
      return route.continue({ url: origin + '/tests/fixtures/components-session.html' })
    }
    if (!url.pathname.startsWith('/api/')) return route.continue()
    const path = url.pathname
    const entry = { path, method: request.method(), csrf: request.headers()['x-csrf-token'], body: request.postData() ? request.postDataJSON() : null }
    state.requests.push(entry)
    if (state.handle && await state.handle({ route, entry })) return
    switch (path) {
      case '/api/v1/session': return json(route, { csrfToken: tokenA })
      case '/api/v1/session/login': return json(route, { csrfToken: tokenB })
      case '/api/v1/session/logout': return json(route, {})
      case '/api/v1/status': return json(route, { controlPlane: { version: 'test' }, xray: {}, xkeen: {}, balancer: {}, observatory: {}, benchmark: { controlPlane: {} }, selection: {}, setup: {}, lifecycle: { maintenance: false, applying: false } })
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: '0.2.0' } })
      case '/api/v1/components': return json(route, state.inventory)
      case '/api/v1/components/preview': {
        const { component, operation, channel } = entry.body
        const target = { version: '2.0.0', generation: 'synthetic-generation' }
        return json(route, { schemaVersion: 1, component, operation, previewToken: `synthetic-preview-${++previewNumber}`, expiresAt: new Date(Date.now() + 300_000).toISOString(), ...(operation === 'rollback' ? { previous: target } : { channel, candidate: target }) })
      }
      case '/api/v1/components/cancel': return json(route, { canceled: true })
      default: return json(route, { error: 'unexpected synthetic route' }, 404)
    }
  })
  return state
}

const inventoryCount = (state) => state.requests.filter((item) => item.path === '/api/v1/components').length
const mutationRequests = (state) => state.requests.filter((item) => /\/components\/(apply|rollback)$/.test(item.path))
const confirm = async (page, operation) => {
  await page.locator('[data-component="xray"]').getByRole('button', { name: `Preview ${operation}` }).click()
  await page.getByRole('button', { name: `Confirm ${operation}` }).click()
}
const repliesDrained = async (page, count) => expect.poll(() => page.evaluate(() => window.__componentReplies)).toBe(count)
const signedIn = async (page) => expect(page.getByRole('button', { name: 'Sign out' })).toBeVisible()

for (const operation of ['update', 'rollback']) {
  for (const replyStatus of [200, 401]) {
    test(`ignores old ${operation} ${replyStatus} after logout and re-login`, async ({ page }) => {
      const state = await prepare(page)
      const release = deferred()
      state.handle = async ({ route, entry }) => {
        if (entry.path !== operationPath(operation)) return false
        await release.promise
        await json(route, replyStatus === 401 ? { error: 'unauthorized' } : success(operation), replyStatus)
        return true
      }
      await page.goto('/')
      await signedIn(page)
      await page.getByRole('button', { name: 'Components / Updates' }).click()
      await confirm(page, operation)
      await expect.poll(() => mutationRequests(state).length).toBe(1)
      await page.getByRole('button', { name: 'Sign out' }).click()
      await page.getByLabel('Panel password').fill('synthetic-password')
      await page.getByRole('button', { name: 'Sign in' }).click()
      await signedIn(page)
      await page.getByRole('button', { name: 'Components / Updates' }).click()
      await expect(page.locator('[data-component]')).toHaveCount(6)
      const readsBeforeReply = inventoryCount(state)
      release.resolve()
      await repliesDrained(page, 1)
      await signedIn(page)
      await expect(page.locator('.operation-result')).toHaveCount(0)
      expect(inventoryCount(state)).toBe(readsBeforeReply)
      expect(mutationRequests(state)).toEqual([expect.objectContaining({ csrf: tokenA, body: { previewToken: 'synthetic-preview-1' } })])
      expect(state.requests.filter((entry) => entry.path.endsWith('/components/cancel'))).toHaveLength(0)
      expect(state.issues).toEqual([])
    })

    test(`old ${operation} ${replyStatus} cannot clean up a new pending CSRF generation`, async ({ page }) => {
      const state = await prepare(page)
      const releaseA = deferred()
      const releaseB = deferred()
      state.handle = async ({ route, entry }) => {
        if (entry.path !== operationPath(operation)) return false
        const old = entry.csrf === tokenA
        await (old ? releaseA : releaseB).promise
        await json(route, old && replyStatus === 401 ? { error: 'unauthorized' } : success(operation), old ? replyStatus : 200)
        return true
      }
      await page.goto('/__component_session_fixture__')
      await expect(page).toHaveTitle('Component session fixture')
      await page.getByRole('button', { name: 'Refresh inventory' }).click()
      await confirm(page, operation)
      await expect.poll(() => mutationRequests(state).length).toBe(1)
      await page.getByRole('button', { name: 'Replace CSRF' }).click()
      await expect(page.getByTestId('session')).toHaveText(tokenB)
      await confirm(page, operation)
      await expect.poll(() => mutationRequests(state).length).toBe(2)
      const readsBeforeReply = inventoryCount(state)
      const sentinel = page.getByRole('button', { name: 'Focus sentinel' })
      await sentinel.focus()
      releaseA.resolve()
      await repliesDrained(page, 1)
      await expect(page.getByTestId('unauthorized')).toHaveText('0')
      await expect(page.locator('.operation-result')).toHaveCount(0)
      await expect(page.locator('.operation-running')).toBeVisible()
      await expect(page.locator('[data-component="xray"]').getByRole('button', { name: `Preview ${operation}` })).toBeDisabled()
      await expect(sentinel).toBeFocused()
      expect(inventoryCount(state)).toBe(readsBeforeReply)
      releaseB.resolve()
      await repliesDrained(page, 2)
      await expect(page.getByText(`Xray ${operation} completed`, { exact: true })).toBeVisible()
      await expect(page.locator('.operation-running')).toHaveCount(0)
      await expect.poll(() => inventoryCount(state)).toBe(readsBeforeReply + 1)
      expect(mutationRequests(state).map(({ csrf }) => csrf)).toEqual([tokenA, tokenB])
      expect(state.issues).toEqual([])
    })
  }
}

test('does not publish a stale post-operation refresh error after CSRF replacement', async ({ page }) => {
  const state = await prepare(page)
  const releaseRefresh = deferred()
  state.handle = async ({ route, entry }) => {
    if (entry.path === operationPath('update')) {
      await json(route, success('update'))
      return true
    }
    if (entry.path === '/api/v1/components' && inventoryCount(state) > 1) {
      await releaseRefresh.promise
      await json(route, { error: 'synthetic inventory unavailable' }, 503)
      return true
    }
    return false
  }
  await page.goto('/__component_session_fixture__')
  await page.getByRole('button', { name: 'Refresh inventory' }).click()
  await confirm(page, 'update')
  await expect.poll(() => inventoryCount(state)).toBe(2)
  await page.getByRole('button', { name: 'Replace CSRF' }).click()
  await expect(page.getByTestId('session')).toHaveText(tokenB)
  releaseRefresh.resolve()
  await expect.poll(() => page.evaluate(() => window.__inventoryReplies)).toBe(2)
  await expect(page.getByTestId('refresh-error')).toHaveText('')
  await expect(page.locator('.operation-result')).toHaveCount(0)
  await expect(page.getByTestId('unauthorized')).toHaveText('0')
  expect(mutationRequests(state)).toHaveLength(1)
  expect(state.issues).toEqual([])
})

test('presence labels follow backend missing/unknown states, not versionUnknown', async ({ page }, testInfo) => {
  const state = await prepare(page)
  // These are real inventory shapes: absent/unknown components also have
  // versionUnknown=true; a present path need not mean a known usable component.
  state.inventory.xray = { kind: 'xray', state: 'missing', present: false, versionUnknown: true, capability: 'unsupported', reasonCode: 'not-installed' }
  state.inventory.xkeen = { kind: 'xkeen', state: 'unknown', present: true, versionUnknown: true, capability: 'unsupported', reasonCode: 'layout-unsupported' }
  state.inventory.keeneticos = { kind: 'keeneticos', state: 'unknown', present: false, versionUnknown: true, capability: 'informational', reasonCode: 'signal-unavailable' }
  state.inventory.entware = { kind: 'entware', state: 'missing', present: false, versionUnknown: true, capability: 'unsupported', reasonCode: 'not-installed' }
  state.inventory.geodata = { ...state.inventory.geodata, version: '', versionUnknown: true }
  await page.goto('/')
  await page.getByRole('button', { name: 'Components / Updates' }).click()
  for (const kind of ['xray', 'entware']) await expect(page.locator(`[data-component="${kind}"] .component-version`)).toHaveText('Missing')
  for (const kind of ['xkeen', 'keeneticos']) await expect(page.locator(`[data-component="${kind}"] .component-version`)).toHaveText('Unknown')
  await expect(page.locator('[data-component="geodata"] .component-version')).toHaveText('Present · version unknown')
  await expect(page.locator('[data-component="panel"] .component-version')).toHaveText('1.0.0')
  await page.screenshot({ path: testInfo.outputPath('presence-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: testInfo.outputPath('presence-mobile.png'), fullPage: true })
  expect(state.issues).toEqual([])
})
