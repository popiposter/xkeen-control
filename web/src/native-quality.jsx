import { IconGauge, IconPlayerPlay, IconRefresh, IconTerminal2 } from '@tabler/icons-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from './components/ui/button.jsx'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './components/ui/card.jsx'
import { Alert, AlertTitle, AlertDescription } from './components/ui/alert.jsx'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from './components/ui/table.jsx'
import { Badge } from './components/ui/badge.jsx'
import { Disclosure } from './ui.jsx'

const rate = (value) => Number.isFinite(value) && value > 0 ? `${(value * 8 / 1e6).toFixed(1)} Mbps` : '—'
const stageReasons = {
  'configuration-changed': 'Subscriptions or configuration changed after this test. Run a fresh speed test before applying a recommendation.',
  'configuration-pending': 'Saved configuration changes are pending. Apply or discard them in the configuration editor before running a fresh speed test.',
  'configuration-unavailable': 'The current configuration could not be verified. Inspect it in the configuration editor and refresh the status.',
  'measurement-expired-or-incomplete': 'Measurements have expired or do not include enough successful nodes. Run a fresh speed test.',
  'inspection-required': 'An earlier operation needs inspection before another recommendation can be applied.',
  'quota-unavailable': 'The private comparison receipt is unavailable. Inspect it before another action.',
}
const reviewReasons = {
  'eligible-unavailable-or-over-limit': 'The eligible node set is unavailable; the running pool was kept.',
  'probe-route-shadowed': 'A routing rule without an inbound restriction could capture measurement traffic, so the review would measure the wrong node. Restrict every rule to its inbound tags; the running pool was kept.',
  'rtt-candidates-insufficient': 'Fewer than two candidates answered the latency probe. No speed test ran and no quota was used; the running pool was kept.',
  'rtt-probe-unavailable': 'The latency probe could not run. No speed test ran and no quota was used; the running pool was kept.',
  'rtt-probe-incomplete': 'The latency probe returned an incomplete result. No speed test ran and no quota was used; the running pool was kept.',
  'probe-cleanup-pending': 'A temporary probe rule could not be confirmed removed; the next review retries the cleanup before probing.',
  'insufficient-valid-results': 'Fewer than six nodes produced valid speed results, which is too few to replace a full healthy pool; the running pool was kept.',
  'eligible-unavailable': 'The bounded review subset could not be verified; the running pool was kept.',
  'quota-unavailable-or-exhausted': 'The automatic traffic quota is unavailable or exhausted; the running pool was kept.',
  'quota-busy-or-unavailable': 'Another automatic review owns the traffic quota, or its receipt is unavailable; the running pool was kept.',
  'review-budget-exceeded': 'The review reached its traffic limit; the running pool was kept.',
  'review-cancelled': 'The review was cancelled; the running pool was kept.',
  'review-timeout-or-cancelled': 'The review timed out or was cancelled; the running pool was kept.',
  'review-incomplete-or-expired': 'Not every eligible node was measured with fresh evidence; the running pool was kept.',
  'review-insufficient-or-expired': 'Too few nodes produced valid fresh measurements; the running pool was kept.',
  'batch-incomplete-or-unknown': 'A batch did not finish with confirmed cleanup; inspect the current state.',
  'configuration-changed-or-pending': 'Subscriptions or configuration changed during the review; the running pool was kept.',
  'configuration-pending-or-unavailable': 'Configuration changes are pending or unavailable; the review was deferred.',
  'native-override-or-unavailable': 'Native selection is overridden or unavailable; the running pool was kept.',
  'native-speed-conflict-or-unavailable': 'A native speed job or resource check blocks automatic comparison.',
  'resource-pressure-or-unavailable': 'Router resource pressure or unavailable telemetry prevented application.',
  'panel-busy': 'Another panel operation is running; the review was deferred.',
  'node-profile-changed': 'The enabled node set changed during review; the running pool was kept.',
  'selector-unavailable': 'The selected pool could not be validated; the running pool was kept.',
  'recommendation-invalid': 'The proposed routing update could not be validated; the running pool was kept.',
  'application-unavailable': 'Native application is unavailable; the running pool was kept.',
  'native-selection-readback-unavailable': 'The native selected node could not be verified; inspect the current pool.',
  'save-outcome-unknown': 'The routing save outcome is unknown. Inspect configuration before another action.',
  'saved-digest-unconfirmed': 'The saved routing digest could not be confirmed. Inspect configuration before another action.',
  'apply-admission-unknown': 'The native restart was not confirmed after saving. Inspect the pending configuration.',
  'apply-outcome-unknown': 'The native restart outcome is unknown. Inspect the native job and configuration.',
  'apply-readback-unavailable': 'The native job could not be read back. Inspect its receipt and configuration.',
  'apply-not-verified': 'The native job or configuration did not verify as applied.',
  'applied-config-readback-mismatch': 'The applied routing does not match the proposed pool. Inspect configuration.',
  'inspection-required': 'Automatic application needs inspection. Do not repeat the operation.',
  'inspection-receipt-unavailable': 'The private inspection receipt could not be settled. Inspect it before another comparison.',
  'pool-unchanged': 'The measured recommendation has the same members as the active pool; no restart was needed.',
  'healthy-target-sample-invalid': 'The current healthy node had an invalid speed sample; its active pool was retained.',
  'insufficient-challenger-margin': 'Challengers did not improve the measured pool score by 15%; the active pool was retained.',
  'incumbent-unmeasured-or-unknown': 'A current pool member lacks fresh, valid evidence; the active pool was retained.',
  'native-health-ambiguous': 'Native health readback is ambiguous; the active pool was retained.',
  'selector-changed': 'The active selector changed during the review; the active pool was retained.',
}
const poolDecisionText = {
  'pool-unchanged': 'same pool members; no restart',
  'first-pool-initialization': 'first bounded pool',
  'material-improvement': 'measured improvement of at least 15%',
  'unhealthy-incumbent-replaced': 'unhealthy pool member replaced',
  'observatory-repair': 'same members; native health observation narrowed to the pool',
  'insufficient-valid-results': 'too few valid results to replace a full healthy pool',
  'provisional-pool-replaced': 'provisional recovery pool replaced by measured nodes',
  'pool-filled': 'undersized pool filled with measured nodes, keeping every member',
}
const recoveryReasons = {
  'manual-override-active': 'a manual node override is active; clear it to allow recovery',
  'recovery-apply-gap': 'a provisional pool was applied less than an hour ago; recovery does not restart XKeen again before then',
  'inspection-required': 'an earlier operation needs inspection',
  'probe-route-shadowed': 'a routing rule could capture probe traffic',
  'probe-cleanup-pending': 'a temporary probe rule could not be confirmed removed',
  'panel-busy': 'another panel operation is running',
  'configuration-changed-or-pending': 'configuration changed or is pending',
  'configuration-pending-or-unavailable': 'configuration changes are pending or unavailable',
  'resource-pressure-or-unavailable': 'router resources are busy',
}
const validTime = (value) => value && new Date(value).getFullYear() >= 2020
function recoveryText(recovery) {
  const reason = recoveryReasons[recovery?.reason] || recovery?.reason || 'unavailable'
  switch (recovery?.state) {
    case 'probing': case 'applying': return 'No pool member is healthy. Availability recovery is probing other enabled nodes…'
    case 'vpn-unavailable': return `No pool member is healthy and none of the ${recovery.probed || 0} probed nodes answered. VPN destinations are unavailable; the configuration was left unchanged and they stay blocked rather than going direct. Recovery retries other nodes every 10 minutes.`
    case 'applied': return `No pool member was healthy. Availability recovery applied a provisional pool of ${recovery.pool?.length || 0} nodes that answered a latency probe (${recovery.verified || 0} of ${recovery.probed || 0} answered).`
    case 'deferred': return `No pool member is healthy, but availability recovery is deferred: ${reason}.`
    case 'failed': return `Availability recovery could not apply a provisional pool: ${reason}. The configuration was kept.`
    case 'inspection-required': return `Availability recovery needs inspection: ${reason}.`
    default: return ''
  }
}

