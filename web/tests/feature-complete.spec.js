import { revealDetails, revealSystemSettings, revealNavigation } from './fixtures/disclosures.js'
import { expect, test } from '@playwright/test'
import { featureCompleteRequests, mountFeatureCompleteDashboard, PRIVATE_SENTINELS } from './fixtures/feature-complete-model.js'

const lazySettingsPaths = [
  '/api/v1/appliance/policy',
  '/api/v1/appliance/dns-observatory',
  '/api/v1/performance/policy',
  '/api/v1/components',
  '/api/v1/components/policy',
  '/api/v1/panel/listener',
  '/api/v1/update',
]

const openSection = async (page, name) => {
  await (await revealNavigation(page)).getByRole('button').filter({ hasText: name }).click()
  if (name === 'System / Panel') await revealSystemSettings(page)
  if (name === 'Components / Updates') await revealDetails(page, 'Background discovery')
  if (name === 'Routing') await revealDetails(page, 'Protected routing policy')
  if (name === 'DNS') await revealDetails(page, 'Protected DNS and Observatory')
  if (name === 'Performance') await revealDetails(page, 'Fixed traffic and time limits')
}

const stringValues = (value) => typeof value === 'string' ? [value]
  : value && typeof value === 'object' ? Object.values(value).flatMap(stringValues) : []
const setBackupBundle = (page) => page.getByLabel('Backup bundle').setInputFiles({
  name: 'synthetic-feature-backup.json',
  mimeType: 'application/json',
  buffer: Buffer.from('{"schemaVersion":1}'),
})

test.afterEach(async ({ page }) => {
  if (page.__featureCompleteModel) expect(page.__featureCompleteModel.issues).toEqual([])
})

