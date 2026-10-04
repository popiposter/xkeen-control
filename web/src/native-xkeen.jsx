import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { NativeCommands } from './native-commands'
import { StatusBadge } from './status-ui'

export function NativeXkeenStatus({ facts = {}, onOpenNodes }) {
  const installed = facts?.installation === 'available'
  const attached = facts?.panelIntegration === 'available'
  const missing = facts?.installation === 'missing'
  const attachable = installed && facts?.panelIntegration === 'missing'
  const running = installed && facts.xrayRunning === true
  const uncertain = !installed && !missing || installed && !running && !attached && !attachable
  const title = running ? 'XKeen is running' : uncertain ? 'Check XKeen status' : missing ? 'Install XKeen first' : attachable ? 'Connect the panel to XKeen' : 'Add your VPN profiles'
  return <Card role="region" aria-label="Native XKeen">
    <CardHeader><div className="flex flex-wrap items-center justify-between gap-3"><CardTitle><h2>{title}</h2></CardTitle><StatusBadge tone={installed && facts.xrayRunning ? 'success' : 'warning'}>{installed ? `XKeen ${facts.version} - ${facts.channel}` : 'Installation not confirmed'}</StatusBadge></div>
      <CardDescription>{running || installed && attached ? 'XKeen manages the traffic service. The panel manages your profiles and configuration.' : uncertain ? installed ? 'XKeen is installed. Its configuration access needs inspection; refresh status.' : 'The native installation state is unavailable. Inspect XKeen and refresh status.' : !installed ? 'Use the official XKeen installer, then refresh this page.' : 'Connect the panel to the native configuration to manage profiles.'}</CardDescription></CardHeader>
    {installed && attached && onOpenNodes && <CardContent><Button variant="outline" onClick={onOpenNodes}>Manage VPN profiles</Button></CardContent>}
  </Card>
}
export function NativeXkeenSection({ facts, onRefresh, onOpenSystem, csrfToken, onUnauthorized, jobNotification }) {
  return <div className="section-stack">
    <NativeXkeenStatus facts={facts} />
    <Card><CardHeader><CardTitle><h2>XKeen and components</h2></CardTitle><CardDescription>Updates and their schedules are managed by XKeen.</CardDescription></CardHeader><CardContent className="flex flex-col gap-4">
      <dl className="system-facts-grid"><div><dt>Core</dt><dd>{facts?.core || 'Not detected'}</dd></div><div><dt>Geodata files</dt><dd>{facts?.geodataFiles ?? 'Unknown'}</dd></div><div><dt>Native geodata schedule</dt><dd>{facts?.geodataCron === 'available' ? 'Configured in XKeen' : facts?.geodataCron === 'missing' ? 'Not configured' : 'Unknown'}</dd></div></dl>
      <div><Button variant="outline" onClick={onRefresh}>Refresh status</Button></div>
    </CardContent></Card>
    {csrfToken && <NativeCommands csrfToken={csrfToken} onUnauthorized={onUnauthorized} onRefresh={onRefresh} jobNotification={jobNotification} />}
    <Card><CardHeader><CardTitle>Panel updates</CardTitle><CardDescription>XKeen Control updates are separate from native components.</CardDescription></CardHeader><CardContent><Button variant="outline" onClick={onOpenSystem}>Open panel settings</Button></CardContent></Card>
  </div>
}
