import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard, featureCompleteRequests } from './fixtures/feature-complete-model.js'
import { revealNavigation } from './fixtures/disclosures.js'

const complete = () => ({ state: 'completed', digest: 'a'.repeat(64), generation: 1, canStage: true, poolCount: 12, progress: { state: 'completed', candidates: [
  { tag: 'proxy-a', rttMs: 20, downloadBps: 1e6, uploadBps: 1e6, valid: true },
  { tag: 'proxy-b', rttMs: 60, downloadBps: 100e6, uploadBps: 10e6, valid: true },
] } })

async function open(page) {
  await page.goto('/')
  await (await revealNavigation(page)).getByRole('button', { name: 'Performance', exact: true }).click()
  await expect(page.getByText('Speed test', { exact: true })).toBeVisible()
}

test('compares and stages once without applying or writing a selection override', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  let starts = 0; let stages = 0
  await page.route('**/api/v1/performance/quality/start', async (route) => {
    starts++; model.quality = complete()
    await route.fulfill({ status: 202, json: { accepted: true } })
  })
  await page.route('**/api/v1/performance/quality/stage', async (route) => {
    stages++; expect(route.request().postDataJSON()).toEqual({ digest: 'a'.repeat(64) })
    model.quality = { ...complete(), state: 'consumed', canStage: false }
    await route.fulfill({ json: { digest: 'b'.repeat(64), restartRequired: true } })
  })
  await open(page)
  await page.getByRole('button', { name: 'Run speed test', exact: true }).click()
  await expect(page.getByText('800.0 Mbps', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Save recommendation', exact: true }).click()
  await expect(page.getByText('Routing configuration', { exact: true })).toBeVisible()
  expect(starts).toBe(1); expect(stages).toBe(1)
  expect(featureCompleteRequests(model, '/api/v1/selection/override', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/xkeen/config/apply', 'POST')).toEqual([])
})

test('ambiguous stage is read back without automatic retry or restart', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page); model.quality = complete()
  let stages = 0
  await page.route('**/api/v1/performance/quality/stage', async (route) => {
    stages++; model.quality = { ...complete(), state: 'consumed', canStage: false }; await route.abort('connectionfailed')
  })
  await open(page)
  await page.getByRole('button', { name: 'Save recommendation', exact: true }).click()
  await expect(page.getByText(/Action was not confirmed/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save recommendation', exact: true })).toBeDisabled()
  expect(stages).toBe(1)
  expect(featureCompleteRequests(model, '/api/v1/xkeen/config/apply', 'POST')).toEqual([])
})

test('idle quality view has no recurring comparison reads or bandwidth tests', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  await page.clock.install(); await open(page)
  await expect.poll(() => featureCompleteRequests(model, '/api/v1/performance/quality', 'GET').length).toBe(1)
  await page.clock.runFor(10_000)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality', 'GET')).toHaveLength(1)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
})
