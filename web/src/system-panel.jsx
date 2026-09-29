import { useCallback, useEffect, useRef, useState } from 'react'

const EDITABILITY = Object.freeze(['editable', 'drift-detected', 'environment-owned', 'unavailable'])
const SOURCES = Object.freeze(['default', 'file', 'environment'])

class SystemPanelError extends Error {
  constructor(message, { status = 0, code = '', kind = 'response' } = {}) {
    super(message)
    this.status = status
    this.code = code
    this.kind = kind
  }
}

const safeText = (value, max = 256) => typeof value === 'string' && value.length > 0 && value.length <= max

const requestJSON = async (path, options = {}) => {
  let response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...(options.headers || {}) },
      ...options,
    })
  } catch {
    throw new SystemPanelError('The response was lost.', { kind: 'network' })
  }
  let text
  try {
    text = await response.text()
  } catch {
    throw new SystemPanelError('The response was lost.', { status: response.status, kind: 'network' })
  }
  let body = {}
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      if (response.ok) throw new SystemPanelError('The server returned an unreadable response.', { status: response.status, kind: 'malformed' })
    }
  }
  if (!response.ok) {
    throw new SystemPanelError(body.error || `Request failed (${response.status})`, {
      status: response.status,
      code: typeof body.code === 'string' ? body.code : '',
    })
  }
  return body
}

const postJSON = (path, csrfToken, body) => requestJSON(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
  body: JSON.stringify(body),
})

const postEmpty = (path, csrfToken) => requestJSON(path, {
  method: 'POST',
  headers: { 'X-CSRF-Token': csrfToken },
})

const validListener = (value) => value && safeText(value.host, 64)
  && Number.isInteger(value.port) && value.port >= 1 && value.port <= 65535
  && SOURCES.includes(value.source) && EDITABILITY.includes(value.editability)
  && Array.isArray(value.allowedHosts) && value.allowedHosts.length <= 64
  && value.allowedHosts.every((host) => safeText(host, 64))

const validAddress = (value) => value && safeText(value.host, 64)
  && Number.isInteger(value.port) && value.port >= 1 && value.port <= 65535

const validListenerPreview = (value) => value && safeText(value.previewToken)
  && Number.isFinite(Date.parse(value.expiresAt)) && validAddress(value.before) && validAddress(value.after)
  && typeof value.noop === 'boolean' && safeText(value.reconnectClassification, 64)
  && value.restartRequired === !value.noop && value.sessionInvalidated === !value.noop && value.loginRequired === !value.noop

const validListenerApply = (value) => value && typeof value.accepted === 'boolean' && safeText(value.state, 64)
  && validAddress(value.before) && validAddress(value.after) && typeof value.noop === 'boolean'
  && value.accepted === !value.noop
  && safeText(value.reconnectClassification, 64)
  && value.restartRequired === !value.noop && value.sessionInvalidated === !value.noop && value.loginRequired === !value.noop

const validHandoff = (value) => value && value.accepted === true && safeText(value.state, 64)

const validUpdate = (value) => value && value.installed && typeof value.installed === 'object'
  && safeText(value.channel, 16) && value.policy && typeof value.policy === 'object'
  && safeText(value.policy.channel, 16) && ['manual', 'notify', 'auto-stable'].includes(value.policy.mode)
  && Number.isInteger(value.policy.checkCadenceMinutes)
  && typeof value.rollbackAvailable === 'boolean'
  && typeof value.signingKeyConfigured === 'boolean'
  && (value.rollbackVerificationRequired == null || typeof value.rollbackVerificationRequired === 'boolean')
  && (value.latestCompatibleVersion == null || safeText(value.latestCompatibleVersion, 64))
  && (value.latestChannel == null || ['stable', 'beta'].includes(value.latestChannel))

const addressText = (address) => {
  if (!address) return '—'
  return address.host.includes(':') ? `[${address.host}]:${address.port}` : `${address.host}:${address.port}`
}

