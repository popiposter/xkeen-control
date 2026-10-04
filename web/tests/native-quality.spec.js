import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard, featureCompleteRequests } from './fixtures/feature-complete-model.js'
import { revealNavigation } from './fixtures/disclosures.js'

const complete = () => ({ state: 'completed', digest: 'a'.repeat(64), generation: 1, canStage: true, poolCount: 12, manualSample: true, latencyLimitMs: 300, eligibleCount: 12, ranking: [{tag:'proxy-b',rank:1},{tag:'proxy-a',rank:2}], progress: { state: 'completed', validCount:2, candidates: [
  { tag: 'proxy-a', rttMs: 20, downloadBps: 1e6, uploadBps: 1e6, valid: true },
  { tag: 'proxy-b', rttMs: 60, downloadBps: 100e6, uploadBps: 10e6, valid: true },
] } })

async function open(page) {
  await page.goto('/')
  await (await revealNavigation(page)).getByRole('button', { name: 'Performance', exact: true }).click()
  await expect(page.getByText('Speed test', { exact: true })).toBeVisible()
}

test('changed subscription generation explains stale recommendation without applying or retesting', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), canStage: false, stageReason: 'configuration-changed' }
  await open(page)
  await expect(page.getByText('Subscriptions or configuration changed after this test. Run a fresh speed test before applying a recommendation.', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply recommendation', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Run speed test', exact: true })).toBeEnabled()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('speed test applies one recommendation and waits for verified configuration instead of treating 202 as success', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  let starts = 0; let applies = 0; let finished = false
  await page.route('**/api/v1/performance/quality/start', async (route) => {
    starts++; model.quality = complete()
    await route.fulfill({ status: 202, json: { accepted: true } })
  })
  const job = { id:'c'.repeat(32),action:'restart',state:'running',configurationState:'running',output:'',cursor:0,interactive:false }
  await page.route('**/api/v1/performance/quality/apply', async (route) => {
    applies++; expect(route.request().postDataJSON()).toEqual({ digest: 'a'.repeat(64) })
    model.quality = { ...complete(), state: 'consumed', canStage: false }
    await route.fulfill({ status:202, json:job })
  })
  await page.route('**/api/v1/xkeen/jobs/read', (route) => route.fulfill({json:finished ? {...job,state:'completed',exitCode:0,configurationState:'applied'} : job}))
  await open(page)
  await page.getByRole('button', { name: 'Run speed test', exact: true }).click()
  await expect(page.getByText('800.0 Mbps', { exact: true })).toBeVisible()
  await expect(page.getByText(/12 eligible nodes, latency threshold 300 ms/)).toBeVisible()
  await page.getByRole('button', { name: 'Apply recommendation', exact: true }).click()
  await expect(page.getByRole('status').filter({hasText:'waiting for configuration verification'})).toBeVisible()
  await expect(page.getByText('Recommendation applied and configuration verified.',{exact:true})).toHaveCount(0)
  finished=true
  await expect(page.getByText('Recommendation applied and configuration verified.',{exact:true})).toBeVisible()
  expect(starts).toBe(1); expect(applies).toBe(1)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/stage', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/selection/override', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/xkeen/config/apply', 'POST')).toEqual([])
})

test('ambiguous Apply is read back without automatic retry or second restart', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page); model.quality = complete()
  let applies = 0
  await page.route('**/api/v1/performance/quality/apply', async (route) => {
    applies++; model.quality = { ...complete(), state: 'consumed', canStage: false }; await route.abort('connectionfailed')
  })
  await open(page)
  await page.getByRole('button', { name: 'Apply recommendation', exact: true }).click()
  await expect(page.getByText(/Action was not confirmed/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply recommendation', exact: true })).toBeDisabled()
  await expect(page.getByRole('button',{name:'Inspect configuration',exact:true})).toBeVisible()
  expect(applies).toBe(1)
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
