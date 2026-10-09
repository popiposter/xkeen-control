import { useEffect, useState } from 'react'
import { IconWorld, IconRefresh } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

const labels = { synced: 'Legacy resolver active', checking: 'Checking', pending: 'Waiting for Apply', failed: 'Needs attention', unconfigured: 'No separate resolver', retired: 'Retired' }

export function SplitDNSSection({ csrfToken, onUnauthorized, readbackKey }) {
  const [status, setStatus] = useState(null)
  const [notice, setNotice] = useState('')
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    let alive = true
    setStatus(null)
    setNotice('')
    const read = async () => {
      try {
        const response = await fetch('/api/v1/dns/split', { credentials: 'same-origin', cache: 'no-store', signal: controller.signal })
        if (!alive) return
        if (response.status === 401) { onUnauthorized?.(); return }
        if (!response.ok) throw new Error('Unavailable')
        const value = await response.json()
        if (alive) setStatus(value)
      } catch (error) { if (alive && error.name !== 'AbortError') setNotice('DNS status is unavailable.') }
    }
    void read()
    return () => { alive = false; controller.abort() }
  }, [csrfToken, onUnauthorized, readbackKey, refresh])
  const legacy = status && !['unconfigured', 'retired'].includes(status.state)
  return <Card>
    <CardHeader><CardTitle className="flex items-center gap-2"><IconWorld className="size-5 text-blue-500" />Router DNS</CardTitle><CardDescription>New installations keep the router’s normal DNS settings. No separate resolver or DNS-over-VPN rules are added.</CardDescription></CardHeader>
    <CardContent className="space-y-4">
      <Badge variant="outline">{labels[status?.state] || (notice ? 'Unavailable' : 'Loading')}</Badge>
      {legacy ? <>
        <p className="text-sm text-muted-foreground">A legacy LAN resolver is still configured. Its status is read-only here. Complete the supported DNS retirement before stopping it; router clients may still depend on it.</p>
        <p className="text-sm text-muted-foreground">DNS service {status.running ? 'running' : 'not confirmed'}</p>
        {status.message && <p role="status" className="text-sm text-amber-600 dark:text-amber-400">{status.message}</p>}
      </> : status && <p className="text-sm text-muted-foreground">The native editor below controls Xray configuration only. Router DNS is managed in Keenetic.</p>}
      <Button variant="outline" onClick={() => setRefresh(value => value + 1)}><IconRefresh />Refresh DNS status</Button>
      {notice && <p role="status" className="text-sm text-amber-600 dark:text-amber-400">{notice}</p>}
    </CardContent>
  </Card>
}
