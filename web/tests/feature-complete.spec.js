import { revealDetails, revealSystemSettings, revealNavigation } from './fixtures/disclosures.js'
import { expect, test } from '@playwright/test'
import { featureCompleteRequests, mountFeatureCompleteDashboard, PRIVATE_SENTINELS } from './fixtures/feature-complete-model.js'

const lazySettingsPaths = [
  '/api/v1/xkeen/config/workspace',
  '/api/v1/notifications',
  '/api/v1/panel/listener',
  '/api/v1/update',
]

const openSection = async (page, name) => {
  await (await revealNavigation(page)).getByRole('button').filter({ hasText: name }).click()
  if (name === 'System / Panel') await revealSystemSettings(page)
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
  await page.clock.install()
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')

  await expect(page.getByRole('navigation', { name: 'Dashboard sections' }).getByRole('button')).toHaveCount(9)
  expect(await page.getByRole('navigation', { name: 'Dashboard sections' }).getByRole('button').evaluateAll((buttons) => buttons.map((button) => button.getAttribute('aria-label')))).toEqual([
    'Overview', 'Nodes 1', 'Routing', 'DNS', 'Configurations', 'Performance', 'Components / Updates', 'Backup & Restore', 'System / Panel',
  ])
  await page.clock.runFor(5_300)
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBeGreaterThan(1)
  for (const path of lazySettingsPaths) expect(featureCompleteRequests(model, path, 'GET')).toHaveLength(0)

  const openedPaths = new Set()
  for (const [section, path] of [
    ['Routing', '/api/v1/xkeen/config/workspace'],
    ['DNS', null],
    ['Performance', null],
    ['Components / Updates', null],
    ['System / Panel', '/api/v1/panel/listener'],
    ['Backup & Restore', null],
  ]) {
    await openSection(page, section)
    if (path) openedPaths.add(path)
    if (section === 'System / Panel') { openedPaths.add('/api/v1/update'); openedPaths.add('/api/v1/notifications') }
    for (const lazyPath of lazySettingsPaths) {
      await expect.poll(() => featureCompleteRequests(model, lazyPath, 'GET').length).toBe(openedPaths.has(lazyPath) ? 1 : 0)
    }
    // Check each mounted workspace too: an active-only poll would be invisible
    // if time advanced only before navigation and after leaving every editor.
    const beforeTick = featureCompleteRequests(model, '/api/v1/status', 'GET').length
    await page.clock.runFor(5_300)
    await expect.poll(() => featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBeGreaterThan(beforeTick)
    for (const lazyPath of lazySettingsPaths) {
      expect(featureCompleteRequests(model, lazyPath, 'GET')).toHaveLength(openedPaths.has(lazyPath) ? 1 : 0)
    }
  }
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/notifications', 'GET').length).toBe(1)
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/update', 'GET').length).toBe(1)

  const settingsReads = Object.fromEntries(lazySettingsPaths.map((path) => [path, featureCompleteRequests(model, path, 'GET').length]))
  const statusReads = featureCompleteRequests(model, '/api/v1/status', 'GET').length
  await page.clock.runFor(5_300)
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/status', 'GET').length).toBeGreaterThan(statusReads)
  expect(Object.fromEntries(lazySettingsPaths.map((path) => [path, featureCompleteRequests(model, path, 'GET').length]))).toEqual(settingsReads)
  expect(model.requests.filter(({ path }) => /^\/api\/v1\/(components|setup)(\/|$)/.test(path))).toEqual([])
})

test('gates new mutation initiation across all workspaces when lifecycle is blocked or unknown', async ({ page }) => {
  test.setTimeout(60_000)
  const model = await mountFeatureCompleteDashboard(page)
  page.__featureCompleteModel = model
  await page.goto('/')

  await openSection(page, 'System / Panel')
  await page.getByLabel('New management host').selectOption('10.0.0.4')
  await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeEnabled()
  await revealSystemSettings(page, 'Releases')
  await expect(page.getByRole('button', { name: 'Apply checked release' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Rollback retained release' })).toBeEnabled()
  await openSection(page, 'Nodes')
  await page.getByRole('checkbox', { name: 'Select Feature test node', exact: true }).check()
  await expect(page.getByRole('button', { name: 'Enable', exact: true })).toBeEnabled()
  await openSection(page, 'Performance')
  await expect(page.getByRole('button', { name: 'Compare nodes' })).toBeEnabled()
  await openSection(page, 'Components / Updates')
  await expect(page.getByRole('heading', { name: 'XKeen and components' })).toBeVisible()
  await openSection(page, 'Backup & Restore')
  await setBackupBundle(page)
  await page.getByLabel('Backup passphrase', { exact: true }).fill('synthetic transfer passphrase')
  await expect(page.getByRole('button', { name: 'Preview transfer' })).toBeEnabled()

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

    await page.getByRole('checkbox', { name: 'Select Feature test node', exact: true }).check()
    await expect(page.getByRole('button', { name: 'Enable', exact: true })).toBeDisabled()
    await expect(page.getByText('Feature test node', { exact: true })).toBeVisible()
    await openSection(page, 'Performance')
    await expect(page.getByRole('button', { name: 'Compare nodes' })).toBeDisabled()
    await expect(page.getByText('Node quality', { exact: true })).toBeVisible()
    await openSection(page, 'Components / Updates')
    await expect(page.getByRole('heading', { name: 'XKeen and components' })).toBeVisible()
    await openSection(page, 'Backup & Restore')
    await expect(page.getByRole('button', { name: 'Preview transfer' })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Download encrypted backup' })).toBeEnabled()
    await openSection(page, 'System / Panel')
    await expect(page.getByRole('button', { name: 'Preview rebind' })).toBeDisabled()
    await revealSystemSettings(page, 'Releases')
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
    await page.getByRole('checkbox', { name: 'Select Feature test node', exact: true }).check()
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
    listener: '/api/v1/panel/listener/apply',
  }
  const scenarios = [
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
  await expect(page.getByRole('region', { name: 'Listener rebind Preview', exact: true })).toBeVisible()
  const passwordResetToken = [...model.previewTokens.keys()][0]
  await page.getByRole('tab', { name: 'Password', exact: true }).click()
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
    ['Routing', page.getByText('Routing configuration', { exact: true })],
    ['DNS', page.getByText('DNS configuration', { exact: true })],
    ['Performance', page.getByText('Node quality', { exact: true })],
    ['Components / Updates', page.getByRole('heading', { name: 'XKeen and components', exact: true })],
    ['Backup & Restore', page.getByLabel('Backup bundle')],
    ['System / Panel', page.getByRole('heading', { name: '127.0.0.1:8787', exact: true })],
  ]) {
    await openSection(page, section)
    await expect(ready).toBeVisible()
    renderedSections.push(await page.locator('body').innerText(), await page.locator('body').evaluate((body) => body.outerHTML))
  }
  await revealSystemSettings(page, 'Releases')
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