const sourceLabel = (source) => source === 'environment' ? 'Environment override' : source === 'file' ? 'Persisted listener file' : 'Source default'
const editabilityLabel = (value) => value === 'editable' ? 'Editable' : value === 'environment-owned' ? 'Environment-owned' : value === 'drift-detected' ? 'Drift detected' : 'Unavailable'
const modeLabel = (mode) => mode === 'notify' ? 'Notify metadata' : mode === 'auto-stable' ? 'Auto-stable metadata' : 'Manual'

const safeCancel = (token, csrfToken) => {
  if (!token || !csrfToken) return
  void postJSON('/api/v1/panel/listener/cancel', csrfToken, { previewToken: token }).catch(() => {})
}

export function useSystemPanelController({ csrfToken, lifecycle, onUnauthorized, active }) {
  const [listener, setListener] = useState({ value: null, loading: false, error: '' })
  const [update, setUpdate] = useState({ value: null, loading: false, error: '' })
  const [preview, setPreview] = useState(null)
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState(null)
  const [password, setPassword] = useState({ newPassword: '', confirmation: '', pending: false, error: '' })
  const [channelDraft, setChannelDraft] = useState('stable')
  const [channelPending, setChannelPending] = useState(false)
  const [checkVersion, setCheckVersion] = useState('')
  const [checkPending, setCheckPending] = useState(false)
  const [rollbackPending, setRollbackPending] = useState(false)
  const [listenerHandoffState, setListenerHandoffState] = useState('idle')
  const [handoffState, setHandoffState] = useState('idle')
  const readGate = useRef(false)
  const loaded = useRef(false)
  const previewRef = useRef(preview)
  const previewGate = useRef(false)
  const applyGate = useRef(false)
  const listenerHandoffGate = useRef(false)
  const handoffGate = useRef(false)
  const epoch = useRef(0)
  const activeRef = useRef(active)
  const csrfRef = useRef(csrfToken)
  const sessionCSRFRef = useRef(csrfToken)

  activeRef.current = active
  csrfRef.current = csrfToken
  previewRef.current = preview

  const clearPreview = useCallback((cancel = true) => {
    const token = previewRef.current?.previewToken
    previewRef.current = null
    setPreview(null)
    previewGate.current = false
    if (cancel) safeCancel(token, csrfRef.current)
  }, [])

  const load = useCallback(async ({ force = false } = {}) => {
    if (readGate.current || (!force && loaded.current)) return false
    readGate.current = true
    const requestEpoch = epoch.current
    setListener((current) => ({ ...current, loading: true, error: '' }))
    setUpdate((current) => ({ ...current, loading: true, error: '' }))
    try {
      const [listenerValue, updateValue] = await Promise.all([
        requestJSON('/api/v1/panel/listener'),
        requestJSON('/api/v1/update'),
      ])
      if (!validListener(listenerValue) || !validUpdate(updateValue)) throw new SystemPanelError('The System / Panel projection is invalid.', { kind: 'malformed' })
      if (requestEpoch !== epoch.current) return false
      loaded.current = true
      setListener({ value: listenerValue, loading: false, error: '' })
      setUpdate({ value: updateValue, loading: false, error: '' })
      setChannelDraft(updateValue.policy.channel)
      if (listenerHandoffGate.current) {
        listenerHandoffGate.current = false
        setListenerHandoffState('idle')
        setResult(null)
      }
      return true
    } catch (cause) {
      if (requestEpoch !== epoch.current) return false
      if (cause.status === 401) onUnauthorized()
      setListener((current) => ({ ...current, loading: false, error: 'Management listener state is unavailable.' }))
      setUpdate((current) => ({ ...current, loading: false, error: 'Signed panel release state is unavailable.' }))
      return false
    } finally {
      readGate.current = false
    }
  }, [onUnauthorized])

  const refresh = useCallback(() => {
    clearPreview()
    void load({ force: true })
  }, [clearPreview, load])

  const previewListener = useCallback(async () => {
    const current = listener.value
    const host = current?.selectedHost || current?.host
    if (listenerHandoffGate.current || listenerHandoffState !== 'idle' || previewGate.current || applyGate.current || pending || !current || current.editability !== 'editable' || !host || host === current.host || lifecycle?.maintenance || lifecycle?.applying) return
    previewGate.current = true
    const requestEpoch = epoch.current
    setResult(null)
    try {
      const value = await postJSON('/api/v1/panel/listener/preview', csrfToken, { host })
      if (!validListenerPreview(value)) throw new SystemPanelError('The listener Preview response is invalid.', { kind: 'malformed' })
      if (requestEpoch !== epoch.current || !activeRef.current) {
        safeCancel(value.previewToken, csrfToken)
        return
      }
      previewRef.current = value
      setPreview(value)
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      setResult({ tone: 'error', title: 'Listener Preview unavailable', message: cause.code === 'drift-detected' ? 'The persisted listener authority changed. Refresh before another attempt.' : cause.message })
    } finally {
      previewGate.current = false
    }
  }, [csrfToken, lifecycle, listener.value, listenerHandoffState, onUnauthorized, pending])

  const chooseHost = useCallback((host) => {
    setListener((current) => current.value ? { ...current, value: { ...current.value, selectedHost: host } } : current)
    clearPreview()
    setResult(null)
  }, [clearPreview])

  const applyListener = useCallback(async () => {
    const current = previewRef.current
    if (!current || listenerHandoffGate.current || listenerHandoffState !== 'idle' || previewGate.current || applyGate.current || lifecycle?.maintenance || lifecycle?.applying) return
    applyGate.current = true
    previewRef.current = null
    setPreview(null)
    setPending(true)
    try {
      const value = await postJSON('/api/v1/panel/listener/apply', csrfToken, { previewToken: current.previewToken })
      if (!validListenerApply(value)) throw new SystemPanelError('The listener Apply response is invalid.', { kind: 'malformed' })
      if (value.noop) {
        setResult({ tone: 'success', title: 'Listener already effective', message: 'No listener file or restart was requested.' })
      } else {
        listenerHandoffGate.current = true
        setListenerHandoffState('sent')
        setResult({ tone: 'warning', title: 'Listener rebind handoff started', message: 'The panel will restart at the selected address. This response proves helper handoff only; reconnect and verify, then sign in again.' })
      }
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      if (!current.noop && (cause.kind === 'network' || cause.kind === 'malformed')) {
        listenerHandoffGate.current = true
        setListenerHandoffState('unknown')
        setResult({ tone: 'warning', title: 'Listener rebind outcome is unknown', message: 'The Apply request was sent but its outcome is unproven. Reconnect and verify the active listener, then run a fresh listener read before another Preview or Apply. The sent handoff was not canceled or replayed.' })
      } else {
        setResult({ tone: 'error', title: 'Listener rebind was not started', message: cause.message })
      }
    } finally {
      setPending(false)
      applyGate.current = false
    }
  }, [csrfToken, lifecycle, listenerHandoffState, onUnauthorized])

  const cancelPreview = useCallback(() => {
    const token = previewRef.current?.previewToken
    clearPreview(false)
    if (token) void postJSON('/api/v1/panel/listener/cancel', csrfToken, { previewToken: token }).catch(() => {})
  }, [clearPreview, csrfToken])

  const saveChannel = useCallback(async () => {
    const current = update.value
    if (!current || channelPending || channelDraft === current.policy.channel) return
    setChannelPending(true)
    setResult(null)
    try {
      const value = await postJSON('/api/v1/update/policy', csrfToken, { ...current.policy, channel: channelDraft })
      if (!validUpdate(value)) throw new SystemPanelError('The update policy response is invalid.', { kind: 'malformed' })
      setUpdate({ value, loading: false, error: '' })
      setChannelDraft(value.policy.channel)
      setCheckVersion('')
      setResult({ tone: 'success', title: 'Release channel saved', message: 'The previously checked candidate was cleared. Run an explicit Check before Apply.' })
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      setResult({ tone: 'error', title: 'Release channel was not saved', message: cause.message })
    } finally {
      setChannelPending(false)
    }
  }, [channelDraft, channelPending, csrfToken, onUnauthorized, update.value])

  const checkUpdate = useCallback(async () => {
    const current = update.value
    if (!current || checkPending) return
    const channel = current.policy.channel
    const version = channel === 'beta' ? checkVersion.trim() : checkVersion.trim()
    if (channel === 'beta' && !version) {
      setResult({ tone: 'warning', title: 'Beta Check needs a version', message: 'Enter the explicit beta version before checking.' })
      return
    }
    setCheckPending(true)
    setResult(null)
    try {
      const value = await postJSON('/api/v1/update/check', csrfToken, { channel, ...(version ? { version } : {}) })
      if (!validUpdate(value)) throw new SystemPanelError('The release Check response is invalid.', { kind: 'malformed' })
      setUpdate({ value, loading: false, error: '' })
      setCheckVersion(value.latestCompatibleVersion || version)
      handoffGate.current = false
      setHandoffState('idle')
      setResult({ tone: 'success', title: 'Explicit release Check completed', message: `Checked ${value.latestChannel || channel} candidate ${value.latestCompatibleVersion || 'latest'} from ${value.latestSource || 'the signed release source'}.` })
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      setResult({ tone: 'error', title: 'Release Check failed', message: cause.message })
    } finally {
      setCheckPending(false)
    }
  }, [checkPending, checkVersion, csrfToken, onUnauthorized, update.value])

  const applyUpdate = useCallback(async () => {
    const current = update.value
    const version = current?.latestCompatibleVersion
    if (!current || !version || current.latestChannel !== current.policy.channel || current.latestSource === '' || current.latestSource == null || rollbackPending || handoffGate.current || handoffState !== 'idle' || current.rollbackVerificationRequired) return
    handoffGate.current = true
    setHandoffState('update-unknown')
    setUpdate((currentState) => currentState.value ? {
      ...currentState,
      value: { ...currentState.value, latestCompatibleVersion: null, latestChannel: null, latestSource: '', latestSourceCommit: '', releaseNotesUrl: '' },
    } : currentState)
    setRollbackPending(true)
    setResult(null)
    try {
      const value = await postJSON('/api/v1/update/apply', csrfToken, { channel: current.policy.channel, version })
      if (!validHandoff(value)) throw new SystemPanelError('The update handoff response is invalid.', { kind: 'malformed' })
      setResult({ tone: 'warning', title: 'Panel update attempt started', message: value.state === 'update-attempt-started' ? 'The checked version was handed to the existing updater. Final install success is not proven by 202; reconnect and verify the installed version. No automatic retry will be sent.' : 'The updater handoff was accepted. Reconnect and verify; final install success is not proven by 202.' })
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      setResult({ tone: 'warning', title: 'Panel update outcome is unknown', message: 'The Apply request was sent but its outcome is unproven. Reconnect and verify, then run a fresh explicit Check before another Apply.' })
    } finally {
      setRollbackPending(false)
    }
  }, [csrfToken, handoffState, onUnauthorized, rollbackPending, update.value])

  const rollbackUpdate = useCallback(async () => {
    if (!update.value?.rollbackAvailable || rollbackPending || handoffGate.current || handoffState !== 'idle' || update.value.rollbackVerificationRequired) return
    handoffGate.current = true
    setHandoffState('rollback-unknown')
    setRollbackPending(true)
    setResult(null)
    try {
      const value = await postEmpty('/api/v1/update/rollback', csrfToken)
      if (!validHandoff(value)) throw new SystemPanelError('The rollback handoff response is invalid.', { kind: 'malformed' })
      setResult({ tone: 'warning', title: 'Panel rollback attempt started', message: value.state === 'rollback-attempt-started' ? 'The retained generation was handed to the updater. Reconnect and verify; final rollback success is not proven by 202.' : 'Rollback handoff was accepted. Reconnect and verify the retained generation.' })
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      if (cause.kind === 'network' || cause.kind === 'malformed') {
        setResult({ tone: 'warning', title: 'Panel rollback outcome is unknown', message: 'The rollback request was sent but its outcome is unproven. Reconnect and verify the retained generation before any further action.' })
      } else {
        handoffGate.current = false
        setHandoffState('idle')
        setResult({ tone: 'error', title: 'Panel rollback was rejected', message: 'The server rejected this rollback before handoff. No rollback handoff was started; you may retry.' })
      }
    } finally {
      setRollbackPending(false)
    }
  }, [csrfToken, handoffState, onUnauthorized, rollbackPending, update.value])

  const replacePassword = useCallback(async (event) => {
    event.preventDefault()
    if (password.pending) return
    if (!password.newPassword || password.newPassword !== password.confirmation) {
      setPassword((current) => ({ ...current, newPassword: '', confirmation: '', error: 'Enter matching new passwords.' }))
      return
    }
    setPassword((current) => ({ ...current, pending: true, error: '' }))
    try {
      await postJSON('/api/v1/session/password', csrfToken, { newPassword: password.newPassword })
      // The server invalidates all sessions and RAM previews. Do not leave the
      // old Dashboard mounted or synthesize a local success state.
      onUnauthorized()
    } catch (cause) {
      if (cause.status === 401) onUnauthorized()
      setPassword({ newPassword: '', confirmation: '', pending: false, error: cause.message })
    }
  }, [csrfToken, onUnauthorized, password])

  useEffect(() => {
    if (active) {
      void load()
      return undefined
    }
    epoch.current++
    loaded.current = false
    clearPreview()
    return undefined
  }, [active, clearPreview, load])

  useEffect(() => {
    if (!preview?.expiresAt) return undefined
    const delay = Math.max(0, Date.parse(preview.expiresAt) - Date.now())
    const timer = window.setTimeout(() => {
      if (previewRef.current?.previewToken === preview.previewToken) {
        clearPreview()
        setResult({ tone: 'warning', title: 'Listener Preview expired', message: 'The token was discarded. Create a fresh Preview.' })
      }
    }, Math.min(delay, 2_147_483_647))
    return () => window.clearTimeout(timer)
  }, [clearPreview, preview])

  useEffect(() => {
    if (sessionCSRFRef.current === csrfToken) return
    epoch.current++
    loaded.current = false
    clearPreview()
    setListener({ value: null, loading: false, error: '' })
    setUpdate({ value: null, loading: false, error: '' })
    setResult(null)
    setChannelDraft('stable')
    setCheckVersion('')
    sessionCSRFRef.current = csrfToken
  }, [clearPreview, csrfToken])

  const selectedHost = listener.value?.selectedHost || listener.value?.host || ''
  const canPreviewListener = listener.value?.editability === 'editable' && selectedHost && selectedHost !== listener.value.host && !listenerHandoffGate.current && listenerHandoffState === 'idle' && !pending && !lifecycle?.maintenance && !lifecycle?.applying
  const checkedCandidate = Boolean(update.value?.latestCompatibleVersion)
    && update.value.latestChannel === update.value.policy.channel
    && update.value.latestSource
    && handoffState === 'idle'
    && !update.value.rollbackVerificationRequired

  return {
    listener: listener.value ? { ...listener.value, selectedHost } : null,
    update: update.value,
    listenerLoading: listener.loading,
    updateLoading: update.loading,
    listenerError: listener.error,
    updateError: update.error,
    preview,
    pending,
    result,
    password,
    setPassword,
    replacePassword,
    channelDraft,
    setChannelDraft,
    channelPending,
    saveChannel,
    checkVersion,
    setCheckVersion,
    checkPending,
    checkUpdate,
    checkedCandidate,
    rollbackPending,
    listenerHandoffState,
    handoffState,
    applyUpdate,
    rollbackUpdate,
    chooseHost,
    refresh,
    previewListener: () => { if (canPreviewListener) void previewListener() },
    applyListener,
    cancelPreview,
  }
}

