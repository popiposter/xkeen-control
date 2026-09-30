import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard, featureCompleteRequests } from './fixtures/feature-complete-model.js'
import { revealDetails, revealNavigation } from './fixtures/disclosures.js'

async function openTask(page, name) {
  const navigation = await revealNavigation(page)
  await navigation.getByRole('button', { name, exact: true }).click()
}

test.afterEach(async ({ page }) => {
  if (page.__workspaceModel) expect(page.__workspaceModel.issues).toEqual([])
})

test('fits a useful page of a thousand nodes without rendering the whole registry', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const model = await mountFeatureCompleteDashboard(page, { nodeEnabled: true })
  page.__workspaceModel = model
  const seed = model.nodes[0]
  model.nodes = Array.from({ length: 1000 }, (_, index) => ({ ...seed, id: `node-${String(index + 1).padStart(8, '0')}`, outboundTag: `proxy-node-${String(index + 1).padStart(8, '0')}`, displayName: `Node ${String(index + 1).padStart(4, '0')}`, isEffective: index === 0 }))
  await page.goto('/')
  await openTask(page, 'Nodes 1000')
  await expect(page.locator('.nodes-table tbody tr')).toHaveCount(25)
  const dimensions = await page.locator('.nodes-table tbody tr').evaluateAll((rows) => rows.map((row) => ({ height: row.getBoundingClientRect().height, bottom: row.getBoundingClientRect().bottom })))
  expect(dimensions.every(({ height }) => height >= 28 && height <= 32)).toBe(true)
  expect(dimensions.filter(({ bottom }) => bottom <= 900).length).toBeGreaterThanOrEqual(20)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toHaveCount(0)
  await page.getByLabel('Select Node 0001', { exact: true }).check()
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toBeVisible()
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByLabel('Select Node 0026')).toBeVisible()
  await expect(page.getByTestId('selected-count')).toHaveText('1 selected')
  await page.getByRole('button', { name: 'Clear selection' }).click()
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: 'Node pages' }).getByRole('button')).toHaveCount(7)
})

test('a row edit uses the existing replacement Preview and clears the sensitive draft on cancel', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page, { nodeEnabled: true })
  page.__workspaceModel = model
  await page.goto('/')
  await openTask(page, 'Nodes 1')
  const name = model.nodes[0].displayName
  await page.getByRole('button', { name: `Edit ${name}`, exact: true }).click()
  await expect(page.getByLabel('Replacement VLESS profile')).toBeVisible()
  await expect(page.getByTestId('selected-count')).toHaveText('1 selected')
  await page.getByLabel('Replacement VLESS profile').fill('synthetic-private-draft')
  await page.locator('.selection-editor').getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('button', { name: `Edit ${name}`, exact: true }).click()
  await expect(page.getByLabel('Replacement VLESS profile')).toHaveValue('')
  expect(model.requests.some(({ method }) => method === 'POST')).toBe(false)
})

test('System starts with access, reveals optional forms and never fetches on disclosure clicks', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  page.__workspaceModel = model
  await page.route('**/api/v1/notifications', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ provider: 'telegram', configured: false, enabled: false, authorityState: 'unconfigured', deliveryState: 'idle' }) }))
  await page.goto('/')
  await openTask(page, 'System / Panel')
  await expect(page.getByLabel('New management host')).toBeVisible()
  await expect(page.getByLabel('New panel password')).toBeHidden()
  await expect(page.getByLabel('Telegram bot token')).toBeHidden()
  await expect(page.getByRole('button', { name: 'Check fixed release' })).toBeHidden()
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/update', 'GET').length).toBe(1)
  const reads = model.requests.filter(({ method }) => method === 'GET').length
  await revealDetails(page, 'Password')
  await expect(page.getByLabel('New panel password')).toBeVisible()
  await revealDetails(page, 'Panel releases')
  await expect(page.getByRole('button', { name: 'Check fixed release' })).toBeVisible()
  expect(model.requests.filter(({ method }) => method === 'GET').length).toBe(reads)
})

test('policy pages put editable work before protected context and expose the review sequence', async ({ page }) => {
  page.__workspaceModel = await mountFeatureCompleteDashboard(page)
  await page.goto('/')
  for (const [name, editor, details] of [['Routing', '.routing-editor', 'Protected routing policy'], ['DNS', '.dns-editor', 'Protected DNS and Observatory'], ['Performance', '.performance-policy-editor', 'Fixed traffic and time limits']]) {
    await openTask(page, name)
    await expect(page.locator(editor)).toBeVisible()
    await expect(page.getByRole('list', { name: 'Operation steps' })).toBeVisible()
    const positions = await page.evaluate(({ editor, details }) => {
      const target = [...document.querySelectorAll('details')].find((item) => item.querySelector('summary')?.textContent.includes(details))
      return { editor: document.querySelector(editor).getBoundingClientRect().top, context: target.getBoundingClientRect().top, closed: !target.open }
    }, { editor, details })
    expect(positions.context).toBeGreaterThan(positions.editor)
    expect(positions.closed).toBe(true)
  }
})

for (const width of [320, 375, 768]) test(`navigation and every workspace fit ${width}px without page overflow`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  page.__workspaceModel = await mountFeatureCompleteDashboard(page, { nodeEnabled: true })
  await page.goto('/')
  for (const name of ['Overview', 'Nodes 1', 'Routing', 'DNS', 'Performance', 'Components / Updates', 'Backup & Restore', 'System / Panel']) {
    await openTask(page, name)
    await expect(page.locator('.workspace')).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), name).toBe(true)
    if (width < 760) {
      const compactTargets = await page.locator('.workspace button').evaluateAll((buttons) => buttons
        .filter((button) => button.getClientRects().length > 0)
        .filter((button) => button.getBoundingClientRect().height < 43.5)
        .map((button) => button.getAttribute('aria-label') || button.textContent.trim()))
      expect(compactTargets, `${name} touch target heights`).toEqual([])
      const checkTargets = await page.locator('.workspace .selection-checkbox').evaluateAll((labels) => labels
        .filter((label) => label.getClientRects().length > 0)
        .map((label) => ({ width: label.getBoundingClientRect().width, height: label.getBoundingClientRect().height })))
      for (const target of checkTargets) {
        expect(target.width).toBeGreaterThanOrEqual(44)
        expect(target.height).toBeGreaterThanOrEqual(44)
      }
    }
  }
  if (!await page.getByRole('navigation', { name: 'Dashboard sections' }).isVisible()) await page.getByRole('button', { name: 'Toggle navigation' }).click()
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await revealDetails(page, 'Selection details')
  await page.locator('details > summary').filter({ hasText: 'Selection details' }).press('Enter')
  await expect(page.locator('details').filter({ hasText: 'Selection details' }).first()).not.toHaveAttribute('open')
})
