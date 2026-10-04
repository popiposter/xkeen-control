import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard, featureCompleteRequests } from './fixtures/feature-complete-model.js'
import { revealDetails, revealNavigation, revealSystemSettings } from './fixtures/disclosures.js'

async function openTask(page, name) {
  const navigation = await revealNavigation(page)
  await navigation.getByRole('button', { name, exact: true }).click()
}

test.afterEach(async ({ page }) => {
  if (page.__workspaceModel) expect(page.__workspaceModel.issues).toEqual([])
})

for (const width of [320, 375]) test(`mobile navigation is a modal drawer with focus and dismissal at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 812 })
  page.__workspaceModel = await mountFeatureCompleteDashboard(page)
  await page.goto('/')
  const toggle = page.locator('button[aria-label="Toggle navigation"]')
  const drawer = page.getByRole('dialog', { name: 'Navigation menu', exact: true })
  await expect(toggle).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Dashboard sections' })).toHaveCount(0)
  await toggle.click()
  await expect(drawer).toBeVisible()
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('button', { name: 'Toggle navigation', exact: true })).toHaveCount(0)
  await expect(page.locator('body')).toHaveCSS('overflow', 'hidden')
  const close = drawer.getByRole('button', { name: 'Close navigation', exact: true })
  await expect(close).toBeFocused()
  await close.press('Shift+Tab')
  await expect(drawer.getByRole('button', { name: 'Sign out', exact: true })).toBeFocused()
  await drawer.getByRole('button', { name: 'Sign out', exact: true }).press('Tab')
  await expect(close).toBeFocused()
  const buttons = await drawer.getByRole('button').evaluateAll((elements) => elements.map((element) => ({ width: element.getBoundingClientRect().width, height: element.getBoundingClientRect().height })))
  for (const button of buttons) { expect(button.width).toBeGreaterThanOrEqual(43.5); expect(button.height).toBeGreaterThanOrEqual(43.5) }
  await close.press('Escape')
  await expect(drawer).not.toBeVisible()
  await expect(toggle).toBeFocused()
  await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  await expect(page.locator('body')).toHaveCSS('overflow', 'visible')
  await toggle.click()
  await page.mouse.click(width - 4, 200)
  await expect(drawer).not.toBeVisible()
  await expect(toggle).toBeFocused()
  await toggle.click()
  await drawer.getByRole('button', { name: /^Nodes/ }).click()
  await expect(drawer).not.toBeVisible()
  await expect(page.getByRole('heading', { name: /^Nodes/ })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width)
  await toggle.click()
  await close.click()
  await expect(drawer).not.toBeVisible()
  await toggle.click()
  await page.setViewportSize({ width: 1024, height: 812 })
  await expect(drawer).not.toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Dashboard sections' })).toBeVisible()
  await expect(page.locator('body')).toHaveCSS('overflow', 'visible')
  expect(featureCompleteRequests(page.__workspaceModel, '/api/v1/node-changes/apply')).toEqual([])
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
  expect(dimensions.every(({ height }) => height >= 28 && height <= 32), JSON.stringify(dimensions.slice(0, 2))).toBe(true)
  expect(dimensions.filter(({ bottom }) => bottom <= 900).length).toBeGreaterThanOrEqual(20)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toBeVisible()
  const before = await page.locator('.nodes-table').boundingBox()
  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toBeDisabled()
  await page.getByRole('checkbox', { name: 'Select Node 0001', exact: true }).check()
  expect((await page.locator('.nodes-table').boundingBox()).y).toBe(before.y)
  await expect(page.getByRole('button', { name: 'Delete', exact: true })).toBeEnabled()
  await expect(page.getByRole('columnheader', { name: 'Actions' })).toHaveCount(0)
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toBeVisible()
  await page.getByRole('button', { name: 'Next page' }).click()
  await expect(page.getByRole('checkbox', { name: 'Select Node 0026', exact: true })).toBeVisible()
  await expect(page.getByTestId('selected-count')).toHaveText('1 selected')
  await page.getByRole('button', { name: 'Clear selection' }).click()
  await expect(page.getByRole('toolbar', { name: 'Selected node actions' })).toBeVisible()
  await expect(page.getByRole('navigation', { name: 'Node pages' }).getByRole('button')).toHaveCount(7)
})

test('filters named subscriptions and selects rows by click or keyboard without moving the table', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page, { nodeEnabled: true })
  page.__workspaceModel = model
  const seed = model.nodes[0]
  model.nodes = ['Work', 'Travel', ''].map((name, index) => ({ ...seed, id: `node-${String(index + 1).padStart(8, '0')}`, outboundTag: `proxy-node-${String(index + 1).padStart(8, '0')}`, displayName: `Profile ${index + 1}`, sourceType: name ? 'subscription' : 'manual', subscriptionName: name }))
  await page.goto('/')
  await openTask(page, 'Nodes 3')
  await expect(page.getByRole('columnheader', { name: 'Subscription' })).toBeVisible()
  const first = page.locator('.nodes-table tbody tr').filter({ hasText: 'Profile 1' })
  const top = (await page.locator('.nodes-table').boundingBox()).y
  await first.getByText('Profile 1', { exact: true }).click()
  await expect(page.getByRole('checkbox', { name: 'Select Profile 1', exact: true })).toBeChecked()
  expect((await page.locator('.nodes-table').boundingBox()).y).toBe(top)
  await first.press('Space')
  await expect(page.getByRole('checkbox', { name: 'Select Profile 1', exact: true })).not.toBeChecked()
  await page.getByLabel('Filter by subscription').selectOption({ label: 'Travel' })
  await expect(page.locator('.nodes-table tbody tr')).toHaveCount(1)
  await expect(page.locator('.nodes-table tbody')).toContainText('Profile 2')
  await expect(page.locator('.nodes-table tbody')).not.toContainText('Profile 1')
  expect(model.requests.some(({ method }) => method === 'POST')).toBe(false)
})

test('a row edit uses the existing replacement Preview and clears the sensitive draft on cancel', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page, { nodeEnabled: true })
  page.__workspaceModel = model
  await page.goto('/')
  await openTask(page, 'Nodes 1')
  const name = model.nodes[0].displayName
  await page.getByRole('checkbox', { name: `Select ${name}`, exact: true }).check()
  await page.getByRole('button', { name: 'Edit / replace profile', exact: true }).click()
  await expect(page.getByLabel('Replacement VLESS profile')).toBeVisible()
  await expect(page.getByTestId('selected-count')).toHaveText('1 selected')
  await page.getByLabel('Replacement VLESS profile').fill('synthetic-private-draft')
  await page.locator('.selection-editor').getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('checkbox', { name: `Select ${name}`, exact: true }).check()
  await page.getByRole('button', { name: 'Edit / replace profile', exact: true }).click()
  await expect(page.getByLabel('Replacement VLESS profile')).toHaveValue('')
  expect(model.requests.some(({ method }) => method === 'POST')).toBe(false)
})

test('System starts with access, switches subpages without repeating discovery', async ({ page }) => {
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
  await revealSystemSettings(page, 'Password')
  await expect(page.getByLabel('New panel password')).toBeVisible()
  await revealSystemSettings(page, 'Releases')
  await expect(page.getByRole('button', { name: 'Check fixed release' })).toBeVisible()
  expect(model.requests.filter(({ method }) => method === 'GET').length).toBe(reads)
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
        expect(target.width).toBeGreaterThanOrEqual(43.5)
        expect(target.height).toBeGreaterThanOrEqual(43.5)
      }
    }
  }
  if (!await page.getByRole('navigation', { name: 'Dashboard sections' }).isVisible()) await page.getByRole('button', { name: 'Toggle navigation' }).click()
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await revealDetails(page, 'Selection details')
  await page.locator('details > summary').filter({ hasText: 'Selection details' }).press('Enter')
  await expect(page.locator('details').filter({ hasText: 'Selection details' }).first()).not.toHaveAttribute('open')
})
