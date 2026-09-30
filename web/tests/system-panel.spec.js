import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-system-panel-csrf'
const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })

const status = {
  controlPlane: { version: 'synthetic', uptimeSeconds: 3661 },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  observatory: { healthy: 2, total: 3, apiReachable: true },
  balancer: {}, selection: {}, benchmark: { controlPlane: { running: false } },
  setup: { runtime: 'running', credential: 'configured', xkeen: 'ready', xray: 'ready', configuration: 'ready' },
  lifecycle: { maintenance: false, applying: false },
}

const listenerProjection = (overrides = {}) => ({
  host: '127.0.0.1', port: 8787, source: 'default', editability: 'editable', allowedHosts: ['127.0.0.1', '10.0.0.4'], ...overrides,
})

const updateStatus = (overrides = {}) => ({
  installed: { product: 'xkeen-control', version: '0.2.0', sourceCommit: 'a'.repeat(40), channel: 'stable' },
  channel: 'stable',
  policy: { channel: 'stable', mode: 'manual', checkCadenceMinutes: 360 },
  rollbackAvailable: true, signingKeyConfigured: true,
  latestCompatibleVersion: null, latestChannel: null, latestSource: '', lastCheckResult: '',
  ...overrides,
})

async function prepare(page, options = {}) {
  const state = {
    listener: listenerProjection(options.listener),
    update: updateStatus(options.update),
    reconnected: false,
    requests: [],
    issues: [],
    previews: new Map(),
    checked: null,
    notification: { provider: 'telegram', configured: false, enabled: false, authorityState: 'unconfigured', deliveryState: 'idle' },
    lifecycle: Object.hasOwn(options, 'lifecycle') ? options.lifecycle : status.lifecycle,
  }
  page.on('pageerror', (error) => state.issues.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (['error', 'warning'].includes(message.type()) && !message.text().startsWith('Failed to load resource:')) state.issues.push(`${message.type()}: ${message.text()}`)
  })
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    let body = null
    if (request.postData()) {
      try { body = request.postDataJSON() } catch { body = request.postData() }
    }
    state.requests.push({ path, method: request.method(), body, csrf: request.headers()['x-csrf-token'] || '' })
    switch (path) {
      case '/api/v1/session': {
        if (state.reconnected) state.listener = { ...state.listener, host: '10.0.0.4', source: 'file' }
        return json(route, { csrfToken: state.reconnected ? `${csrfToken}-reconnected` : csrfToken })
      }
      case '/api/v1/status': return json(route, { ...status, lifecycle: state.lifecycle })
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/panel/listener': return json(route, state.listener)
      case '/api/v1/panel/listener/preview': {
        if (options.delayListenerPreview) await new Promise((resolve) => setTimeout(resolve, 100))
        const token = `listener-preview-${state.previews.size + 1}`
        state.previews.set(token, body)
        return json(route, { previewToken: token, expiresAt: new Date(Date.now() + 300_000).toISOString(), before: { host: state.listener.host, port: state.listener.port }, after: { host: body.host, port: state.listener.port }, noop: body.host === state.listener.host, reconnectClassification: body.host === state.listener.host ? 'no-op' : 'loopback-to-lan', restartRequired: body.host !== state.listener.host, sessionInvalidated: body.host !== state.listener.host, loginRequired: body.host !== state.listener.host })
      }
      case '/api/v1/panel/listener/apply': {
        const pending = state.previews.get(body.previewToken)
        state.previews.delete(body.previewToken)
        if (options.dropListenerApplyResponse) return route.abort()
        if (options.malformedListenerApplyResponse) return route.fulfill({ status: 202, contentType: 'application/json', body: '{malformed' })
        return json(route, { accepted: true, state: 'rebind-started', before: { host: '127.0.0.1', port: 8787 }, after: { host: pending?.host || '127.0.0.1', port: 8787 }, noop: false, reconnectClassification: 'loopback-to-lan', restartRequired: true, sessionInvalidated: true, loginRequired: true }, 202)
      }
      case '/api/v1/panel/listener/cancel': state.previews.delete(body.previewToken); return json(route, { canceled: true })
      case '/api/v1/update': return json(route, state.update)
      case '/api/v1/notifications': return json(route, state.notification)
      case '/api/v1/notifications/configure':
        state.notification = { ...state.notification, configured: true, enabled: false, authorityState: 'configured' }
        if (options.rejectNotificationConfigure) return json(route, { error: 'synthetic_notification_token_sentinel', code: 'invalid-request' }, 400)
        return json(route, state.notification)
      case '/api/v1/notifications/enabled': state.notification.enabled = body.enabled; return json(route, state.notification)
      case '/api/v1/notifications/test': state.notification.deliveryState = 'delivered'; return json(route, state.notification)
      case '/api/v1/notifications/clear': state.notification = { provider: 'telegram', configured: false, enabled: false, authorityState: 'unconfigured', deliveryState: 'idle' }; return json(route, state.notification)
      case '/api/v1/update/policy':
        state.update = { ...state.update, channel: body.channel, policy: { ...state.update.policy, ...body }, latestCompatibleVersion: null, latestChannel: null, latestSource: '' }
        return json(route, state.update)
      case '/api/v1/update/check': {
        state.checked = body
        const channel = body.channel
        state.update = { ...state.update, channel, policy: { ...state.update.policy, channel }, latestCompatibleVersion: channel === 'beta' ? body.version : '1.2.3', latestChannel: channel, latestSource: 'github-release', lastCheckResult: 'ok', lastCheckAt: new Date().toISOString() }
        return json(route, state.update)
      }
      case '/api/v1/update/apply': {
        if (options.dropApplyResponse) return route.abort()
        return json(route, { accepted: true, state: 'update-attempt-started' }, 202)
      }
      case '/api/v1/update/rollback': {
        if (options.dropRollbackResponse) return route.abort()
        if (options.rollbackRejectOnce && state.requests.filter(({ path }) => path === '/api/v1/update/rollback').length === 1) return json(route, { error: 'panel rollback rejected', code: 'busy' }, 409)
        return json(route, { accepted: true, state: 'rollback-attempt-started' }, 202)
      }
      case '/api/v1/session/password': return json(route, { authenticated: false, state: 'reauthentication-required' })
      default: return json(route, { error: `unexpected synthetic route: ${path}` }, 404)
    }
  })
  return state
}

