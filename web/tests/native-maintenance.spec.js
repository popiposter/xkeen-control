import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard, featureCompleteRequests } from './fixtures/feature-complete-model.js'
import { revealNavigation } from './fixtures/disclosures.js'

const box = (page, name) => page.getByRole('checkbox', { name, exact: true })

async function open(page) {
  await page.goto('/')
  await (await revealNavigation(page)).getByRole('button', { name: 'Performance', exact: true }).click()
  await expect(page.getByText('Nightly maintenance', { exact: true })).toBeVisible()
}

test('maintenance settings load defaults, validate and save the exact field set', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  await open(page)
  const save = page.getByRole('button', { name: 'Save settings', exact: true })
  await expect(page.getByLabel('Start time (router time)')).toHaveValue('04:00')
  await expect(page.getByLabel('Nodes to speed-test')).toHaveValue('18')
  await expect(page.getByLabel('Pool size')).toHaveValue('6')
  await expect(save).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Reset to defaults', exact: true })).toBeDisabled()

  await page.getByLabel('Pool size').fill('7')
  await expect(page.getByText('The pool holds between 2 and 6 nodes.', { exact: true })).toBeVisible()
  await expect(save).toBeDisabled()
  await page.getByLabel('Pool size').fill('4')
  await page.getByLabel('Nodes to speed-test').fill('3')
  await expect(page.getByText('Speed-test at least as many nodes as the pool holds.', { exact: true })).toBeVisible()
  await page.getByLabel('Nodes to speed-test').fill('12')
  await page.getByLabel('Start time (router time)').fill('03:30')
  await box(page, 'Update geodata').click()
  await box(page, 'Apply the best pool automatically').click()
  await expect(save).toBeEnabled()
  await save.click()
  await expect(page.getByText('Settings saved. The next window is planned from them.', { exact: true })).toBeVisible()
  expect(model.maintenanceSaves).toEqual([{ enabled: true, window: '03:30', refreshSubscriptions: true, review: true, updateGeodata: false, autoApply: false, speedCandidates: 12, poolSize: 4, recovery: true, report: true }])
  await expect(save).toBeDisabled()

  await page.getByRole('button', { name: 'Reset to defaults', exact: true }).click()
  await expect(page.getByLabel('Pool size')).toHaveValue('6')
  await expect(save).toBeEnabled()
  expect(model.maintenanceSaves).toHaveLength(1)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
})

test('turning the window off disables its steps but keeps recovery and pool size editable', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  await open(page)
  await box(page, 'Run the nightly window').click()
  await expect(page.getByLabel('Start time (router time)')).toBeDisabled()
  await expect(page.getByLabel('Nodes to speed-test')).toBeDisabled()
  for (const label of ['Refresh subscriptions', 'Review nodes', 'Update geodata', 'Telegram report after the window']) {
    await expect(box(page, label)).toHaveAttribute('aria-disabled', 'true')
  }
  await expect(box(page, 'Availability recovery')).not.toHaveAttribute('aria-disabled', 'true')
  await expect(page.getByLabel('Pool size')).toBeEnabled()
  await page.getByRole('button', { name: 'Save settings', exact: true }).click()
  await expect(page.getByText('Settings saved. The next window is planned from them.', { exact: true })).toBeVisible()
  expect(model.maintenanceSaves[0].enabled).toBe(false)
})
