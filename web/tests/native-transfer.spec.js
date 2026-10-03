import { expect, test } from '@playwright/test'

const csrfToken = 'synthetic-transfer-csrf'
const token = 'a'.repeat(32)
const ready = { token, digest: 'b'.repeat(64), files: ['02_dns.json', '05_routing.json'], nodes: 2, subscriptions: 1, references: [], interfaces: ['eth0'], expiresAt: new Date(Date.now() + 300000).toISOString() }
const json = (route, value, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(value) })
const status = { controlPlane: { version: 'test' }, xray: {}, xkeen: {}, balancer: {}, observatory: {}, benchmark: { controlPlane: {} }, selection: {}, native: { installation: 'available', xrayRunning: true }, lifecycle: { maintenance: false, applying: false } }
async function prepare(page, { mapping = false, stageStatus = 200, delay = null, exportStatus = 200 } = {}) {
  const state = { requests: [], previews: 0 }
	let digest = 'c'.repeat(64), pending = null
	const routing = '{"routing":{"domainStrategy":"AsIs","rules":[]}}'
  await page.route('**/api/v1/**', async (route) => {
    const r = route.request(), path = new URL(r.url()).pathname
    const body = r.headers()['content-type']?.includes('application/json') && r.postData() ? r.postDataJSON() : null
    state.requests.push({ path, body, csrf: r.headers()['x-csrf-token'], upload: path.endsWith('/preview') ? r.postData() : null })
    switch (path) {
      case '/api/v1/session': return json(route, { csrfToken })
      case '/api/v1/status': return json(route, status)
      case '/api/v1/nodes': return json(route, { total: 0, nodes: [], subscriptions: [] })
      case '/api/v1/performance': return json(route, { nodes: [] })
      case '/api/v1/config-summary': return json(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/xkeen/transfer/preview':
        state.previews++
        if (delay) await delay()
        return json(route, mapping && state.previews === 1 ? { ...ready, token: '', mappingRequired: true, references: [{ source: 'old0', destination: '' }] } : ready)
      case '/api/v1/xkeen/transfer/stage':
			 digest = ready.digest; pending = { files: ['05_routing.json'], drift: false }
			 return json(route, stageStatus === 200 ? { digest: ready.digest, saved: true, restartRequired: true } : { error: 'Save outcome unavailable. Inspect saved configurations; do not repeat this transfer.' }, stageStatus)
		 case '/api/v1/xkeen/config/workspace': return json(route, { digest, documents: { '05_routing.json': {} }, targets: {}, pending })
		 case '/api/v1/xkeen/config/document': return json(route, { digest, document: { text: routing } })
      case '/api/v1/xkeen/commands': return json(route, { commands: [], available: false })
      case '/api/v1/xkeen/transfer/cancel': return json(route, { canceled: true })
      case '/api/v1/backup/export-secret': return json(route, exportStatus === 200 ? {} : { error: 'reauthentication failed' }, exportStatus)
      default: return json(route, { error: 'unexpected synthetic route' }, 404)
    }
  })
  await page.goto('/')
  await page.getByRole('button', { name: 'Backup & Restore', exact: true }).click()
  return state
}
async function upload(page) {
  await page.getByLabel('Backup bundle').setInputFiles({ name: 'synthetic.json', mimeType: 'application/json', buffer: Buffer.from('{}') })
  await page.getByLabel('Backup passphrase', { exact: true }).fill('synthetic transfer passphrase')
  await page.getByRole('button', { name: 'Preview transfer' }).click()
}

test('native transfer stages token only, clears secrets and leaves Restart explicit', async ({ page }) => {
  const state = await prepare(page)
  await upload(page)
  await expect(page.getByText('Validated preview ready.', { exact: false })).toBeVisible()
  await expect(page.getByLabel('Backup passphrase', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Backup bundle')).toHaveValue('')
  await expect(page.getByRole('button', { name: 'Save transferred settings' })).toBeDisabled()
  await page.getByRole('checkbox', { name: /I checked this router/ }).check()
  await page.getByRole('button', { name: 'Save transferred settings' }).click()
  await expect(page.getByText('Configurations saved.', { exact: false })).toBeVisible()
  expect(state.requests.find(r => r.path.endsWith('/stage'))).toMatchObject({ body: { token, nativeSettingsChecked: true }, csrf: csrfToken })
  expect(state.requests.some(r => r.path.endsWith('/apply') || r.path.endsWith('/jobs/start'))).toBe(false)
  await expect(page.getByRole('button', { name: 'Save transferred settings' })).toHaveCount(0)
})

test('missing interfaces retain private upload until validated mapping; new file cancels ready preview', async ({ page }) => {
  const state = await prepare(page, { mapping: true })
  await upload(page)
  await expect(page.getByLabel('Interface old0')).toBeVisible()
  await expect(page.getByLabel('Backup passphrase', { exact: true })).toHaveValue('synthetic transfer passphrase')
  await expect(page.getByRole('button', { name: 'Preview transfer' })).toBeDisabled()
  await page.getByLabel('Interface old0').selectOption('eth0')
  await page.getByRole('button', { name: 'Preview transfer' }).click()
  await expect(page.getByText('Validated preview ready.', { exact: false })).toBeVisible()
  expect(state.requests.filter(r => r.path.endsWith('/preview'))[1].upload).toContain('"old0":"eth0"')
  await page.getByLabel('Backup bundle').setInputFiles({ name: 'other.json', mimeType: 'application/json', buffer: Buffer.from('{}') })
  await expect.poll(() => state.requests.filter(r => r.path.endsWith('/cancel')).length).toBe(1)
  expect(state.requests.find(r => r.path.endsWith('/cancel')).body).toEqual({ token })
})

test('failed Stage is consumed locally and requires inspection, with no automatic retry', async ({ page }) => {
  const state = await prepare(page, { stageStatus: 500 })
  await upload(page)
  await page.getByRole('checkbox', { name: /I checked this router/ }).check()
  await page.getByRole('button', { name: 'Save transferred settings' }).click()
  await expect(page.getByText('Save outcome unavailable.', { exact: false })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save transferred settings' })).toHaveCount(0)
  expect(state.requests.filter(r => r.path.endsWith('/stage'))).toHaveLength(1)
})

test('transfer readback uses the retained editor and preserves unfinished drafts through navigation', async ({ page }) => {
  const state = await prepare(page)
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByLabel('Configuration file', { exact: true })).toBeVisible()
  await page.getByLabel('Routing domain resolution', { exact: true }).selectOption('IPIfNonMatch')
  await page.getByRole('button', { name: 'Backup & Restore', exact: true }).click()
  await upload(page)
  await page.getByRole('checkbox', { name: /I checked this router/ }).check()
  await page.getByRole('button', { name: 'Save transferred settings' }).click()
  await page.getByRole('button', { name: 'Review saved configurations' }).click()
  await expect(page.getByLabel('Routing domain resolution', { exact: true })).toHaveValue('IPIfNonMatch')
  await expect(page.getByText('05_routing.json', { exact: false }).first()).toBeVisible()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByLabel('Routing domain resolution', { exact: true })).toHaveValue('IPIfNonMatch')
  expect(state.requests.filter(r => /config\/(save|text|apply)|jobs\/start/.test(r.path))).toEqual([])
})

test('export clears password fields after rejection; detached preview is canceled', async ({ page }) => {
  let release
  const wait = new Promise(resolve => { release = resolve })
  const state = await prepare(page, { exportStatus: 401, delay: () => wait })
  await page.getByLabel('Current panel password').fill('synthetic password')
  await page.getByLabel('Encryption passphrase', { exact: true }).fill('synthetic export passphrase')
  await page.getByLabel('Confirm passphrase').fill('synthetic export passphrase')
  await page.getByRole('button', { name: 'Download encrypted backup' }).click()
  await expect(page.getByText('Current panel password was not accepted.')).toBeVisible()
  await expect(page.getByLabel('Current panel password')).toHaveValue('')
  await expect(page.getByLabel('Encryption passphrase', { exact: true })).toHaveValue('')
  await upload(page)
  await expect.poll(() => state.previews).toBe(1)
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  release()
  await expect.poll(() => state.requests.filter(r => r.path.endsWith('/cancel')).length).toBe(1)
  expect(state.requests.some(r => r.path.endsWith('/stage'))).toBe(false)
})