const appliedText = { applied: 'applied', 'no-op': 'no restart needed', 'not-applied': 'not applied', 'inspection-required': 'needs inspection', running: 'applying', 'not-attempted': 'not applied yet' }
function automaticText(status) {
  switch (status?.automaticReason) {
    case 'constrained-device': return 'Automatic speed comparisons are disabled on this constrained router.'
    case 'operator-disabled': return 'Automatic node reviews are temporarily disabled by the operator for this panel process.'
    case undefined: case '': return 'At most one automatic review per 24 hours, six hours after any speed test; node changes bring it forward.'
    default: return 'Automatic comparisons are deferred: a native periodic speed test is configured or its state is unavailable.'
  }
}
function QualityTile({ label, value, detail }) {
  return <div className="flex flex-col gap-1 rounded-lg border p-3"><span className="text-xs text-muted-foreground">{label}</span><strong className="font-medium">{value}</strong>{detail && <span className="text-xs text-muted-foreground">{detail}</span>}</div>
}

export function NativeQualitySection({ csrfToken, onUnauthorized, busy, workingEdits, onReadback, onNativeJob, onOpenConsole, onInspectConfigs, nodesByTag, onStatusChange }) {
  const [status, setStatus] = useState(null)
  useEffect(() => { onStatusChange?.(status) }, [status, onStatusChange])
  const [error, setError] = useState('')
  const [applyError, setApplyError] = useState('')
  const [working, setWorking] = useState(false)
  const [applyJob, setApplyJob] = useState(null)
  const [applyAttempted, setApplyAttempted] = useState(false)
  const epoch = useRef(0)
  const mounted = useRef(false)
  const requests = useRef(new Set())
  const request = useCallback(async (action = '', body, jobRead = false) => {
    const controller = new AbortController()
    requests.current.add(controller)
    try {
      const response = await fetch(jobRead ? '/api/v1/xkeen/jobs/read' : `/api/v1/performance/quality${action}`, {
        credentials: 'same-origin', cache: 'no-store', signal: controller.signal,
        ...(body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify(body) }),
      })
      if (response.status === 401) { onUnauthorized?.(); throw new Error('Session expired') }
      if (!response.ok) throw new Error(body === undefined ? 'Quality information unavailable' : 'Action was not confirmed. Refresh the status and inspect saved configurations before trying again.')
      return await response.json()
    } catch (failure) {
      if (failure.name === 'AbortError') throw failure
      throw new Error(body === undefined ? 'Quality information unavailable' : 'Action was not confirmed. Refresh the status and inspect saved configurations before trying again.')
    } finally { requests.current.delete(controller) }
  }, [csrfToken, onUnauthorized])
  const refresh = useCallback(async () => {
    const ticket = epoch.current
    try {
      const value = await request()
      if (!mounted.current || ticket !== epoch.current) return
      if (!value || typeof value.state !== 'string' || !Array.isArray(value.progress?.candidates ?? [])) throw new Error('Quality information unavailable')
      setStatus(value); setError('')
    } catch (failure) { if (mounted.current && ticket === epoch.current && failure.name !== 'AbortError') setError(failure.message) }
  }, [request])
  useEffect(() => {
    mounted.current = true; const ticket = ++epoch.current; setStatus(null); setError(''); setApplyError(''); setWorking(false); setApplyJob(null); setApplyAttempted(false)
    Promise.resolve().then(() => { if (mounted.current && ticket === epoch.current) refresh() })
    return () => { mounted.current = false; epoch.current += 1; for (const controller of requests.current) controller.abort(); requests.current.clear() }
  }, [refresh])
  useEffect(() => {
    if (!status) return
    const timer = setTimeout(() => { if (!document.hidden) refresh(); else setStatus((value) => ({ ...value })) }, status.state === 'running' ? 2000 : 30000)
    return () => clearTimeout(timer)
  }, [status, refresh])
  useEffect(() => {
    if (!applyJob || applyJob.state !== 'running') return
    let active = true
    const timer = setTimeout(async () => {
      try {
        const job = await request('', { id: applyJob.id, cursor: Number.MAX_SAFE_INTEGER }, true)
        if (!active || !mounted.current) return
        if (job.id !== applyJob.id || typeof job.state !== 'string') throw new Error('Application state could not be confirmed')
        setApplyJob(job); onNativeJob?.(job)
        if (job.state !== 'running') {
          onReadback?.(); await refresh()
          if (job.state !== 'completed' || job.configurationState !== 'applied') setApplyError('Application was not verified. Inspect the saved configuration and XKeen console. Do not repeat the operation.')
        }
      } catch (failure) {
        if (active && mounted.current && failure.name !== 'AbortError') {
          setApplyJob((job) => ({ ...job, state: 'unknown' })); onReadback?.()
          setApplyError('Application outcome is unknown. Inspect the configuration and console before taking another action.')
        }
      }
    }, 1000)
    return () => { active = false; clearTimeout(timer) }
  }, [applyJob, request, refresh, onReadback, onNativeJob])
  const applying = applyJob?.state === 'running'
  const act = async (action) => {
    if (working || applying || (action === 'apply' && (workingEdits || applyAttempted))) return
    const ticket = epoch.current
    setWorking(true); setError('')
    if (action === 'apply') setApplyAttempted(true)
    try {
      const value = await request(`/${action}`, action === 'apply' ? { digest: status.digest } : {})
      if (!mounted.current || ticket !== epoch.current) return
      if (action === 'apply') {
        if (!value.id || typeof value.state !== 'string') throw new Error('Application acceptance could not be confirmed')
        setApplyJob(value); onNativeJob?.(value); onReadback?.()
        if (value.state !== 'running' && (value.state !== 'completed' || value.configurationState !== 'applied')) setApplyError('Application was not verified. Inspect the configuration and console.')
      } else { setApplyAttempted(false); setApplyJob(null); setApplyError('') }
      await refresh()
    } catch (failure) {
      if (mounted.current && ticket === epoch.current && failure.name !== 'AbortError') {
        setError(failure.message)
        if (action === 'apply') setApplyError(failure.message)
        // Readback only: a failed response never replays an accepted action.
        const value = await request().catch(() => null)
        if (value && mounted.current && ticket === epoch.current) setStatus(value)
        if (action === 'apply') onReadback?.()
      }
    } finally { if (mounted.current && ticket === epoch.current) setWorking(false) }
  }
  return <Card>
    <CardHeader><CardTitle>Speed test</CardTitle><CardDescription>Reviews keep a pool of up to six fast, healthy nodes. Xray selects among them by live health and latency.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      {(applyError || error) && <Alert variant="destructive"><AlertTitle>Check the current state</AlertTitle><AlertDescription>{applyError || error}</AlertDescription></Alert>}
      {status?.startReason && <p role="alert">{status.startReason === 'native-speed-conflict' ? 'A native periodic speed test is configured. Inspect and quiesce it before starting this comparison.' : 'Resource telemetry is unavailable; the comparison was not started.'}</p>}
      {status?.state === 'completed' && !status.canStage && stageReasons[status.stageReason] && <Alert><AlertTitle>Recommendation unavailable</AlertTitle><AlertDescription>{stageReasons[status.stageReason]}</AlertDescription></Alert>}
      {status?.inspectionRequired && <Alert variant="destructive"><AlertTitle>Inspection required</AlertTitle><AlertDescription>Check the native job and configuration. Then verify the current state below. This action may clear the panel's temporary probe rules; it will refuse to clear the block if native state cannot be proven settled.</AlertDescription></Alert>}
      {['resource-pressure', 'resource-telemetry-unavailable', 'native-speed-conflict'].includes(status?.progress?.reasonCode) && <p role="alert">{status.progress.reasonCode === 'resource-pressure' ? 'Test stopped because router resources are busy. Partial results are shown below.' : status.progress.reasonCode === 'native-speed-conflict' ? 'Quiesce the native periodic speed test before testing here.' : 'Resource telemetry is unavailable; testing stopped.'}</p>}
      {status?.orphanedPool?.length > 0 && <p role="status">{status.orphanedPool.length === 1 ? 'One pool member no longer exists' : `${status.orphanedPool.length} pool members no longer exist`} in the enabled nodes (for example after a subscription update). The next automatic review treats them as unhealthy and replaces them{status.activePoolCount > 0 ? '; the remaining members keep working.' : '. No pool member remains enabled, so proxied destinations may be unavailable until then.'}</p>}
      {validTime(status?.provisionalAt) && <p role="status">Provisional pool since <time dateTime={status.provisionalAt}>{new Date(status.provisionalAt).toLocaleString()}</time>: availability recovery chose its members by latency probe only, not by speed. The next complete automatic review replaces it with measured nodes.</p>}
      {recoveryText(status?.recovery) && <p role="status">{recoveryText(status.recovery)}{validTime(status.recovery.checkedAt) ? ` Last check: ${new Date(status.recovery.checkedAt).toLocaleString()}.` : ''}</p>}
      {status && <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <QualityTile label="Pool" value={status.activePoolState === 'unavailable' ? 'Unavailable' : `${status.activePoolCount || 0} nodes`} detail={status.activePoolState === 'unavailable' ? 'Active pool readback unavailable; inspect the native routing configuration.' : `${validTime(status.provisionalAt) ? 'Provisional' : status.activePoolState === 'frozen-at-review' ? 'At review start' : 'Verified'} · ${status.poolCount || 0} enabled${status.nativeSelectedState === 'observed' && status.nativeSelected ? ` · selected ${nodesByTag?.get(status.nativeSelected)?.name || status.nativeSelected}` : ''}`} />
        <QualityTile label={status.manualSample ? 'Last speed test' : 'Last review'} value={status.batchCount > 0 ? `${status.validCount || 0} of ${status.attemptedCount || 0} valid` : status.state === 'deferred' ? 'Deferred' : 'None yet'} detail={status.batchCount > 0 ? `${((status.aggregateBytes || 0) / 1048576).toFixed(1)} MiB${status.reviewTrigger ? ` · ${status.reviewTrigger}` : ''}${status.poolDecision ? ` · ${poolDecisionText[status.poolDecision] || 'current pool retained'}` : ''}${!status.manualSample && status.appliedState ? ` · ${appliedText[status.appliedState] || status.appliedState}` : ''}` : status.reviewTrigger ? `Trigger: ${status.reviewTrigger}` : ''} />
        <QualityTile label="Next review" value={status.automaticReason ? 'Off' : validTime(status.nextDueAt) ? new Date(status.nextDueAt).toLocaleString() : '—'} detail={automaticText(status)} />
        {status.resourceProfile?.automatic && <QualityTile label="Traffic today" value={status.quotaState === 'available' ? `${((status.quotaUsedBytes || 0) / 1048576).toFixed(0)} of ${(((status.quotaUsedBytes || 0) + (status.quotaRemainingBytes || 0)) / 1048576).toFixed(0)} MiB` : 'Unavailable'} detail={status.quotaState === 'available' ? `${status.quotaReviewsUsed || 0} of 1 automatic review in 24 h${validTime(status.quotaNextResetAt) ? ` · frees ${new Date(status.quotaNextResetAt).toLocaleString()}` : ''}` : 'Inspect the private receipt before another comparison.'} />}
      </div>}
      {status?.reviewReason && <p role="status">{reviewReasons[status.reviewReason] || `${status.manualSample ? 'Speed test' : 'Automatic review'}: ${status.reviewReason}.`}</p>}
      {status?.appliedJobState && <p className="text-sm text-muted-foreground">Native job: {status.appliedJobState}; configuration: {status.appliedConfigState || 'not yet verified'}.</p>}
      <p role="status">{applying ? 'Recommendation saved. XKeen is restarting; waiting for configuration verification…' : applyJob?.state === 'completed' && applyJob.configurationState === 'applied' ? 'Recommendation applied and configuration verified.' : status?.state === 'applying' ? 'Automatic review complete; verifying one native pool application…' : status?.state === 'running' ? `Testing nodes (${status.progress?.validCount || 0} successful, ${status.progress?.candidates?.length || 0} attempted)…` : status?.state === 'completed' ? `Speed test complete: ${status.progress?.validCount || 0} successful${status.progress?.reasonCode === 'generation-budget' ? ' (budget reached)' : ''}` : status?.state === 'deferred' ? 'Automatic review deferred before measuring node speeds.' : status?.state === 'consumed' ? 'Recommendation consumed. Inspect the configuration for its application state.' : status?.state === 'failed' || status?.state === 'cleanup-pending' || status?.state === 'cancelled' ? 'Speed test did not complete. Inspect diagnostics before starting another.' : 'Ready to test node speeds'}</p>
      {(status?.progress?.candidates?.length > 0) && <Table><TableHeader><TableRow><TableHead>Rank</TableHead><TableHead>Node</TableHead><TableHead>Latency</TableHead><TableHead>Speed ↓ / ↑</TableHead></TableRow></TableHeader><TableBody>{status.progress.candidates.map((node) => { const rank = status.ranking?.find((item) => item.tag === node.tag)?.rank; return <TableRow key={node.tag}><TableCell>{rank ? <Badge variant="secondary">#{rank}</Badge> : '—'}</TableCell><TableCell>{nodesByTag?.get(node.tag)?.name || node.tag}</TableCell><TableCell>{node.rttMs} ms</TableCell><TableCell>{node.valid ? `${rate(node.downloadBps)} / ${rate(node.uploadBps)}` : 'Unavailable'}</TableCell></TableRow> })}</TableBody></Table>}
      {status && <Disclosure title="How reviews work">
        <div className="flex flex-col gap-3 text-sm text-muted-foreground">
          <p>Every review probes its candidates first: the current pool and a rotating share of the other enabled nodes. Only nodes that answer within the configured latency criterion{status.latencyLimitMs > 0 ? ` (${status.latencyLimitMs} ms)` : ''} are speed-tested, and the best six measured nodes form the recommendation. A bounded subset is not a global ranking.{status.selectedForSpeed > 0 ? ` Last scope: ${status.selectedForSpeed} of ${status.totalEligible} eligible nodes, ${status.deferredForFutureReview || 0} deferred to later reviews.` : ''}</p>
          {status.limits && <p>Router profile: {status.resourceProfile?.name}. Up to {status.limits.candidates} successful nodes and {status.limits.attempts} attempts, {(status.limits.bytes / 1048576).toFixed(0)} MiB and {status.limits.seconds} seconds including cleanup per batch; a whole review stays within {((status.manualAllowanceBytes || 0) / 1048576).toFixed(0)} MiB, the same limit as one manual speed test. Failed transfers count toward the budget. Tests stop if sustained CPU, memory or swap pressure is detected.</p>}
          <p>A manual speed test needs an explicit Apply; a complete automatic review may apply one verified pool change. Apply saves the pool and restarts XKeen once. Xray then chooses within the pool using its native strategy and live health; rank one is not pinned and existing connections may need to reconnect. The previous configuration remains available in the editor.</p>
        </div>
      </Disclosure>}
      {workingEdits && <p role="alert">Save or discard unfinished configuration edits before applying the recommendation.</p>}
    </CardContent>
    <CardFooter className="flex flex-wrap gap-2"><Button disabled={!status || busy || working || applying || status.state === 'running' || status.state === 'applying' || status.inspectionRequired || (status.resourceProfile?.automatic && status.quotaState !== 'available')} onClick={() => act('start')}><IconGauge data-icon="inline-start" />Run speed test</Button><Button variant="outline" disabled={!status?.canStage || working || busy || applying || applyAttempted || workingEdits || status.state === 'applying' || status.inspectionRequired} onClick={() => act('apply')}><IconPlayerPlay data-icon="inline-start" />Apply recommendation</Button><Button variant="ghost" disabled={working} onClick={refresh}><IconRefresh data-icon="inline-start" />Refresh</Button>{status?.inspectionRequired && <Button variant="outline" disabled={busy || working || applying} onClick={() => act('inspect')}>Verify and clear inspection block</Button>}{(applyAttempted || status?.inspectionRequired) && <><Button variant="ghost" onClick={onInspectConfigs}>Inspect configuration</Button><Button variant="ghost" onClick={onOpenConsole}><IconTerminal2 data-icon="inline-start" />XKeen console</Button></>}</CardFooter>
  </Card>
}
