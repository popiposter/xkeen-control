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

test('constrained router shows actual ceilings and pressure without launching traffic', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), state: 'failed', canStage: false,
    resourceProfile: { name: 'constrained', constrained: true, automatic: false },
    limits: { candidates: 3, attempts: 4, bytes: 24 * 1048576, seconds: 90 },
    automaticReason: 'constrained-device', progress: { ...complete().progress, reasonCode: 'resource-pressure' } }
  await open(page)
  await expect(page.getByText(/3 successful nodes and 4 attempts, 24 MiB and 90 seconds including cleanup/)).toBeVisible()
  await expect(page.getByText(/Automatic speed comparisons are disabled on this constrained router/)).toBeVisible()
  await expect(page.getByRole('alert').filter({ hasText: 'router resources are busy' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Apply recommendation', exact: true })).toBeDisabled()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
})

test('constrained automatic-disabled status does not hide manual native cron refusal', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { state: 'idle', canStage: false, automaticReason: 'constrained-device', startReason: 'native-speed-conflict' }
  await open(page)
  await expect(page.getByText(/Automatic speed comparisons are disabled/)).toBeVisible()
  await expect(page.getByRole('alert').filter({ hasText: 'Inspect and quiesce' })).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
})

test('bounded automatic review separates full pool, coverage and applied state without starting traffic from the browser', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), resourceProfile: { name: 'constrained', constrained: true, automatic: true },
    limits: { candidates: 3, attempts: 4, bytes: 24 * 1048576, seconds: 90 },
    poolCount: 52, activePoolCount: 6, eligibleCount: 14, attemptedCount: 14, validCount: 12,
    batchCount: 5, aggregateBytes: 44 * 1048576, reviewTrigger: 'subscription-refresh',
    appliedState: 'applied', nextDueAt: '2026-10-10T12:00:00Z',
    quotaState: 'available', quotaUsedBytes: 144 * 1048576, quotaRemainingBytes: 144 * 1048576,
    quotaReviewsUsed: 1, quotaNextResetAt: '2026-10-11T01:00:00Z', manualAllowanceBytes: 24 * 1048576 }
  await open(page)
  await expect(page.getByText(/Current active pool: 6 nodes. Enabled nodes available for comparison: 52/)).toBeVisible()
  await expect(page.getByText(/14 of 14 eligible nodes attempted, 12 valid; 5 batches and 44.0 MiB transferred/)).toBeVisible()
  await expect(page.getByText(/Automatic pool application: applied/)).toBeVisible()
  await expect(page.getByText(/Automatic reviews test eligible nodes in small sequential batches/)).toBeVisible()
  await expect(page.getByText(/144 of 288 MiB reserved in the rolling 24 hours \(1 of 2 reviews\); 144 MiB remaining/)).toBeVisible()
  await expect(page.getByText(/Manual speed tests have a separate limit of 24 MiB per run/)).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('inspection-required automatic outcome fences browser testing and applying', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), inspectionRequired: true, reviewReason: 'inspection-required' }
  await open(page)
  await expect(page.getByText('Inspection required', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Run speed test', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Apply recommendation', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Verify and clear inspection block' })).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/inspect', 'POST')).toEqual([])
})

test('inspection action does not retry an uncertain application when verification refuses', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), inspectionRequired: true, reviewReason: 'inspection-required' }
  let inspections = 0
  await page.route('**/api/v1/performance/quality/inspect', async (route) => {
    inspections++
    await route.fulfill({ status: 409, json: { error: 'inspection incomplete' } })
  })
  await open(page)
  await page.getByRole('button', { name: 'Verify and clear inspection block' }).click()
  await expect(page.getByText(/Action was not confirmed/)).toBeVisible()
  await expect(page.getByText('Inspection required', { exact: true })).toBeVisible()
  expect(inspections).toBe(1)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('verified inspection clears the browser block without repeating Apply', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), state: 'failed', canStage: false, inspectionRequired: true, reviewReason: 'inspection-required' }
  let inspections = 0
  await page.route('**/api/v1/performance/quality/inspect', async (route) => {
    inspections++
    model.quality = { ...model.quality, inspectionRequired: false, reviewReason: 'inspection-settled' }
    await route.fulfill({ status: 200, json: model.quality })
  })
  await open(page)
  await page.getByRole('button', { name: 'Verify and clear inspection block' }).click()
  await expect(page.getByText('Inspection required', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Run speed test', exact: true })).toBeEnabled()
  expect(inspections).toBe(1)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('unavailable quota is shown as unknown rather than unused', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), resourceProfile: { name: 'constrained', constrained: true, automatic: true },
    quotaState: 'unavailable', manualAllowanceBytes: 24 * 1048576 }
  await open(page)
  await expect(page.getByText(/Automatic traffic quota: unavailable; inspect the private receipt before another comparison/)).toBeVisible()
  await expect(page.getByText(/0 of 288 MiB reserved/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Run speed test', exact: true })).toBeDisabled()
})

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
