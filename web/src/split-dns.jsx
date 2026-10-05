import { useEffect, useRef, useState } from 'react'
import { IconWorld, IconRefresh, IconShieldCheck, IconAlertTriangle, IconClock } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

const labels = { synced: 'Synchronized', checking: 'Checking', pending: 'Waiting for Apply', failed: 'Needs attention', unconfigured: 'Not installed' }

export function SplitDNSSection({ csrfToken, onUnauthorized, readbackKey }) {
  const action = useRef(null)
  const owner = useRef(null)
  useEffect(() => {
    const session = Symbol('dns-session')
    owner.current = session
    return () => { owner.current = null; action.current?.abort(); action.current = null }
  }, [csrfToken])
  const [status, setStatus] = useState(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    let alive = true
    const read = async () => {
      try {
        const response = await fetch('/api/v1/dns/split', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
        if (!alive) return
        if (response.status === 401) { onUnauthorized?.(); return }
        if (!response.ok) return
        const value = await response.json()
        if (alive) setStatus(value)
      } catch (error) { if (alive && error.name !== 'AbortError') setNotice('DNS status is unavailable.') }
    }
    void read()
    const timer = setInterval(read, 30_000)
    return () => { alive = false; controller.abort(); clearInterval(timer) }
  }, [csrfToken, onUnauthorized, readbackKey, refresh])
  async function synchronize() {
    if (action.current) return
    const controller = new AbortController()
    action.current = controller
    const session = owner.current
    const current = () => owner.current === session && !controller.signal.aborted
    const deadline = setTimeout(() => controller.abort(), 45_000)
    setBusy(true); setNotice('')
    try {
      const response = await fetch('/api/v1/dns/split/sync', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: '{}', signal: controller.signal })
      if (!current()) return
      if (response.status === 401) { onUnauthorized?.(); return }
      const value = await response.json()
      if (!current()) return
      if (value.state) setStatus(value)
      if (!response.ok) setNotice(value.message || value.error || 'DNS synchronization was not confirmed. Native commands were not repeated.')
      setRefresh((v) => v + 1)
    } catch { if (owner.current === session) setNotice('DNS result is unknown. Refresh its status before another action.') }
    finally { clearTimeout(deadline); if (action.current === controller) action.current = null; if (owner.current === session) setBusy(false) }
  }
  const ready = status?.state === 'synced' && status?.running
  const StateIcon = ready ? IconShieldCheck : status?.state === 'failed' ? IconAlertTriangle : IconClock
  return <Card>
    <CardHeader><CardTitle className="flex items-center gap-2"><IconWorld className="size-5 text-blue-500" />LAN DNS</CardTitle><CardDescription>Router DNS stays available for direct traffic when Xray stops. VPN names resolve through the VPN without a direct fallback.</CardDescription></CardHeader>
    <CardContent className="space-y-4">
      <div className="flex flex-wrap items-center gap-3"><Badge variant="outline" className={ready ? 'border-emerald-500/40 text-emerald-600 dark:text-emerald-400' : status?.state === 'failed' ? 'border-amber-500/40 text-amber-600 dark:text-amber-400' : ''}><StateIcon className="size-4" />{labels[status?.state] || 'Loading'}</Badge>{status && status.state !== 'unconfigured' && <span className="text-sm text-muted-foreground">DNS service {status.running ? 'running' : 'not confirmed'}</span>}</div>
      {status?.state === 'unconfigured' ? <p className="text-sm text-muted-foreground">Independent LAN DNS is not installed. The editor below configures internal Xray DNS.</p> : <>
        <dl className="grid max-w-2xl gap-4 sm:grid-cols-2"><div><dt className="text-sm text-muted-foreground">Domain rules</dt><dd className="font-medium">{status?.entries?.toLocaleString() ?? '—'}</dd></div><div><dt className="text-sm text-muted-foreground">Last synchronization</dt><dd className="font-medium">{status?.lastSync && !status.lastSync.startsWith('0001') ? new Date(status.lastSync).toLocaleString() : '—'}</dd></div></dl>
        <p className="text-sm text-muted-foreground">Applied routing and VPN resolver changes synchronize automatically. Native geodata updates are detected while the panel is running; the resolver keeps its last rules if the panel stops. IP and protocol rules apply to traffic, not DNS names.</p>
        {status?.message && <p role="status" className="text-sm text-amber-600 dark:text-amber-400">{status.message}</p>}
        <Button variant="outline" disabled={busy || !status || status.state === 'pending'} onClick={synchronize}><IconRefresh className={busy ? 'animate-spin' : ''} />{busy ? 'Checking DNS…' : 'Check and synchronize'}</Button>
      </>}
      {notice && <p role="status" className="text-sm text-amber-600 dark:text-amber-400">{notice}</p>}
    </CardContent>
  </Card>
}