test('configures tests enables disables and clears without redisplaying or storing credentials', async ({ page }) => {
  const state = await prepare(page); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  const card = page.getByRole('region', { name: 'Notifications', exact: true })
  await expect(card).toBeVisible()
  await page.getByLabel('Telegram bot token').fill('123456:synthetic_notification_token_sentinel')
  await page.getByLabel('Telegram chat ID').fill('-1234567890123')
  expect(await page.getByLabel('Telegram bot token').getAttribute('type')).toBe('password')
  expect(await page.getByLabel('Telegram chat ID').getAttribute('type')).toBe('password')
  await page.getByRole('button', { name: 'Configure notifications', exact: true }).click()
  await expect(page.getByLabel('Telegram bot token')).toHaveValue('')
  await expect(page.getByLabel('Telegram chat ID')).toHaveValue('')
  await expect(card).toContainText('Disabled')
  await page.getByRole('button', { name: 'Send test notification', exact: true }).click()
  await expect(card).toContainText('Test notification delivered.')
  expect(state.notification.enabled).toBe(false)
  await page.getByRole('button', { name: 'Enable notifications', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Disable notifications', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Disable notifications', exact: true }).click()
  await page.getByRole('button', { name: 'Clear notifications', exact: true }).click()
  await expect(card).toContainText('unconfigured')
  const mutations = state.requests.filter(({ path, method }) => path.startsWith('/api/v1/notifications/') && method === 'POST')
  expect(mutations.map(({ body }) => body)).toEqual([{ botToken: '123456:synthetic_notification_token_sentinel', chatId: '-1234567890123' }, {}, { enabled: true }, { enabled: false }, {}])
  expect(mutations.every(({ csrf }) => csrf === csrfToken)).toBe(true)
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0])
  expect(await page.locator('body').textContent()).not.toContain('synthetic_notification_token_sentinel')
  expect(await page.locator('body').textContent()).not.toContain('-1234567890123')
  await page.screenshot({ path: '../dist/qualification/issue99/notifications-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(card).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: '../dist/qualification/issue99/notifications-mobile.png', fullPage: true })
})

