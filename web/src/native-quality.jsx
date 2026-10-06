import { IconGauge, IconPlayerPlay, IconRefresh, IconTerminal2 } from '@tabler/icons-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from './components/ui/button.jsx'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './components/ui/card.jsx'
import { Alert, AlertTitle, AlertDescription } from './components/ui/alert.jsx'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from './components/ui/table.jsx'
import { Badge } from './components/ui/badge.jsx'

const rate = (value) => Number.isFinite(value) && value > 0 ? `${(value * 8 / 1e6).toFixed(1)} Mbps` : '—'
const milliseconds = (value) => Number.isFinite(value) && value >= 0 ? `${value.toFixed(1)} ms` : '—'

function QualityDetails({ node, cost }) {
  const m = node.metrics
  if (!m) return <span className="text-muted-foreground">No detailed measurements</span>
  return <details><summary className="cursor-pointer text-primary">{Number.isFinite(node.qualityScore) && node.qualityScore > 0 ? `Quality ${node.qualityScore.toFixed(0)}/100 · ` : ''}Full metrics{cost ? ` · weight ${cost.toFixed(2)}` : ''}</summary>
    <div className="mt-3 grid min-w-64 gap-3 text-sm">
      {m.failureCode && <p role="alert">Measurement failed during {m.failurePhase || 'transfer'}: {m.failureCode === 'provider-http-status' ? `test provider returned HTTP ${m.httpStatus}` : m.failureCode === 'timeout-or-cancelled' ? 'request timed out or was cancelled' : m.failureCode === 'insufficient-samples' ? 'not enough latency samples' : 'transfer was incomplete'}. This does not by itself prove the node is offline.</p>}
      <Table><TableHeader><TableRow><TableHead>Latency</TableHead><TableHead>Median</TableHead><TableHead>p95</TableHead><TableHead>Jitter</TableHead><TableHead>Samples</TableHead></TableRow></TableHeader><TableBody>{[['Idle', m.idle], ['Downloading', m.downloadLatency], ['Uploading', m.uploadLatency]].map(([label, metric]) => <TableRow key={label}><TableCell>{label}</TableCell><TableCell>{metric?.samples ? milliseconds(metric.medianMs) : '—'}</TableCell><TableCell>{metric?.samples ? milliseconds(metric.p95Ms) : '—'}</TableCell><TableCell>{metric?.samples >= 2 ? milliseconds(metric.jitterMs) : '—'}</TableCell><TableCell>{metric?.samples || 0}</TableCell></TableRow>)}</TableBody></Table>
      <Table><TableHeader><TableRow><TableHead>Throughput</TableHead><TableHead>Median</TableHead><TableHead>p10–p90</TableHead><TableHead>Samples</TableHead></TableRow></TableHeader><TableBody>{[['Download', m.download], ['Upload', m.upload]].map(([label, metric]) => <TableRow key={label}><TableCell>{label}</TableCell><TableCell>{rate(metric?.medianBps)}</TableCell><TableCell>{rate(metric?.p10Bps)} – {rate(metric?.p90Bps)}</TableCell><TableCell>{metric?.samples || 0}{metric?.shortSamples && ' · short transfers'}</TableCell></TableRow>)}</TableBody></Table>
      <p>Requests: {m.requests - m.failures}/{m.requests} successful{m.requests > 0 ? ` (${(100 * (m.requests - m.failures) / m.requests).toFixed(1)}%)` : ''}. This is HTTP request reliability, not UDP packet loss.</p>
      {[['Download',m.download],['Upload',m.upload]].map(([label,metric])=>metric?.measurements?.length>0 && <details key={label}><summary className="cursor-pointer">{label}: all transfer samples</summary><Table><TableHeader><TableRow><TableHead>Size</TableHead><TableHead>Duration</TableHead><TableHead>Speed</TableHead></TableRow></TableHeader><TableBody>{metric.measurements.map((sample,i)=><TableRow key={i}><TableCell>{(sample.bytes/1048576).toFixed(0)} MiB</TableCell><TableCell>{milliseconds(sample.durationMs)}</TableCell><TableCell>{rate(sample.bps)}</TableCell></TableRow>)}</TableBody></Table></details>)}
      <p className="text-muted-foreground">Warm-up is excluded from speed results. Quality is relative to this sample; higher is better. It accounts for speed, failures, jitter, loaded latency growth and speed variation. Speed benefit saturates above 100 Mbps download / 30 Mbps upload; ranking also uses latency. Short transfers or fewer than two loaded samples limit confidence. Results describe the router-to-Cloudflare path at measurement time.</p>
    </div>
  </details>
}
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
      {status?.state === 'completed' && !status.canStage && stageReasons[status.stageReason] && <Alert><AlertTitle>Recommendation unavailable</AlertTitle><AlertDescription>{stageReasons[status.stageReason]}</AlertDescription></Alert>}
      <p>The detailed test samples up to 12 healthy enabled nodes in fresh latency order, within twice the lowest latency (minimum threshold 300 ms, maximum 750 ms). Failed measurements are replaced by the next eligible node. At most 18 attempts, 864 MiB and twelve minutes; results may be partial.</p>
      <p>The same detailed test runs every six hours and after successful subscription updates. Updates coalesce for two minutes, with at least one hour between tests. Measurements alone do not change the running configuration.</p>
      {status?.latencyLimitMs > 0 && <p className="text-sm text-muted-foreground">Detailed sample: {status.eligibleCount} eligible nodes, latency threshold {status.latencyLimitMs} ms. Recommendation: best {Math.min(6, status.progress?.validCount || 0)} measured nodes by speed, latency and observed stability.</p>}
      <p role="status">{applying ? 'Recommendation saved. XKeen is restarting; waiting for configuration verification…' : applyJob?.state === 'completed' && applyJob.configurationState === 'applied' ? 'Recommendation applied and configuration verified.' : status?.state === 'running' ? `Testing nodes (${status.progress?.validCount || 0} successful, ${status.progress?.candidates?.length || 0} attempted)…` : status?.state === 'completed' ? `Speed test complete: ${status.progress?.validCount || 0} successful${status.progress?.reasonCode === 'generation-budget' ? ' (budget reached)' : ''}` : status?.state === 'consumed' ? 'Recommendation consumed. Inspect the configuration for its application state.' : status?.state === 'failed' || status?.state === 'cleanup-pending' || status?.state === 'cancelled' ? 'Speed test did not complete. Inspect diagnostics before starting another.' : 'Ready to test node speeds'}</p>
      {(status?.progress?.candidates?.length > 0) && <Table><TableHeader><TableRow><TableHead>Recommended pool</TableHead><TableHead>Node</TableHead><TableHead>Latency</TableHead><TableHead>Download</TableHead><TableHead>Upload</TableHead><TableHead>Quality & metrics</TableHead></TableRow></TableHeader><TableBody>{status.progress.candidates.map((node) => <TableRow key={node.tag}><TableCell>{status.ranking?.find((item) => item.tag === node.tag)?.rank ? <Badge variant="secondary">#{status.ranking.find((item) => item.tag === node.tag).rank}</Badge> : '—'}</TableCell><TableCell>{nodesByTag?.get(node.tag)?.name || node.tag}</TableCell><TableCell>{node.rttMs} ms</TableCell><TableCell>{node.valid ? rate(node.downloadBps) : 'Unavailable'}</TableCell><TableCell>{node.valid ? rate(node.uploadBps) : 'Unavailable'}</TableCell><TableCell><QualityDetails node={node} cost={status.ranking?.find((item) => item.tag === node.tag)?.cost} /></TableCell></TableRow>)}</TableBody></Table>}
      <p>Apply recommendation saves the selected pool and restarts XKeen once. Xray chooses within that pool using live health, latency and speed weights; rank one is not pinned. Existing connections may need to reconnect. Previous configuration remains available in the editor.</p>
      {workingEdits && <p role="alert">Save or discard unfinished configuration edits before applying the recommendation.</p>}
    </CardContent>
    <CardFooter className="flex flex-wrap gap-2"><Button disabled={!status || busy || working || applying || status.state === 'running'} onClick={() => act('start')}><IconGauge data-icon="inline-start" />Run speed test</Button><Button variant="outline" disabled={!status?.canStage || working || busy || applying || applyAttempted || workingEdits} onClick={() => act('apply')}><IconPlayerPlay data-icon="inline-start" />Apply recommendation</Button><Button variant="ghost" disabled={working} onClick={refresh}><IconRefresh data-icon="inline-start" />Refresh</Button>{applyAttempted && <><Button variant="ghost" onClick={onInspectConfigs}>Inspect configuration</Button><Button variant="ghost" onClick={onOpenConsole}><IconTerminal2 data-icon="inline-start" />XKeen console</Button></>}</CardFooter>
  </Card>
}
