import { useCallback, useEffect, useRef, useState } from 'react'

const authorityStates = ['unconfigured', 'configured', 'unavailable']
const deliveryStates = ['idle', 'delivered', 'failed']
const safeCodes = ['authority-unavailable', 'invalid-request', 'unconfigured', 'disabled', 'dns-unavailable', 'unsafe-destination', 'timeout', 'transport-failed', 'provider-rejected']

const safeStatus = (value) => {
  if (!value || value.provider !== 'telegram' || typeof value.configured !== 'boolean' || typeof value.enabled !== 'boolean'
    || !authorityStates.includes(value.authorityState) || !deliveryStates.includes(value.deliveryState)) throw new Error('unavailable')
  return {
    configured: value.configured, enabled: value.enabled, authorityState: value.authorityState, deliveryState: value.deliveryState,
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
  useEffect(() => { setToken(''); setChat('') }, [sessionKey])
  const { status, pending, message, run } = controller
  const configure = (event) => {
    event.preventDefault()
    const credentials = { botToken: token, chatId: chat }
    setToken(''); setChat('') // Clear on submission, including rejected/lost responses.
    void run('/configure', credentials)
  }
  return <section className="panel system-panel-card" aria-label="Notifications">
    <div><span className="panel-label">Notifications</span><h2>Outbound Telegram alerts</h2><p className="muted">Update alerts only. Configure, test, then enable delivery. Credentials are never read back.</p></div>
    {status && <div className="system-facts-grid">
      <div><span>Authority</span><strong>{status.authorityState}</strong></div>
      <div><span>Delivery</span><strong>{status.enabled ? 'Enabled' : 'Disabled'}</strong></div>
      <div><span>Last delivery</span><strong>{status.deliveryState}</strong></div>
      <div><span>Last attempt</span><strong>{status.lastAttemptAt}</strong></div>
      <div><span>Delivered at</span><strong>{status.lastDeliveredAt}</strong></div>
      {status.errorCode && <div><span>Delivery error</span><strong>{status.errorCode}</strong></div>}
    </div>}
    <form className="system-password-form" onSubmit={configure} autoComplete="off">
      <label>Telegram bot token<input type="password" autoComplete="off" maxLength="150" value={token} onChange={(event) => setToken(event.target.value)} disabled={pending} /></label>
      <label>Telegram chat ID<input type="password" autoComplete="off" maxLength="21" value={chat} onChange={(event) => setChat(event.target.value)} disabled={pending} /></label>
      <button type="submit" disabled={pending || !token || !chat}>Configure notifications</button>
    </form>
    <div className="system-card-actions">
      <button type="button" onClick={() => void run('/test', {})} disabled={pending || !status?.configured}>Send test notification</button>
      <button type="button" onClick={() => void run('/enabled', { enabled: !status?.enabled })} disabled={pending || !status?.configured}>{status?.enabled ? 'Disable notifications' : 'Enable notifications'}</button>
      <button type="button" className="ghost" onClick={() => void run('/clear', {})} disabled={pending || status?.authorityState === 'unconfigured'}>Clear notifications</button>
      <button type="button" className="ghost" onClick={() => void run()} disabled={pending}>Refresh notifications</button>
    </div>
    {message && <p role="status">{message}</p>}
    <p className="muted">Separate panel-local credentials; backups exclude them. Reconfigure after reinstall if needed.</p>
  </section>
}
