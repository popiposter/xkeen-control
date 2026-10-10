import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard, featureCompleteRequests } from './fixtures/feature-complete-model.js'
import { revealDetails, revealNavigation } from './fixtures/disclosures.js'

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
  await revealDetails(page, 'How reviews work')
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

test('deferred review explains a zero-traffic latency pre-phase refusal and next due', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), state: 'deferred', canStage: false, appliedState: 'not-attempted',
    progress: { state: 'idle', candidates: [] }, ranking: [], selectedForSpeed: 0,
    attemptedCount: 0, validCount: 0, batchCount: 0, aggregateBytes: 0,
    resourceProfile: { name: 'constrained', constrained: true, automatic: true },
    limits: { candidates: 3, attempts: 4, bytes: 24 * 1048576, seconds: 90 },
    reviewReason: 'rtt-candidates-insufficient', nextDueAt: '2026-10-10T12:00:00Z',
    aggregateBytes: 0, quotaState: 'available', quotaUsedBytes: 0,
    quotaRemainingBytes: 288 * 1048576, quotaReviewsUsed: 0 }
  await open(page)
  await expect(page.getByRole('status').filter({ hasText: 'No speed test ran and no quota was used' })).toBeVisible()
  await expect(page.getByRole('status').filter({ hasText: 'deferred before measuring node speeds' })).toBeVisible()
  await expect(page.getByText(/Automatic review: 18 of 18 selected nodes attempted/)).toHaveCount(0)
  await expect(page.getByText(/Automatic pool application: applied/)).toHaveCount(0)
  await expect(page.getByText('Next review', { exact: true })).toBeVisible()
  await expect(page.getByText('Deferred', { exact: true })).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
})

test('an orphaned pool member is reported as replaceable, not as an unavailable pool', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), state: 'idle', canStage: false, activePoolCount: 5, activePoolState: 'degraded-orphaned',
    activePool: ['proxy-node-00000001'], orphanedPool: ['proxy-node-gone'] }
  await open(page)
  await expect(page.getByRole('status').filter({ hasText: 'One pool member no longer exists' })).toBeVisible()
  await expect(page.getByText('Active pool readback unavailable')).toHaveCount(0)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
})

