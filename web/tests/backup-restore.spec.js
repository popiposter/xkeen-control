import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-backup-csrf'
const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })

const baseStatus = {
  controlPlane: { version: 'test', uptimeSeconds: 12 },
  xray: {},
  xkeen: {},
  balancer: {},
  observatory: {},
  benchmark: { controlPlane: {} },
  selection: {},
  setup: { state: 'ready', eligible: false, reasonCode: 'already-configured' },
  lifecycle: { maintenance: false, applying: false },
}

const preview = (mode, blockers = []) => ({
  schemaVersion: 1,
  previewToken: `synthetic-${mode}-token`,
  expiresAt: new Date(Date.now() + 300_000).toISOString(),
  mode,
  noop: false,
  containsSecrets: mode !== 'settings-only',
  compatibility: { blockers },
  changes: { applianceChanged: true, nodesAdded: mode === 'settings-only' ? 0 : 1 },
})

async function prepare(page, { blockers = [], secretExportStatus = 200 } = {}) {
  const state = { requests: [] }
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let body = null
    if (request.postData() && request.headers()['content-type']?.includes('application/json')) {
      body = request.postDataJSON()
    }
    state.requests.push({ path, method: request.method(), body, csrf: request.headers()['x-csrf-token'] })
    switch (path) {
      case '/api/v1/session': return json(route, { csrfToken })
      case '/api/v1/status': return json(route, baseStatus)
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/update': return json(route, { channel: 'stable', installed: { version: 'test' } })
      case '/api/v1/backup/import/preview': return json(route, preview(url.searchParams.get('mode'), blockers))
      case '/api/v1/backup/import/apply': return json(route, { schemaVersion: 1, operation: 'restore', state: 'ready' })
      case '/api/v1/backup/import/cancel': return json(route, { canceled: true })
      case '/api/v1/backup/export-secret':
        return secretExportStatus === 200
          ? route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
          : json(route, { error: 'reauthentication failed' }, secretExportStatus)
      default: return json(route, { error: 'unexpected synthetic route' }, 404)
    }
  })
  await page.goto('/')
  await page.getByRole('button', { name: 'Backup & Restore' }).click()
  return state
}

const chooseBundle = (page) => page.getByLabel('Backup bundle').setInputFiles({
  name: 'synthetic-backup.json',
  mimeType: 'application/json',
  buffer: Buffer.from('{"schemaVersion":1}'),
})

test('settings-only restore applies only the short-lived preview token', async ({ page }) => {
  const state = await prepare(page)
  await chooseBundle(page)
  await page.getByRole('button', { name: 'Preview restore' }).click()
  await expect(page.getByRole('heading', { name: 'Ready for confirmation' })).toBeVisible()
  await page.getByRole('button', { name: 'Apply restore' }).click()
  await expect(page.getByText('Restore applied and the dashboard was refreshed.')).toBeVisible()

  const apply = state.requests.find((request) => request.path === '/api/v1/backup/import/apply')
  expect(apply).toEqual(expect.objectContaining({
    method: 'POST',
    body: { previewToken: 'synthetic-settings-only-token' },
    csrf: csrfToken,
  }))
})

test('destructive restore requires confirmation and blockers keep Apply disabled', async ({ page }) => {
  await prepare(page, { blockers: ['nodes-authority-unsupported'] })
  await page.getByLabel('Restore mode').selectOption('replace-registry')
  await chooseBundle(page)
  await expect(page.getByRole('button', { name: 'Preview restore' })).toBeDisabled()
  await page.getByLabel('I understand this restore can replace or merge secret-bearing node registry state.').check()
  await page.getByRole('button', { name: 'Preview restore' }).click()
  await expect(page.getByText('The current node registry cannot accept this restore mode.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply restore' })).toBeDisabled()
})

test('encrypted export validates passphrase bytes and handles reauthentication locally', async ({ page }) => {
  await prepare(page, { secretExportStatus: 401 })
  await page.getByLabel('Current panel password').fill('synthetic-current-password')
  await page.getByLabel('Encryption passphrase').fill('пароль')
  await page.getByLabel('Confirm passphrase').fill('пароль')
  await page.getByRole('button', { name: 'Download encrypted backup' }).click()
  await expect(page.getByText('Current panel password was not accepted.')).toBeVisible()
  await expect(page.getByLabel('Current panel password')).toHaveValue('')
  await expect(page.getByLabel('Encryption passphrase')).toHaveValue('')
  await expect(page.getByLabel('Confirm passphrase')).toHaveValue('')
})