export function SystemPanelSection({ controller, status, onOpenComponents, onOpenBackup }) {
  const listener = controller.listener
  const update = controller.update
  const lifecycleBlocked = !status?.lifecycle || status.lifecycle.maintenance || status.lifecycle.applying
  return <div className="section-stack system-panel-section">
    {controller.result && <div className={`notice ${controller.result.tone}`} role={controller.result.tone === 'error' ? 'alert' : 'status'}><strong>{controller.result.title}</strong> {controller.result.message}</div>}
    <div className="system-panel-grid">
      <section className="panel system-panel-card listener-card" aria-label="Management listener">
        <div className="system-card-heading"><div><span className="panel-label">Management listener</span><h2>{listener ? addressText(listener) : 'Reading listener…'}</h2><p className="muted">The active process bind is shown. A changed file is never adopted until a typed rebind is applied.</p></div>{listener && <span className={`chip ${listener.editability === 'editable' ? 'green' : 'amber'}`}>{editabilityLabel(listener.editability)}</span>}</div>
        {listener && <>
          <div className="system-facts-grid"><Fact label="Source" value={sourceLabel(listener.source)} /><Fact label="Port" value={listener.port} /><Fact label="Allowed hosts" value={listener.allowedHosts.length ? listener.allowedHosts.join(', ') : 'Unavailable'} /></div>
          {listener.editability === 'environment-owned' && <p className="system-blocked" role="alert">An inherited XKEEN_CONTROL_LISTEN environment override owns this bind. Persisted listener changes are read-only.</p>}
          {listener.editability === 'drift-detected' && <p className="system-blocked" role="alert">The listener file changed after startup. Refresh may inspect the drift, but Preview and Apply remain blocked.</p>}
          {listener.editability === 'unavailable' && <p className="system-blocked" role="alert">The local interface catalog or listener authority is unavailable. Mutation is disabled.</p>}
          {controller.listenerHandoffState === 'sent' && <p className="system-blocked" role="alert">The listener rebind handoff started. Reconnect and verify the active listener, then use Refresh for a successful fresh listener read before another Preview or Apply. The sent handoff will not be canceled or replayed.</p>}
          {controller.listenerHandoffState === 'unknown' && <p className="system-blocked" role="alert">The listener rebind outcome is unknown. Reconnect and verify the active listener, then use Refresh for a successful fresh listener read before another Preview or Apply. The sent handoff was not canceled or replayed.</p>}
          <label className="system-select-field">New management host<select aria-label="New management host" value={listener.selectedHost} onChange={(event) => controller.chooseHost(event.target.value)} disabled={listener.editability !== 'editable' || controller.pending || lifecycleBlocked || controller.listenerHandoffState !== 'idle'}><option value={listener.host}>{listener.host} (current)</option>{listener.allowedHosts.filter((host) => host !== listener.host).map((host) => <option key={host} value={host}>{host}</option>)}</select></label>
          <div className="system-card-actions"><button className="ghost" type="button" onClick={controller.refresh} disabled={controller.pending || controller.listenerLoading}>Refresh</button><button type="button" onClick={controller.previewListener} disabled={!controller.listener || !controller.listener.selectedHost || controller.listener.selectedHost === controller.listener.host || controller.listener.editability !== 'editable' || controller.pending || lifecycleBlocked || controller.listenerHandoffState !== 'idle'}>Preview rebind</button></div>
        </>}
        {controller.preview && <ListenerPreview preview={controller.preview} busy={controller.pending || lifecycleBlocked} onCancel={controller.cancelPreview} onApply={controller.applyListener} />}
      </section>

      <section className="panel system-panel-card" aria-label="Panel password">
        <div><span className="panel-label">Panel password</span><h2>Replace credential</h2><p className="muted">The bcrypt hash never enters the browser. Success invalidates every session and returns to login.</p></div>
        <form className="system-password-form" onSubmit={controller.replacePassword}>
          <label>New panel password<input type="password" autoComplete="new-password" value={controller.password.newPassword} onChange={(event) => controller.setPassword((current) => ({ ...current, newPassword: event.target.value, error: '' }))} disabled={controller.password.pending} /></label>
          <label>Confirm new password<input type="password" autoComplete="new-password" value={controller.password.confirmation} onChange={(event) => controller.setPassword((current) => ({ ...current, confirmation: event.target.value, error: '' }))} disabled={controller.password.pending} /></label>
          {controller.password.error && <p className="warning" role="alert">{controller.password.error}</p>}
          <button type="submit" disabled={controller.password.pending}>{controller.password.pending ? 'Replacing…' : 'Replace password'}</button>
        </form>
      </section>

      <section className="panel system-panel-card" aria-label="Signed panel release">
        <div className="system-card-heading"><div><span className="panel-label">Signed panel release</span><h2>{update?.installed?.version || 'Unavailable'}</h2><p className="muted">Checks use the fixed signed release source. Apply is pinned to the exact checked version.</p></div>{update && <span className={`chip ${update.signingKeyConfigured ? 'green' : 'amber'}`}>{update.signingKeyConfigured ? 'Signing key configured' : 'Signing key unavailable'}</span>}</div>
        {update && <>
          <div className="system-facts-grid"><Fact label="Installed source" value={update.installed?.sourceCommit ? 'Signed release commit' : 'Unknown'} /><Fact label="Effective channel" value={update.policy.channel} /><Fact label="Rollback" value={update.rollbackVerificationRequired ? 'Verify required' : update.rollbackAvailable ? 'Available' : 'None'} /><Fact label="Policy mode" value={modeLabel(update.policy.mode)} /></div>
          {update.policy.mode !== 'manual' && <p className="system-blocked">{modeLabel(update.policy.mode)} is persisted metadata only. No automatic panel update scheduler is active.</p>}
          {controller.handoffState === 'update-unknown' && <p className="system-blocked" role="alert">The panel update handoff outcome is unknown. Reconnect and verify the installed version, then run a fresh explicit Check before another Apply.</p>}
          {controller.handoffState === 'rollback-unknown' && <p className="system-blocked" role="alert">The panel rollback outcome is unknown. Reconnect and verify the retained generation before another action.</p>}
          {update.rollbackVerificationRequired && <p className="system-blocked" role="alert">A previous rollback handoff is unproven in this process. Verification is required; no replay is available.</p>}
          <label className="system-select-field">Effective release channel<select aria-label="Effective release channel" value={controller.channelDraft} onChange={(event) => controller.setChannelDraft(event.target.value)} disabled={controller.channelPending || controller.checkPending}><option value="stable">Stable</option><option value="beta">Beta</option></select></label>
          <div className="system-card-actions"><button type="button" className="ghost" onClick={controller.saveChannel} disabled={controller.channelPending || controller.channelDraft === update.policy.channel}>{controller.channelPending ? 'Saving…' : 'Save channel'}</button></div>
          <div className="system-check-row"><label>Beta version (optional for stable)<input aria-label="Beta version" type="text" inputMode="text" maxLength="64" value={controller.checkVersion} onChange={(event) => controller.setCheckVersion(event.target.value)} disabled={controller.checkPending} /></label><button type="button" onClick={controller.checkUpdate} disabled={controller.checkPending}>{controller.checkPending ? 'Checking…' : 'Check fixed release'}</button></div>
          <div className="system-facts-grid"><Fact label="Latest checked" value={update.latestCompatibleVersion || 'Not checked'} /><Fact label="Checked channel" value={update.latestChannel || '—'} /><Fact label="Checked source" value={update.latestSource || '—'} /><Fact label="Last check" value={update.lastCheckAt ? new Date(update.lastCheckAt).toLocaleString() : '—'} /></div>
          {update.releaseNotesUrl && <p><a href={update.releaseNotesUrl} target="_blank" rel="noreferrer">Checked release notes</a></p>}
          <div className="system-card-actions"><button type="button" onClick={controller.applyUpdate} disabled={!controller.checkedCandidate || controller.rollbackPending}>{controller.rollbackPending && controller.handoffState === 'update-unknown' ? 'Verifying…' : 'Apply checked release'}</button><button className="ghost" type="button" onClick={controller.rollbackUpdate} disabled={!update.rollbackAvailable || update.rollbackVerificationRequired || controller.rollbackPending || controller.handoffState !== 'idle'}>{controller.rollbackPending && controller.handoffState === 'rollback-unknown' ? 'Verifying…' : 'Rollback retained release'}</button></div>
        </>}
        {controller.updateError && <p className="system-blocked" role="alert">{controller.updateError}</p>}
      </section>

      <section className="panel system-panel-card" aria-label="Source-owned runtime facts">
        <div><span className="panel-label">Source-owned/runtime facts</span><h2>Operational boundary</h2><p className="muted">System / Panel summarizes safe facts owned by the running control plane. Raw DNS, routing, resolver and generated-outbound data stay in their owning workspaces.</p></div>
        <div className="system-facts-grid"><Fact label="Control plane" value={status?.controlPlane?.version || 'dev'} /><Fact label="Runtime" value={status?.setup?.runtime || 'unknown'} /><Fact label="XKeen" value={status?.xkeen?.running ? 'Running' : 'Not detected'} /><Fact label="Xray" value={status?.xray?.running ? 'Running' : 'Not detected'} /><Fact label="Observatory" value={status?.observatory?.apiReachable ? 'Reachable' : 'Degraded'} /><Fact label="Uptime" value={formatUptime(status?.controlPlane?.uptimeSeconds)} /></div>
      </section>

      <section className="panel system-panel-card system-navigation-card" aria-label="System workspace navigation"><span className="panel-label">Existing owners</span><h2>Open the owning workspace</h2><div className="system-navigation-actions"><button type="button" onClick={onOpenComponents}>Components / Updates</button><button type="button" className="ghost" onClick={onOpenBackup}>Backup &amp; Restore</button></div></section>
    </div>
  </div>
}