test('availability recovery and a provisional pool are labelled apart from a measured pool', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), state: 'idle', canStage: false, ranking: [], progress: { state: 'idle', candidates: [] },
    provisionalAt: '2026-10-10T09:00:00Z',
    recovery: { state: 'applied', reason: 'no-healthy-member', checkedAt: '2026-10-10T09:00:00Z', probed: 12, verified: 4, pool: ['proxy-a', 'proxy-b', 'proxy-c', 'proxy-d'] } }
  await open(page)
  await expect(page.getByRole('status').filter({ hasText: /Provisional pool since/ })).toContainText('not by speed')
  await expect(page.getByRole('status').filter({ hasText: /applied a provisional pool of 4 nodes/ })).toContainText('4 of 12 answered')
  model.quality = { ...model.quality, provisionalAt: undefined, recovery: { state: 'vpn-unavailable', reason: 'no-candidate-verified', probed: 12, verified: 0 } }
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: /none of the 12 probed nodes answered/ })).toContainText('rather than going direct')
  await expect(page.getByText(/Provisional pool since/)).toHaveCount(0)
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('bounded automatic review separates full pool, coverage and applied state without starting traffic from the browser', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), resourceProfile: { name: 'constrained', constrained: true, automatic: true },
    limits: { candidates: 3, attempts: 4, bytes: 24 * 1048576, seconds: 90 },
    poolCount: 52, activePoolCount: 6, eligibleCount: 14, attemptedCount: 14, validCount: 12,
    manualSample: false, batchCount: 5, aggregateBytes: 44 * 1048576, reviewTrigger: 'subscription-refresh',
    appliedState: 'applied', nextDueAt: '2026-10-10T12:00:00Z',
    quotaState: 'available', quotaUsedBytes: 72 * 1048576, quotaRemainingBytes: 0,
    quotaReviewsUsed: 1, quotaNextResetAt: '2026-10-11T01:00:00Z', manualAllowanceBytes: 72 * 1048576 }
  await open(page)
  await expect(page.getByText('6 nodes', { exact: true })).toBeVisible()
  await expect(page.getByText(/Verified · 52 enabled/)).toBeVisible()
  await expect(page.getByText('12 of 14 valid', { exact: true })).toBeVisible()
  await expect(page.getByText(/44\.0 MiB · subscription-refresh · .*applied/)).toBeVisible()
  await expect(page.getByText(/At most one automatic review per 24 hours/)).toBeVisible()
  await expect(page.getByText('72 of 72 MiB', { exact: true })).toBeVisible()
  await expect(page.getByText(/1 of 1 automatic review in 24 h/)).toBeVisible()
  await revealDetails(page, 'How reviews work')
  await expect(page.getByText(/within 72 MiB, the same limit as one manual speed test/)).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/start', 'POST')).toEqual([])
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('large eligible set shows bounded subset, deferred nodes and no-op without claiming a global winner', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), resourceProfile: { name: 'constrained', constrained: true, automatic: true },
    limits: { candidates: 3, attempts: 4, bytes: 24 * 1048576, seconds: 90 },
    poolCount: 52, activePoolCount: 6, eligibleCount: 46, totalEligible: 46,
    selectedForSpeed: 18, deferredForFutureReview: 28, subsetState: 'subset-complete',
    fairCursorState: 'continued', attemptedCount: 18, validCount: 15, batchCount: 6,
    aggregateBytes: 48 * 1048576, appliedState: 'no-op', poolDecision: 'pool-unchanged',
    reviewReason: 'pool-unchanged', activePool: ['proxy-a', 'proxy-b'],
    recommendedPool: ['proxy-b', 'proxy-a'], nativeSelected: 'proxy-a',
    quotaState: 'available', quotaUsedBytes: 144 * 1048576,
    quotaRemainingBytes: 144 * 1048576, quotaReviewsUsed: 1,
    manualAllowanceBytes: 24 * 1048576 }
  await open(page)
  await expect(page.getByText('15 of 18 valid', { exact: true })).toBeVisible()
  await revealDetails(page, 'How reviews work')
  await expect(page.getByText(/Last scope: 18 of 46 eligible nodes, 28 deferred/)).toBeVisible()
  await expect(page.getByText(/bounded subset is not a global ranking/)).toBeVisible()
  await expect(page.getByText(/same pool members; no restart/)).toBeVisible()
  expect(featureCompleteRequests(model, '/api/v1/performance/quality/apply', 'POST')).toEqual([])
})

test('unavailable native readback does not present cached pool or selected node as current', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), activePoolCount: 6, activePool: [], activePoolState: 'unavailable',
    nativeSelectedState: 'unavailable' }
  await open(page)
  await expect(page.getByText(/Active pool readback unavailable/)).toBeVisible()
  await expect(page.getByText(/Current active pool: 6 nodes/)).toHaveCount(0)
  await expect(page.getByText(/Last observed native selected node/)).toHaveCount(0)
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
  await expect(page.getByText(/Inspect the private receipt before another comparison/)).toBeVisible()
  await expect(page.getByText(/0 of 288 MiB/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Run speed test', exact: true })).toBeDisabled()
})

test('empty automatic quota does not display Go zero timestamp as a reset date', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  model.quality = { ...complete(), resourceProfile: { name: 'constrained', constrained: true, automatic: true },
    quotaState: 'available', quotaUsedBytes: 0, quotaRemainingBytes: 288 * 1048576,
    quotaReviewsUsed: 0, quotaNextResetAt: '0001-01-01T00:00:00Z', manualAllowanceBytes: 24 * 1048576 }
  await open(page)
  await expect(page.getByText('0 of 288 MiB', { exact: true })).toBeVisible()
  await expect(page.getByText(/frees/)).toHaveCount(0)
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
  await expect(page.getByText(/^800\.0 Mbps \//)).toBeVisible()
  await revealDetails(page, 'How reviews work')
  await expect(page.getByText(/latency criterion \(300 ms\)/)).toBeVisible()
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
