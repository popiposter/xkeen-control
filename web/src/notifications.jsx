import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'

const authorityStates = ['unconfigured', 'configured', 'unavailable']
const deliveryStates = ['idle', 'delivered', 'failed']
const safeCodes = ['authority-unavailable', 'invalid-request', 'unconfigured', 'disabled', 'dns-unavailable', 'unsafe-destination', 'timeout', 'transport-failed', 'provider-rejected']

const safeStatus = (value) => {
  if (!value || value.provider !== 'telegram' || typeof value.configured !== 'boolean' || typeof value.enabled !== 'boolean'
    || !authorityStates.includes(value.authorityState) || !deliveryStates.includes(value.deliveryState)) throw new Error('unavailable')
  return {
    configured: value.configured, enabled: value.enabled, authorityState: value.authorityState, deliveryState: value.deliveryState,
    controlConfigured: value.controlConfigured === true, controlEnabled: value.controlEnabled === true,
    controlState: ['disabled', 'listening', 'failed'].includes(value.controlState) ? value.controlState : 'disabled',
    lastAttemptAt: Number.isFinite(Date.parse(value.lastAttemptAt)) ? new Date(value.lastAttemptAt).toLocaleString() : '—',
    lastDeliveredAt: Number.isFinite(Date.parse(value.lastDeliveredAt)) ? new Date(value.lastDeliveredAt).toLocaleString() : '—',
    errorCode: safeCodes.includes(value.errorCode) ? value.errorCode : '',
  }
}

async function request(path, csrfToken, body) {
  const response = await fetch(`/api/v1/notifications${path}`, {
    credentials: 'same-origin', method: body ? 'POST' : 'GET',
    headers: body ? { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken } : { Accept: 'application/json' },
    ...(body ? { body: JSON.stringify(body) } : {}),
  })
  if (response.status === 401) { const error = new Error('unauthorized'); error.status = 401; throw error }
  if (!response.ok) throw new Error('operation-failed')
  return safeStatus(await response.json())
}

export function useNotifications({ active, csrfToken, onUnauthorized }) {
  const [status, setStatus] = useState(null)
  const [pending, setPending] = useState(false)
  const [message, setMessage] = useState('')
  const gate = useRef(false)
  const epoch = useRef(0)
  const loaded = useRef(false)
  const session = useRef(csrfToken)
  useEffect(() => {
    if (session.current === csrfToken) return
    session.current = csrfToken; epoch.current++; loaded.current = false; gate.current = false; setPending(false); setStatus(null); setMessage('')
  }, [csrfToken])
  const run = useCallback(async (path = '', body) => {
    if (gate.current) return
    gate.current = true; setPending(true); setMessage('')
    const current = epoch.current
    try {
      const value = await request(path, csrfToken, body)
      if (current !== epoch.current) return
      loaded.current = true; setStatus(value)
      if (path) setMessage(path === '/test' ? 'Test notification delivered.' : 'Notification settings saved.')
    } catch (error) {
      if (current !== epoch.current) return
      if (error.status === 401) onUnauthorized()
      setMessage(path ? 'Notification operation failed. Refresh state before retrying.' : 'Notification state unavailable.')
    } finally { if (current === epoch.current) { gate.current = false; setPending(false) } }
  }, [csrfToken, onUnauthorized])
  useEffect(() => { if (active && !loaded.current) void run() }, [active, run])
  useEffect(() => () => { epoch.current++ }, [])
  return { status, pending, message, run }
}

export function NotificationsCard({ controller, sessionKey }) {
  const [token, setToken] = useState('')
  const [chat, setChat] = useState('')
  const [user, setUser] = useState('')
  useEffect(() => { setToken(''); setChat(''); setUser('') }, [sessionKey])
  const { status, pending, message, run } = controller
  const configure = (event) => {
    event.preventDefault()
    const credentials = { botToken: token, chatId: chat }
    setToken(''); setChat('') // Clear on submission, including rejected/lost responses.
    void run('/configure', credentials)
  }
  return <Card role="region" aria-label="Notifications">
    <CardHeader><CardTitle>Telegram</CardTitle><CardDescription>Notifications and native commands from one authorized user and chat.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
    {status && <div className="system-facts-grid">
      <div><span>Authority</span><strong>{status.authorityState}</strong></div>
      <div><span>Delivery</span><strong>{status.enabled ? 'Enabled' : 'Disabled'}</strong></div>
      <div><span>Last delivery</span><strong>{status.deliveryState}</strong></div>
      <div><span>Last attempt</span><strong>{status.lastAttemptAt}</strong></div>
      <div><span>Delivered at</span><strong>{status.lastDeliveredAt}</strong></div>
      {status.errorCode && <div><span>Delivery error</span><strong>{status.errorCode}</strong></div>}
      <div><span>Bot control</span><strong>{status.controlEnabled ? status.controlState : 'Disabled'}</strong></div>
    </div>}
    <form onSubmit={configure} autoComplete="off"><FieldGroup>
      <Field><FieldLabel htmlFor="telegram-token">Telegram bot token</FieldLabel><Input id="telegram-token" type="password" autoComplete="off" maxLength={150} value={token} onChange={(event) => setToken(event.target.value)} disabled={pending} /></Field>
      <Field><FieldLabel htmlFor="telegram-chat">Telegram chat ID</FieldLabel><Input id="telegram-chat" type="text" autoComplete="off" maxLength={21} value={chat} onChange={(event) => setChat(event.target.value)} disabled={pending} /></Field>
      <Button type="submit" disabled={pending || !token || !chat}>Configure notifications</Button>
      </FieldGroup>
    </form>
    <div className="system-card-actions">
      <Button type="button" variant="outline" onClick={() => void run('/test', {})} disabled={pending || !status?.configured}>Send test notification</Button>
      <Button type="button" variant="outline" onClick={() => void run('/enabled', { enabled: !status?.enabled })} disabled={pending || !status?.configured}>{status?.enabled ? 'Disable notifications' : 'Enable notifications'}</Button>
      <Button type="button" variant="ghost" onClick={() => void run('/clear', {})} disabled={pending || status?.authorityState === 'unconfigured'}>Clear notifications</Button>
      <Button type="button" variant="ghost" onClick={() => void run()} disabled={pending}>Refresh notifications</Button>
    </div>
    <form onSubmit={(event) => { event.preventDefault(); const id = user; setUser(''); void run('/control', { enabled: true, allowedUserId: id }) }} autoComplete="off"><FieldGroup>
      <Field><FieldLabel htmlFor="telegram-user">Allowed Telegram user ID</FieldLabel><Input id="telegram-user" inputMode="numeric" autoComplete="off" maxLength={19} value={user} onChange={(event) => setUser(event.target.value)} disabled={pending} /></Field>
      <div className="flex flex-wrap gap-2"><Button type="submit" disabled={pending || !status?.configured || !/^[1-9][0-9]{0,18}$/.test(user)}>Enable bot control</Button><Button type="button" variant="outline" disabled={pending || !status?.controlEnabled} onClick={() => void run('/control', { enabled: false, allowedUserId: '' })}>Disable bot control</Button></div>
    </FieldGroup></form>
    <p className="text-muted-foreground">Commands: /status, /start, /stop, /restart, /update_xkeen, /update_xray, /update_geodata, /refresh. Command acceptance is not a verified result. Inspect the native console in the panel for prompts and final state. Pending configurations must be applied in the editor first.</p>
    {message && <p role="status">{message}</p>}
    <p className="text-muted-foreground">Not included in backups. Reconfiguring credentials disables bot control.</p>
    </CardContent>
  </Card>
}