test('composes the final navigation lazily and leaves settings out of the dashboard collector', async ({ page }) => {
  test.setTimeout(45_000)
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')

  await expect(page.locator('.section-nav button')).toHaveCount(8)
  expect(await page.locator('.section-nav button').allTextContents()).toEqual([
    'Overview', 'Nodes 1', 'Routing', 'DNS', 'Performance', 'Components / Updates', 'Backup & Restore', 'System / Panel',
  ])
  await page.waitForTimeout(5_300)
  expect(featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBeGreaterThan(1)
  for (const path of lazySettingsPaths) expect(featureCompleteRequests(model, path, 'GET')).toHaveLength(0)

  for (const [section, path] of [
    ['Routing', '/api/v1/appliance/policy'],
    ['DNS', '/api/v1/appliance/dns-observatory'],
    ['Performance', '/api/v1/performance/policy'],
    ['Components / Updates', '/api/v1/components'],
    ['System / Panel', '/api/v1/panel/listener'],
    ['Backup & Restore', null],
  ]) {
    await openSection(page, section)
    if (path) await expect.poll(() => featureCompleteRequests(model, path, 'GET').length).toBe(1)
  }
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/components/policy', 'GET').length).toBe(1)
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/update', 'GET').length).toBe(1)

  const settingsReads = Object.fromEntries(lazySettingsPaths.map((path) => [path, featureCompleteRequests(model, path, 'GET').length]))
  const statusReads = featureCompleteRequests(model, '/api/v1/status', 'GET').length
  await page.waitForTimeout(5_300)
  expect(featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBeGreaterThan(statusReads)
  expect(Object.fromEntries(lazySettingsPaths.map((path) => [path, featureCompleteRequests(model, path, 'GET').length]))).toEqual(settingsReads)
})

test('keeps Routing, DNS and Performance semantic Preview and one-shot Apply paths separate', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')

  await openSection(page, 'Routing')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'No effective changes', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply routing changes' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Cancel Preview' }).click()
  expect(model.writes).toHaveLength(0)

  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Feature route')
  await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Review routing changes', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toContainText('Feature route')
  await page.getByRole('button', { name: 'Apply routing changes', exact: true }).click()
  await expect(page.getByTestId('routing-result')).toContainText('Routing changes applied')

  await openSection(page, 'DNS')
  await page.getByLabel('Parallel queries').uncheck()
  await page.getByRole('button', { name: 'Preview DNS changes' }).click()
  await expect(page.getByRole('heading', { name: 'Review DNS and Observatory changes', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Apply DNS changes', exact: true }).click()
  await expect(page.getByTestId('dns-result')).toContainText('changes applied')

  await openSection(page, 'Performance')
  await page.getByLabel('Active probe interval').fill('120')
  await page.getByRole('button', { name: 'Preview performance changes' }).click()
  await expect(page.getByRole('heading', { name: 'Review performance policy changes', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Performance policy Preview' })).toContainText('60 → 120')
  await page.getByRole('button', { name: 'Apply performance policy', exact: true }).click()
  await expect(page.getByTestId('performance-policy-result')).toContainText('Performance policy applied')

  const applies = model.requests.filter((request) => request.method === 'POST' && /\/api\/v1\/(appliance\/policy|appliance\/dns-observatory|performance\/policy)\/apply$/.test(request.path))
  expect(applies.map(({ path, body }) => [path, Object.keys(body).sort()])).toEqual([
    ['/api/v1/appliance/policy/apply', ['previewToken']],
    ['/api/v1/appliance/dns-observatory/apply', ['previewToken']],
    ['/api/v1/performance/policy/apply', ['previewToken']],
  ])
  expect(model.writes.map(({ owner }) => owner)).toEqual(['routing', 'dns', 'performance'])
  expect(featureCompleteRequests(model, '/api/v1/appliance/policy/apply', 'POST')).toHaveLength(1)
})

for (const owner of ['routing', 'dns']) {
  test(`keeps the ${owner === 'routing' ? 'Routing' : 'DNS'} unknown-outcome gate shared only with its appliance-policy peer`, async ({ page }) => {
    const model = await mountFeatureCompleteDashboard(page)
    page.__featureCompleteModel = model
    await page.goto('/')

    const ownerPath = owner === 'routing' ? '/api/v1/appliance/policy' : '/api/v1/appliance/dns-observatory'
    const ownerApplyPath = `${ownerPath}/apply`
    await openSection(page, owner === 'routing' ? 'Routing' : 'DNS')
    if (owner === 'routing') {
      await page.getByRole('button', { name: 'Add rule', exact: true }).click()
      await page.getByLabel('Rule 1 display name').fill('Unknown route')
      await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
    } else {
      await page.getByLabel('Parallel queries').uncheck()
      await page.getByRole('button', { name: 'Preview DNS changes' }).click()
    }

    model.failNextApply.add(owner)
    model.failReadCounts.set(ownerPath, 5)
    await page.getByRole('button', { name: owner === 'routing' ? 'Apply routing changes' : 'Apply DNS changes', exact: true }).click()
    await expect(page.getByTestId(owner === 'routing' ? 'routing-result' : 'dns-result')).toContainText('outcome is unknown')
    await expect.poll(() => featureCompleteRequests(model, ownerApplyPath, 'POST').length).toBe(1)
    await expect.poll(() => featureCompleteRequests(model, ownerPath, 'GET').length).toBeGreaterThanOrEqual(2)

    const peer = owner === 'routing' ? 'DNS' : 'Routing'
    await openSection(page, peer)
    await expect(page.getByRole('button', { name: owner === 'routing' ? 'Preview DNS changes' : 'Preview changes', exact: true })).toBeDisabled()

    await openSection(page, 'Performance')
    await expect(page.getByRole('button', { name: 'Preview performance changes' })).toBeEnabled()

    const readsBeforeRecovery = featureCompleteRequests(model, ownerPath, 'GET').length
    model.failReadCounts.delete(ownerPath)
    await openSection(page, owner === 'routing' ? 'Routing' : 'DNS')
    await expect.poll(() => featureCompleteRequests(model, ownerPath, 'GET').length).toBeGreaterThan(readsBeforeRecovery)
    await expect(page.getByRole('button', { name: owner === 'routing' ? 'Preview changes' : 'Preview DNS changes', exact: true })).toBeEnabled()
    await openSection(page, peer)
    await expect(page.getByRole('button', { name: owner === 'routing' ? 'Preview DNS changes' : 'Preview changes', exact: true })).toBeEnabled()
    expect(featureCompleteRequests(model, ownerApplyPath, 'POST')).toHaveLength(1)
  })
}

test('preserves dirty drafts through unrelated reads, navigation and dashboard telemetry refresh', async ({ page }) => {
  test.setTimeout(35_000)
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')

  await openSection(page, 'Routing')
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByLabel('Rule 1 display name').fill('Preserved routing draft')
  await openSection(page, 'DNS')
  await page.getByLabel('Parallel queries').uncheck()
  await openSection(page, 'Performance')
  await page.getByLabel('Active probe interval').fill('120')
  await openSection(page, 'Components / Updates')
  await expect(page.getByText('Xray', { exact: true })).toBeVisible()
  await openSection(page, 'System / Panel')
  await expect(page.getByRole('heading', { name: '0.2.0', exact: true })).toBeVisible()
  await openSection(page, 'Backup & Restore')

  const readsBefore = Object.fromEntries(lazySettingsPaths.slice(0, 3).map((path) => [path, featureCompleteRequests(model, path, 'GET').length]))
  const telemetryBefore = featureCompleteRequests(model, '/api/v1/performance', 'GET').length
  await page.waitForTimeout(5_300)
  expect(featureCompleteRequests(model, '/api/v1/performance', 'GET').length).toBeGreaterThan(telemetryBefore)
  expect(Object.fromEntries(lazySettingsPaths.slice(0, 3).map((path) => [path, featureCompleteRequests(model, path, 'GET').length]))).toEqual(readsBefore)

  await openSection(page, 'Routing')
  await expect(page.getByLabel('Rule 1 display name')).toHaveValue('Preserved routing draft')
  await expect(page.getByText('Unsaved', { exact: true })).toBeVisible()
  await openSection(page, 'DNS')
  await expect(page.getByLabel('Parallel queries')).not.toBeChecked()
  await expect(page.getByText('Unsaved', { exact: true })).toBeVisible()
  await openSection(page, 'Performance')
  await expect(page.getByLabel('Active probe interval')).toHaveValue('120')
  await expect(page.getByText('Unsaved', { exact: true })).toBeVisible()
  for (const [path, count] of Object.entries(readsBefore)) expect(featureCompleteRequests(model, path, 'GET')).toHaveLength(count)
})

test('gates new mutation initiation across all workspaces when lifecycle is blocked or unknown', async ({ page }) => {
  test.setTimeout(60_000)
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')

  await openSection(page, 'System / Panel')
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Apply checked release' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Rollback retained release' })).toBeEnabled()
  await openSection(page, 'Nodes')
  await page.getByLabel('Select Feature test node').check()
  await expect(page.getByRole('button', { name: 'Enable', exact: true })).toBeEnabled()
  await openSection(page, 'Routing')
  await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeEnabled()
  await openSection(page, 'DNS')
  await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeEnabled()
  await openSection(page, 'Performance')
  await expect(page.getByRole('button', { name: 'Preview performance changes' })).toBeEnabled()
  await openSection(page, 'Components / Updates')
  await page.getByLabel('Component policy mode').selectOption('notify')
  await expect(page.getByRole('button', { name: 'Save policy' })).toBeEnabled()
  await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Check stable' })).toBeEnabled()
  await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })).toBeEnabled()
  await openSection(page, 'Backup & Restore')
  await setBackupBundle(page)
  await expect(page.getByRole('button', { name: 'Preview restore' })).toBeEnabled()

  for (const [label, lifecycle] of [
    ['maintenance', { maintenance: true, applying: false }],
    ['applying', { maintenance: false, applying: true }],
    ['unavailable', null],
    ['partial lifecycle', { maintenance: false }],
  ]) {
    model.lifecycle = lifecycle
    await openSection(page, 'Nodes')
    const previousStatusReads = featureCompleteRequests(model, '/api/v1/status', 'GET').length
    await page.getByRole('button', { name: 'Refresh dashboard' }).click()
    // The existing five-second status poll may overlap the explicit refresh.
    await expect.poll(() => featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBeGreaterThan(previousStatusReads)

    await page.getByLabel('Select Feature test node').check()
    await expect(page.getByRole('button', { name: 'Enable', exact: true })).toBeDisabled()
    await expect(page.getByText('Feature test node', { exact: true })).toBeVisible()
    await openSection(page, 'Routing')
    await expect(page.getByRole('button', { name: 'Preview changes', exact: true })).toBeDisabled()
    await expect(page.getByText('Policy boundary', { exact: true })).toBeVisible()
    await openSection(page, 'DNS')
    await expect(page.getByRole('button', { name: 'Preview DNS changes' })).toBeDisabled()
    await expect(page.getByText('Proxy resolver 1', { exact: true })).toBeVisible()
    await openSection(page, 'Performance')
    await expect(page.getByRole('button', { name: 'Preview performance changes' })).toBeDisabled()
    await expect(page.getByText('Fixed traffic and time envelope', { exact: true })).toBeVisible()
    await openSection(page, 'Components / Updates')
    await expect(page.getByLabel('Component policy mode')).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Save policy' })).toBeDisabled()
    await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Check stable' })).toBeEnabled()
    await expect(page.locator('[data-component="xray"]').getByRole('button', { name: 'Preview update' })).toBeDisabled()
    await expect(page.locator('[data-component="xray"]')).toBeVisible()
    await openSection(page, 'Backup & Restore')
    await expect(page.getByRole('button', { name: 'Preview restore' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Download safe backup' })).toBeEnabled()
    await openSection(page, 'System / Panel')
    await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Apply checked release' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Rollback retained release' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Check fixed release' })).toBeEnabled()
    await expect(page.getByRole('heading', { name: '0.2.0', exact: true })).toBeVisible()
    if (label !== 'unavailable') {
      await page.getByRole('button', { name: 'Check fixed release' }).click()
      await expect(page.getByText('Explicit release Check completed')).toBeVisible()
      await expect(page.getByRole('button', { name: 'Apply checked release' })).toBeDisabled()
      await expect(page.getByRole('button', { name: 'Rollback retained release' })).toBeDisabled()
    }
  }

  expect(model.requests.filter(({ path, method }) => method === 'POST' && /\/(preview|apply|rollback)$/.test(path))).toHaveLength(0)
  expect(model.writes).toHaveLength(0)
})

for (const [label, lifecycle] of [
  ['missing', null],
  ['missing applying flag', { maintenance: false }],
  ['missing maintenance flag', { applying: false }],
]) {
  test(`blocks an enabled node's Full speed test with ${label} lifecycle`, async ({ page }) => {
    const model = await mountFeatureCompleteDashboard(page, { nodeEnabled: true })
    page.__featureCompleteModel = model
    await page.goto('/')
    await openSection(page, 'Nodes')
    await page.getByLabel('Select Feature test node').check()
    const speedTest = page.getByRole('button', { name: 'Full speed test', exact: true })
    await expect(speedTest).toBeEnabled()

    model.lifecycle = lifecycle
    const previousStatusReads = featureCompleteRequests(model, '/api/v1/status', 'GET').length
    await page.getByRole('button', { name: 'Refresh dashboard' }).click()
    await expect.poll(() => featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBe(previousStatusReads + 1)
    await expect(speedTest).toBeDisabled()
    // Even a synthetic click on this disabled action must emit no mutation.
    await speedTest.evaluate((button) => button.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await expect(page.getByText('Feature test node', { exact: true })).toBeVisible()
    expect(featureCompleteRequests(model, '/api/v1/performance/manual-node', 'POST')).toHaveLength(0)
    expect(model.writes).toHaveLength(0)
  })
}

test('clears cross-domain previews on session turnover and rejects old tokens after login and password reset', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')
  const applyPathByOwner = {
    routing: '/api/v1/appliance/policy/apply',
    dns: '/api/v1/appliance/dns-observatory/apply',
    performance: '/api/v1/performance/policy/apply',
    listener: '/api/v1/panel/listener/apply',
  }
  const scenarios = [
    { owner: 'routing', section: 'Routing', preview: async () => {
      await page.getByRole('button', { name: 'Add rule', exact: true }).click()
      await page.getByLabel('Rule 1 display name').fill('Old session draft')
      await page.getByRole('button', { name: 'Preview changes', exact: true }).click()
      await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toBeVisible()
    }, cleared: async () => {
      await expect(page.getByRole('region', { name: 'Routing Preview confirmation' })).toHaveCount(0)
      await expect(page.getByLabel('Rule 1 display name')).toHaveCount(0)
    } },
    { owner: 'dns', section: 'DNS', preview: async () => {
      await page.getByLabel('Parallel queries').uncheck()
      await page.getByRole('button', { name: 'Preview DNS changes' }).click()
      await expect(page.getByRole('heading', { name: 'Review DNS and Observatory changes', exact: true })).toBeVisible()
    }, cleared: async () => {
      await expect(page.getByRole('heading', { name: 'Review DNS and Observatory changes', exact: true })).toHaveCount(0)
      await expect(page.getByLabel('Parallel queries')).toBeChecked()
    } },
    { owner: 'performance', section: 'Performance', preview: async () => {
      await page.getByLabel('Active probe interval').fill('120')
      await page.getByRole('button', { name: 'Preview performance changes' }).click()
      await expect(page.getByRole('heading', { name: 'Review performance policy changes', exact: true })).toBeVisible()
    }, cleared: async () => {
      await expect(page.getByRole('heading', { name: 'Review performance policy changes', exact: true })).toHaveCount(0)
      await expect(page.getByLabel('Active probe interval')).toHaveValue('60')
    } },
    { owner: 'listener', section: 'System / Panel', preview: async () => {
      await page.getByLabel('New management host').selectOption('10.0.0.4')
      await page.getByRole('button', { name: 'Preview rebind' }).click()
      await expect(page.getByRole('heading', { name: 'Review management listener rebind' })).toBeVisible()
    }, cleared: async () => {
      await expect(page.getByRole('heading', { name: 'Review management listener rebind' })).toHaveCount(0)
      await expect(page.getByLabel('New management host')).toHaveValue('127.0.0.1')
    } },
  ]

  for (const scenario of scenarios) {
    await openSection(page, scenario.section)
    await scenario.preview()
    const token = [...model.previewTokens.keys()][0]
    expect(model.previewTokens.get(token)?.owner).toBe(scenario.owner)
    await page.getByRole('button', { name: 'Sign out', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'XKeen Control' })).toBeVisible()
    expect(model.invalidatedTokens).toContain(token)
    await page.getByLabel('Panel password').fill('synthetic-login-password')
    await page.getByRole('button', { name: 'Sign in', exact: true }).click()
    await openSection(page, scenario.section)
    await scenario.cleared()

    const path = applyPathByOwner[scenario.owner]
    const staleApplyStatus = await page.evaluate(async ({ token: previewToken, path: applyPath, csrfToken }) => {
      const response = await fetch(applyPath, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify({ previewToken }),
      })
      return response.status
    }, { token, path, csrfToken: model.csrfToken })
    expect(staleApplyStatus).toBe(409)
    expect(featureCompleteRequests(model, path, 'POST')).toHaveLength(1)
  }

  expect(model.writes).toHaveLength(0)

  await openSection(page, 'System / Panel')
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  const passwordResetToken = [...model.previewTokens.keys()][0]
  await page.getByLabel('New panel password').fill('synthetic-new-password')
  await page.getByLabel('Confirm new password').fill('synthetic-new-password')
  await page.getByRole('button', { name: 'Replace password', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'XKeen Control' })).toBeVisible()
  expect(model.invalidatedTokens).toContain(passwordResetToken)
  expect(featureCompleteRequests(model, '/api/v1/session/password', 'POST').map(({ body }) => body)).toEqual([{ newPassword: 'synthetic-new-password' }])
  const passwordResetStaleApplyStatus = await page.evaluate(async ({ token, csrfToken }) => {
    const response = await fetch('/api/v1/panel/listener/apply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify({ previewToken: token }),
    })
    return response.status
  }, { token: passwordResetToken, csrfToken: model.csrfToken })
  expect(passwordResetStaleApplyStatus).toBe(409)
  expect(model.writes).toHaveLength(0)
  expect(featureCompleteRequests(model, '/api/v1/panel/listener/apply', 'POST')).toHaveLength(2)
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })
})