test('clears submitted credentials on rejection and navigation and discards raw errors', async ({ page }) => {
  const state = await prepare(page, { rejectNotificationConfigure: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('Telegram bot token').fill('123456:synthetic_notification_token_sentinel')
  await page.getByLabel('Telegram chat ID').fill('-1234567890123')
  await page.getByRole('button', { name: 'Configure notifications', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Notifications' })).toContainText('Notification operation failed.')
  await expect(page.getByLabel('Telegram bot token')).toHaveValue('')
  await expect(page.getByLabel('Telegram chat ID')).toHaveValue('')
  expect(await page.locator('body').textContent()).not.toContain('synthetic_notification_token_sentinel')
  await page.getByLabel('Telegram bot token').fill('unsent_secret_sentinel')
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await expect(page.getByLabel('Telegram bot token')).toHaveValue('')
})

for (const [channel, mode, text] of [['stable', 'notify', 'Background discovery never'], ['beta', 'notify', 'Unsupported channel'], ['stable', 'auto-stable', 'Unsupported mode']]) {
  test(`projects ${channel} ${mode} without arming Apply or explicit Check`, async ({ page }) => {
    const state = await prepare(page, { update: { channel, policy: { channel, mode, checkCadenceMinutes: 60 }, scheduler: { state: mode === 'auto-stable' ? 'unsupported-mode' : channel === 'beta' ? 'unsupported-channel' : 'completed', notificationState: 'notified' } } }); page.__systemIssues = state.issues
    await page.goto('/')
    await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
    await expect(page.getByRole('region', { name: 'Signed panel release' })).toContainText(channel === 'stable' && mode === 'notify' ? 'It never authorizes Apply' : text)
    await expect(page.getByRole('button', { name: 'Apply checked release', exact: true })).toBeDisabled()
    expect(state.requests.filter(({ path }) => ['/api/v1/update/check', '/api/v1/update/apply'].includes(path))).toHaveLength(0)
  })
}

test('saves stable notify policy without checking or enabling Apply', async ({ page }) => {
  const state = await prepare(page); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('Panel notification mode').selectOption('notify')
  await page.getByLabel('Panel check cadence (minutes)').fill('60')
  await page.getByRole('button', { name: 'Save notify policy', exact: true }).click()
  await expect(page.getByText('Panel notify policy saved', { exact: true })).toBeVisible()
  expect(state.requests.find(({ path }) => path === '/api/v1/update/policy').body).toEqual({ channel: 'stable', mode: 'notify', checkCadenceMinutes: 60 })
  await expect(page.getByRole('button', { name: 'Apply checked release', exact: true })).toBeDisabled()
})

test.afterEach(async ({ page }) => {
  if (page.__systemIssues) expect(page.__systemIssues).toEqual([])
})

test('keeps System / Panel lazy and uses the final tail navigation', async ({ page }) => {
  const state = await prepare(page); page.__systemIssues = state.issues
  await page.goto('/')
  await expect(page.locator('.section-nav button')).toHaveCount(8)
  expect(await page.locator('.section-nav button').allTextContents()).toEqual(['Overview', 'Nodes 0', 'Routing', 'DNS', 'Performance', 'Components / Updates', 'Backup & Restore', 'System / Panel'])
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener' || path === '/api/v1/update' || path === '/api/v1/notifications')).toHaveLength(0)
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await expect(page.getByText('Management listener', { exact: true })).toBeVisible()
  await expect.poll(() => state.requests.filter(({ path }) => path === '/api/v1/panel/listener')).toHaveLength(1)
  await expect.poll(() => state.requests.filter(({ path }) => path === '/api/v1/update')).toHaveLength(1)
  await page.waitForTimeout(5_300)
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener')).toHaveLength(1)
  expect(state.requests.filter(({ path }) => path === '/api/v1/update')).toHaveLength(1)
})

test('sends only the server-listed host and presents rebind 202 as a handoff', async ({ page }) => {
  const state = await prepare(page); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await expect(page.getByRole('heading', { name: 'Review management listener rebind' })).toBeVisible()
  const preview = state.requests.find(({ path }) => path === '/api/v1/panel/listener/preview')
  expect(preview.body).toEqual({ host: '10.0.0.4' })
  await page.getByRole('button', { name: 'Start rebind handoff' }).click()
  await expect(page.getByText('Listener rebind handoff started', { exact: true })).toBeVisible()
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/apply')).toHaveLength(1)
  expect(state.requests.find(({ path }) => path === '/api/v1/panel/listener/apply').body).toEqual({ previewToken: 'listener-preview-1' })
  await expect(page.locator('div.notice.warning').filter({ hasText: /reconnect and verify/i })).toBeVisible()
  await expect(page.locator('p.system-blocked').filter({ hasText: /same-session Refresh cannot prove completion/i })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
  await expect(page.getByLabel('New management host')).toBeDisabled()
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('heading', { name: '127.0.0.1:8787', exact: true })).toBeVisible()
  await expect(page.getByLabel('New management host')).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()

  state.reconnected = true
  await page.reload()
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await expect(page.getByRole('heading', { name: '10.0.0.4:8787', exact: true })).toBeVisible()
  await expect(page.getByLabel('New management host')).toBeEnabled()
  await page.getByLabel('New management host').selectOption('127.0.0.1')
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeEnabled()
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await expect(page.getByRole('heading', { name: 'Review management listener rebind' })).toBeVisible()
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/apply')).toHaveLength(1)
})

test('locks listener rebind after a lost Apply response until a fresh listener read', async ({ page }) => {
  const state = await prepare(page, { dropListenerApplyResponse: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await page.getByRole('button', { name: 'Start rebind handoff' }).click()
  await expect(page.getByText('Listener rebind outcome is unknown', { exact: true })).toBeVisible()
  await expect(page.locator('p.system-blocked').filter({ hasText: /same-session Refresh cannot prove completion/i })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/apply')).toHaveLength(1)
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/cancel')).toHaveLength(0)

  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('heading', { name: '127.0.0.1:8787', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()

  state.reconnected = true
  await page.reload()
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await expect(page.getByRole('heading', { name: '10.0.0.4:8787', exact: true })).toBeVisible()
  await page.getByLabel('New management host').selectOption('127.0.0.1')
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeEnabled()
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await expect(page.getByRole('heading', { name: 'Review management listener rebind' })).toBeVisible()
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/preview')).toHaveLength(2)
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/apply')).toHaveLength(1)
})

test('treats a malformed listener Apply response as unknown without replay', async ({ page }) => {
  const state = await prepare(page, { malformedListenerApplyResponse: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await page.getByRole('button', { name: 'Start rebind handoff' }).click()
  await expect(page.getByText('Listener rebind outcome is unknown', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/apply')).toHaveLength(1)
  expect(state.requests.filter(({ path }) => path === '/api/v1/panel/listener/cancel')).toHaveLength(0)
})

test('safe-cancels a late listener Preview after navigation', async ({ page }) => {
  const state = await prepare(page, { delayListenerPreview: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await expect.poll(() => state.requests.filter(({ path }) => path === '/api/v1/panel/listener/cancel')).toHaveLength(1)
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await expect(page.getByText('Management listener', { exact: true })).toBeVisible()
  expect(await page.getByRole('heading', { name: 'Review management listener rebind' }).count()).toBe(0)
})

test('checks then applies the exact checked release and never reports install success', async ({ page }) => {
  const state = await prepare(page); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByRole('button', { name: 'Check fixed release' }).click()
  await expect(page.getByText('Explicit release Check completed')).toBeVisible()
  await page.getByRole('button', { name: 'Apply checked release' }).click()
  await expect(page.getByText('Panel update attempt started')).toBeVisible()
  expect(state.checked).toEqual({ channel: 'stable' })
  expect(state.requests.find(({ path }) => path === '/api/v1/update/apply').body).toEqual({ channel: 'stable', version: '1.2.3' })
  await expect(page.getByText(/final install success is not proven/i)).toBeVisible()
})

test('locks checked Apply after a lost response until a fresh explicit Check', async ({ page }) => {
  const state = await prepare(page, { dropApplyResponse: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByRole('button', { name: 'Check fixed release' }).click()
  await page.getByRole('button', { name: 'Apply checked release' }).click()
  await expect(page.getByText('Panel update outcome is unknown', { exact: true })).toBeVisible()
  await expect(page.locator('p.system-blocked').filter({ hasText: /fresh explicit Check before another Apply/i })).toBeVisible()
  const apply = page.getByRole('button', { name: 'Apply checked release' })
  await expect(apply).toBeDisabled()
  expect(state.requests.filter(({ path }) => path === '/api/v1/update/apply')).toHaveLength(1)
  await page.getByRole('button', { name: 'Check fixed release' }).click()
  await expect(apply).toBeEnabled()
  expect(state.requests.filter(({ path }) => path === '/api/v1/update/check')).toHaveLength(2)
})

test('locks rollback after a lost response and does not replay it', async ({ page }) => {
  const state = await prepare(page, { dropRollbackResponse: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByRole('button', { name: 'Rollback retained release' }).click()
  await expect(page.getByText('Panel rollback outcome is unknown', { exact: true })).toBeVisible()
  await expect(page.locator('p.system-blocked').filter({ hasText: /retained generation before another action/i })).toBeVisible()
  const rollback = page.getByRole('button', { name: 'Rollback retained release' })
  await expect(rollback).toBeDisabled()
  expect(state.requests.filter(({ path }) => path === '/api/v1/update/rollback')).toHaveLength(1)
})

test('gates panel update and rollback during maintenance, Apply, or unavailable lifecycle while Check stays available', async ({ page }) => {
  const state = await prepare(page, {
    update: {
      latestCompatibleVersion: '1.2.3', latestChannel: 'stable', latestSource: 'github-release',
      latestSourceCommit: 'b'.repeat(40),
    },
  }); page.__systemIssues = state.issues

  for (const lifecycle of [
    { maintenance: true, applying: false },
    { maintenance: false, applying: true },
    null,
    { maintenance: false },
  ]) {
    state.lifecycle = lifecycle
    await page.goto('/')
    await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Check fixed release' })).toBeEnabled()
    await expect(page.getByRole('button', { name: 'Apply checked release' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Rollback retained release' })).toBeDisabled()
    await expect(page.getByRole('heading', { name: '0.2.0', exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Check fixed release' }).click()
    await expect(page.getByText('Explicit release Check completed')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Apply checked release' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Rollback retained release' })).toBeDisabled()
  }

  expect(state.requests.filter(({ path }) => path === '/api/v1/update/check')).toHaveLength(4)
  expect(state.requests.filter(({ path }) => path === '/api/v1/update/apply' || path === '/api/v1/update/rollback')).toHaveLength(0)
})

test('re-arms rollback after a proven HTTP rejection and allows a later retry', async ({ page }) => {
  const state = await prepare(page, { rollbackRejectOnce: true }); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByRole('button', { name: 'Rollback retained release' }).click()
  await expect(page.getByText('Panel rollback was rejected', { exact: true })).toBeVisible()
  await expect(page.getByText('Panel rollback outcome is unknown', { exact: true })).toHaveCount(0)
  const rollback = page.getByRole('button', { name: 'Rollback retained release' })
  await expect(rollback).toBeEnabled()
  expect(state.requests.filter(({ path }) => path === '/api/v1/update/rollback')).toHaveLength(1)
  await rollback.click()
  await expect(page.getByText('Panel rollback attempt started', { exact: true })).toBeVisible()
  expect(state.requests.filter(({ path }) => path === '/api/v1/update/rollback')).toHaveLength(2)
})

test('password replacement uses the exact RAM-only request and returns to login', async ({ page }) => {
  const state = await prepare(page); page.__systemIssues = state.issues
  await page.goto('/')
  await page.getByRole('button', { name: 'System / Panel', exact: true }).click()
  await page.getByLabel('New panel password').fill('synthetic-new-password')
  await page.getByLabel('Confirm new password').fill('synthetic-new-password')
  await page.getByRole('button', { name: 'Replace password' }).click()
  await expect(page.getByRole('heading', { name: 'XKeen Control' })).toBeVisible()
  expect(state.requests.find(({ path }) => path === '/api/v1/session/password').body).toEqual({ newPassword: 'synthetic-new-password' })
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
})
