import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from './components/ui/button.jsx'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './components/ui/card.jsx'
import { Alert, AlertTitle, AlertDescription } from './components/ui/alert.jsx'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from './components/ui/table.jsx'

const rate = (value) => Number.isFinite(value) && value > 0 ? `${(value * 8 / 1e6).toFixed(1)} Mbps` : '—'

export function NativeQualitySection({ csrfToken, onUnauthorized, busy, onStaged, nodesByTag, onStatusChange }) {
  const [status, setStatus] = useState(null)
  useEffect(() => { onStatusChange?.(status) }, [status, onStatusChange])
  const [error, setError] = useState('')
  const [working, setWorking] = useState(false)
  const epoch = useRef(0)
  const mounted = useRef(false)
  const requests = useRef(new Set())
  const request = useCallback(async (action = '', body) => {
    const controller = new AbortController()
    requests.current.add(controller)
    try {
      const response = await fetch(`/api/v1/performance/quality${action}`, {
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
    mounted.current = true; const ticket = ++epoch.current; setStatus(null); setError(''); setWorking(false)
    Promise.resolve().then(() => { if (mounted.current && ticket === epoch.current) refresh() })
    return () => { mounted.current = false; epoch.current += 1; for (const controller of requests.current) controller.abort(); requests.current.clear() }
  }, [refresh])
  useEffect(() => {
    if (!status) return
    const timer = setTimeout(() => { if (!document.hidden) refresh(); else setStatus((value) => ({ ...value })) }, status.state === 'running' ? 2000 : 30000)
    return () => clearTimeout(timer)
  }, [status, refresh])
  const act = async (action) => {
    if (working) return
    const ticket = epoch.current
    setWorking(true); setError('')
    try {
      await request(`/${action}`, action === 'stage' ? { digest: status.digest } : {})
      if (!mounted.current || ticket !== epoch.current) return
      await refresh()
      if (action === 'stage') onStaged?.()
    } catch (failure) {
      if (mounted.current && ticket === epoch.current && failure.name !== 'AbortError') {
        setError(failure.message)
        // Readback only: a failed response never replays an accepted action.
        const value = await request().catch(() => null)
        if (value && mounted.current && ticket === epoch.current) setStatus(value)
      }
    } finally { if (mounted.current && ticket === epoch.current) setWorking(false) }
  }
  return <Card>
    <CardHeader><CardTitle>Node quality</CardTitle><CardDescription>Compare download and upload speed, then let Xray select a healthy node using those measurements and latency.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      {error && <Alert variant="destructive"><AlertTitle>Check the current state</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      <p>One comparison checks the current node, up to four low-latency alternatives and one rotating alternative. Maximum 144 MiB and three minutes. Results describe this sample and test provider.</p>
      <p>Automatic comparison runs every six hours, with refreshes coalesced into that interval. Up to 576 MiB per day. Measurements do not restart Xray or apply settings; save and apply a recommendation when needed.</p>
      <p role="status">{status?.state === 'running' ? `Comparing nodes (${status.progress?.candidates?.length || 0} measured)…` : status?.state === 'completed' ? 'Comparison complete' : status?.state === 'consumed' ? 'Recommendation saved to Routing. Inspect the configuration there for its application state.' : status?.state === 'failed' || status?.state === 'cleanup-pending' || status?.state === 'cancelled' ? 'Comparison did not complete. Inspect diagnostics before starting another.' : 'Ready to compare nodes'}</p>
      {(status?.progress?.candidates?.length > 0) && <Table><TableHeader><TableRow><TableHead>Node</TableHead><TableHead>Latency</TableHead><TableHead>Download</TableHead><TableHead>Upload</TableHead></TableRow></TableHeader><TableBody>{status.progress.candidates.map((node) => <TableRow key={node.tag}><TableCell>{nodesByTag?.get(node.tag)?.name || node.tag}</TableCell><TableCell>{node.rttMs} ms</TableCell><TableCell>{node.valid ? rate(node.downloadBps) : 'Unavailable'}</TableCell><TableCell>{node.valid ? rate(node.uploadBps) : 'Unavailable'}</TableCell></TableRow>)}</TableBody></Table>}
      <p>Saving creates a pending routing change. Review, discard or apply it in the configuration editor. Xray handles failover even while the panel is stopped; existing connections may need to reconnect.</p>
    </CardContent>
    <CardFooter className="flex flex-wrap gap-2"><Button disabled={!status || busy || working || status.state === 'running'} onClick={() => act('start')}>Compare nodes</Button><Button variant="outline" disabled={!status?.canStage || working || busy} onClick={() => act('stage')}>Save recommendation</Button><Button variant="ghost" disabled={working} onClick={refresh}>Refresh</Button></CardFooter>
  </Card>
}