test('preserves System listener handoff semantics across same-session refresh and reconnect', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')
  await openSection(page, 'System / Panel')
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await page.getByRole('button', { name: 'Preview rebind' }).click()
  await expect(page.getByRole('heading', { name: 'Review management listener rebind' })).toBeVisible()
  await page.getByRole('button', { name: 'Start rebind handoff' }).click()
  await expect(page.getByText('Listener rebind handoff started', { exact: true })).toBeVisible()
  await expect(page.locator('p.system-blocked').filter({ hasText: /same-session Refresh cannot prove completion/i })).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/panel/listener/apply', 'POST')).toHaveLength(1)
  expect(featureCompleteRequests(model, '/api/v1/panel/listener/cancel', 'POST')).toHaveLength(0)

  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('heading', { name: '127.0.0.1:8787', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
  expect(featureCompleteRequests(model, '/api/v1/panel/listener/apply', 'POST')).toHaveLength(1)

  model.reconnect = true
  await page.reload()
  await openSection(page, 'System / Panel')
  await expect(page.getByRole('heading', { name: '10.0.0.4:8787', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
  expect(featureCompleteRequests(model, '/api/v1/panel/listener/apply', 'POST')).toHaveLength(1)
  expect(featureCompleteRequests(model, '/api/v1/panel/listener/preview', 'POST')).toHaveLength(1)
})

test('keeps safe projections secretless, browser storage empty, and the Dashboard usable at desktop and mobile widths', async ({ page }, testInfo) => {
  test.setTimeout(35_000)
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  const internalValues = stringValues({ nodes: model.nodes, subscriptions: model.subscriptions, dns: model.dns, runtime: model.runtime, performance: model.performance, auth: model.auth })
  // Prove the private material exists in the very sources used by safe GETs.
  for (const sentinel of PRIVATE_SENTINELS) expect(internalValues.join('\n')).toContain(sentinel)
  await page.goto('/')
  const renderedSections = [await page.locator('body').innerText(), await page.locator('body').evaluate((body) => body.outerHTML)]
  for (const [section, ready] of [
    ['Nodes', page.getByText('Feature test node', { exact: true })],
    ['Routing', page.getByText('Policy boundary', { exact: true })],
    ['DNS', page.getByText('Proxy resolver 1', { exact: true })],
    ['Performance', page.getByText('Fixed traffic and time envelope', { exact: true })],
    ['Components / Updates', page.getByText('Xray', { exact: true })],
    ['Backup & Restore', page.getByLabel('Backup bundle')],
    ['System / Panel', page.getByRole('heading', { name: '0.2.0', exact: true })],
  ]) {
    await openSection(page, section)
    await expect(ready).toBeVisible()
    renderedSections.push(await page.locator('body').innerText(), await page.locator('body').evaluate((body) => body.outerHTML))
  }
  await expect(page.getByRole('heading', { name: '0.2.0', exact: true })).toBeVisible()

  for (const path of ['/api/v1/session', '/api/v1/status', '/api/v1/nodes', '/api/v1/performance', ...lazySettingsPaths]) {
    expect(featureCompleteRequests(model, path, 'GET').length).toBeGreaterThan(0)
  }
  const projections = [...model.safeProjectionBodies, ...model.safeProjectionBodies.flatMap((body) => stringValues(JSON.parse(body)))].join('\n')
  const rendered = renderedSections.join('\n')
  for (const sentinel of PRIVATE_SENTINELS) {
    expect(projections).not.toContain(sentinel)
    expect(rendered).not.toContain(sentinel)
  }
  expect(await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length }))).toEqual({ local: 0, session: 0 })

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.screenshot({ path: testInfo.outputPath('feature-complete-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('heading', { name: '0.2.0', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Check fixed release' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('feature-complete-mobile.png'), fullPage: true })
  const dimensions = await page.evaluate(() => ({ viewport: window.innerWidth, document: document.documentElement.scrollWidth }))
  expect(dimensions.document).toBeLessThanOrEqual(dimensions.viewport + 1)
})
