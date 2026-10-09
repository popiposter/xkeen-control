import { IconGauge, IconPlayerPlay, IconRefresh, IconTerminal2 } from '@tabler/icons-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from './components/ui/button.jsx'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './components/ui/card.jsx'
import { Alert, AlertTitle, AlertDescription } from './components/ui/alert.jsx'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from './components/ui/table.jsx'
import { Badge } from './components/ui/badge.jsx'

const rate = (value) => Number.isFinite(value) && value > 0 ? `${(value * 8 / 1e6).toFixed(1)} Mbps` : '—'
const stageReasons = {
  'configuration-changed': 'Subscriptions or configuration changed after this test. Run a fresh speed test before applying a recommendation.',
  'configuration-pending': 'Saved configuration changes are pending. Apply or discard them in the configuration editor before running a fresh speed test.',
  'configuration-unavailable': 'The current configuration could not be verified. Inspect it in the configuration editor and refresh the status.',
  'measurement-expired-or-incomplete': 'Measurements have expired or do not include enough successful nodes. Run a fresh speed test.',
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
    <CardHeader><CardTitle>Speed test</CardTitle><CardDescription>Compare download and upload speed, then let Xray select a healthy node using those measurements and latency.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      {(applyError || error) && <Alert variant="destructive"><AlertTitle>Check the current state</AlertTitle><AlertDescription>{applyError || error}</AlertDescription></Alert>}
      {status?.startReason && <p role="alert">{status.startReason === 'native-speed-conflict' ? 'A native periodic speed test is configured. Inspect and quiesce it before starting this comparison.' : 'Resource telemetry is unavailable; the comparison was not started.'}</p>}
      {status?.state === 'completed' && !status.canStage && stageReasons[status.stageReason] && <Alert><AlertTitle>Recommendation unavailable</AlertTitle><AlertDescription>{stageReasons[status.stageReason]}</AlertDescription></Alert>}
      {status?.limits && <p>Router profile: {status.resourceProfile?.name}. Up to {status.limits.candidates} successful nodes and {status.limits.attempts} attempts, {(status.limits.bytes / 1048576).toFixed(0)} MiB and {status.limits.seconds} seconds including cleanup. Failed transfers count toward the budget. Results may be partial.</p>}
      <p>Only fresh healthy observations within the configured latency criterion qualify. Manual comparisons also limit latency to twice the lowest value, with a 300 ms floor. Measurements alone do not change the running configuration.</p>
      <p>{status?.automaticReason === 'constrained-device' ? 'Automatic speed comparisons are disabled on this constrained router.' : status?.automaticReason ? 'Automatic comparisons are deferred: a native periodic speed test is configured or its state is unavailable.' : 'Automatic comparisons run at most every six hours and coalesce subscription refreshes.'} Tests stop if sustained CPU, memory or swap pressure is detected. External native jobs are outside panel exclusion.</p>
      {['resource-pressure', 'resource-telemetry-unavailable', 'native-speed-conflict'].includes(status?.progress?.reasonCode) && <p role="alert">{status.progress.reasonCode === 'resource-pressure' ? 'Test stopped because router resources are busy. Partial results are shown below.' : status.progress.reasonCode === 'native-speed-conflict' ? 'Quiesce the native periodic speed test before testing here.' : 'Resource telemetry is unavailable; testing stopped.'}</p>}
      {status?.latencyLimitMs > 0 && <p className="text-sm text-muted-foreground">{status.manualSample ? 'Manual' : 'Automatic'} sample: {status.eligibleCount} eligible nodes, latency threshold {status.latencyLimitMs} ms. Recommendation: best {Math.min(6, status.progress?.validCount || 0)} measured nodes by speed, latency and observed stability.</p>}
      <p role="status">{applying ? 'Recommendation saved. XKeen is restarting; waiting for configuration verification…' : applyJob?.state === 'completed' && applyJob.configurationState === 'applied' ? 'Recommendation applied and configuration verified.' : status?.state === 'running' ? `Testing nodes (${status.progress?.validCount || 0} successful, ${status.progress?.candidates?.length || 0} attempted)…` : status?.state === 'completed' ? `Speed test complete: ${status.progress?.validCount || 0} successful${status.progress?.reasonCode === 'generation-budget' ? ' (budget reached)' : ''}` : status?.state === 'consumed' ? 'Recommendation consumed. Inspect the configuration for its application state.' : status?.state === 'failed' || status?.state === 'cleanup-pending' || status?.state === 'cancelled' ? 'Speed test did not complete. Inspect diagnostics before starting another.' : 'Ready to test node speeds'}</p>
      {(status?.progress?.candidates?.length > 0) && <Table><TableHeader><TableRow><TableHead>Recommended pool</TableHead><TableHead>Node</TableHead><TableHead>Latency</TableHead><TableHead>Download</TableHead><TableHead>Upload</TableHead></TableRow></TableHeader><TableBody>{status.progress.candidates.map((node) => <TableRow key={node.tag}><TableCell>{status.ranking?.find((item) => item.tag === node.tag)?.rank ? <Badge variant="secondary">#{status.ranking.find((item) => item.tag === node.tag).rank}</Badge> : '—'}</TableCell><TableCell>{nodesByTag?.get(node.tag)?.name || node.tag}</TableCell><TableCell>{node.rttMs} ms</TableCell><TableCell>{node.valid ? rate(node.downloadBps) : 'Unavailable'}</TableCell><TableCell>{node.valid ? rate(node.uploadBps) : 'Unavailable'}</TableCell></TableRow>)}</TableBody></Table>}
      <p>Apply recommendation saves the selected pool and restarts XKeen once. Xray chooses within that pool using its existing native strategy and live health; speed weights apply only to leastLoad. Rank one is not pinned. Existing connections may need to reconnect. Previous configuration remains available in the editor.</p>
      {workingEdits && <p role="alert">Save or discard unfinished configuration edits before applying the recommendation.</p>}
    </CardContent>
    <CardFooter className="flex flex-wrap gap-2"><Button disabled={!status || busy || working || applying || status.state === 'running'} onClick={() => act('start')}><IconGauge data-icon="inline-start" />Run speed test</Button><Button variant="outline" disabled={!status?.canStage || working || busy || applying || applyAttempted || workingEdits} onClick={() => act('apply')}><IconPlayerPlay data-icon="inline-start" />Apply recommendation</Button><Button variant="ghost" disabled={working} onClick={refresh}><IconRefresh data-icon="inline-start" />Refresh</Button>{applyAttempted && <><Button variant="ghost" onClick={onInspectConfigs}>Inspect configuration</Button><Button variant="ghost" onClick={onOpenConsole}><IconTerminal2 data-icon="inline-start" />XKeen console</Button></>}</CardFooter>
  </Card>
}