function ListenerPreview({ preview, busy, onCancel, onApply }) {
  const region = useRef(null)
  useEffect(() => { region.current?.focus() }, [])
  return <section ref={region} className="panel system-listener-preview" tabIndex="-1" aria-live="polite" aria-label="Listener rebind Preview"><div className="system-card-heading"><div><span className="panel-label">Semantic Preview</span><h3>{preview.noop ? 'No persistent listener change' : 'Review management listener rebind'}</h3></div><small>Expires {new Date(preview.expiresAt).toLocaleString()}</small></div><div className="system-facts-grid"><Fact label="Before" value={addressText(preview.before)} /><Fact label="After" value={addressText(preview.after)} /><Fact label="Reconnect" value={preview.reconnectClassification} /><Fact label="Session" value={preview.sessionInvalidated ? 'Invalidated' : 'Retained'} /><Fact label="Login" value={preview.loginRequired ? 'Required' : 'Not required'} /></div><div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel Preview</button><button type="button" onClick={onApply} disabled={busy}>{busy ? 'Starting…' : preview.noop ? 'Confirm no-op' : 'Start rebind handoff'}</button></div></section>
}

function Fact({ label, value }) { return <div><span>{label}</span><strong>{value}</strong></div> }
function formatUptime(value) { if (!Number.isFinite(value)) return '—'; const hours = Math.floor(value / 3600); const minutes = Math.floor((value % 3600) / 60); return `${hours}h ${minutes}m` }
