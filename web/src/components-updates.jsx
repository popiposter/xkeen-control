import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

const COMPONENTS = [
  { kind: 'panel', label: 'Panel', description: 'Signed control-plane release', informational: true },
  { kind: 'xkeen', label: 'XKeen', description: 'Executable and module generation', channel: 'dev' },
  { kind: 'xray', label: 'Xray', description: 'Traffic data-plane binary', channel: 'stable' },
  { kind: 'geodata', label: 'Geodata', description: 'Complete managed six-item set', channel: 'stable' },
  { kind: 'keeneticos', label: 'KeeneticOS', description: 'Router operating system', informational: true },
  { kind: 'entware', label: 'Entware', description: 'Package environment', informational: true },
]

const COMPONENT_BY_KIND = new Map(COMPONENTS.map((component) => [component.kind, component]))
const POLICY_MODES = Object.freeze(['manual', 'notify', 'off'])
const POLICY_MODE_LABELS = Object.freeze({ manual: 'Manual', notify: 'Notify', off: 'Off' })
const MIN_POLICY_CADENCE_MINUTES = 60
const MAX_POLICY_CADENCE_MINUTES = 7 * 24 * 60
const SAFE_POLICY_CODE = /^[a-z0-9][a-z0-9_-]{0,63}$/
const SAFE_CANDIDATE_IDENTITY = /^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$/

class ComponentRequestError extends Error {
  constructor(message, { status = 0, code = '', reasonCode = '', kind = 'response' } = {}) {
    super(message)
    this.status = status
    this.code = code
    this.reasonCode = reasonCode
    this.kind = kind
  }
}

const CANDIDATE_REJECTION_MESSAGES = Object.freeze({
  'artifact-download': 'The fixed artifact could not be downloaded.',
  'artifact-integrity': 'The downloaded artifact failed its bounded size or digest check.',
  'archive-extract': 'The candidate archive could not be safely extracted.',
  'binary-probe': 'The candidate binary failed the fixed version or architecture probe.',
  'candidate-render': 'The complete candidate configuration could not be rendered.',
  'candidate-config-validation': 'The candidate configuration failed validation.',
  'staging-io': 'The bounded candidate staging operation failed.',
  'candidate-validation': 'The fixed candidate validation did not complete safely.',
})

const requestJSON = async (path, options = {}) => {
  let response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...(options.headers || {}) },
      ...options,
    })
  } catch {
    throw new ComponentRequestError('The response was lost.', { kind: 'network' })
  }

  const text = await response.text()
  let body = {}
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      if (response.ok) {
        throw new ComponentRequestError('The server returned an unreadable response.', { status: response.status, kind: 'malformed' })
      }
    }
  }
  if (!response.ok) {
    throw new ComponentRequestError(body?.error || `Request failed (${response.status})`, {
      status: response.status,
      code: typeof body?.code === 'string' ? body.code : '',
      reasonCode: typeof body?.reasonCode === 'string' ? body.reasonCode : '',
    })
  }
  return body
}

const postJSON = (path, csrfToken, body) => requestJSON(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
  body: JSON.stringify(body),
})

const validInventory = (value) => value?.schemaVersion === 1
  && COMPONENTS.every(({ kind }) => value?.[kind]?.kind === kind)

const validPolicyCode = (value) => typeof value === 'string' && SAFE_POLICY_CODE.test(value)
const validScheduledResult = (value) => value && typeof value === 'object'
  && validPolicyCode(value.state)
  && validPolicyCode(value.installedState)
  && (!value.reasonCode || validPolicyCode(value.reasonCode))
  && (!value.candidateIdentity || SAFE_CANDIDATE_IDENTITY.test(value.candidateIdentity))

const validPolicy = (value) => value?.schemaVersion === 1
  && POLICY_MODES.includes(value.mode)
  && Number.isInteger(value.checkCadenceMinutes)
  && value.checkCadenceMinutes >= MIN_POLICY_CADENCE_MINUTES
  && value.checkCadenceMinutes <= MAX_POLICY_CADENCE_MINUTES
  && value.scheduler && !Array.isArray(value.scheduler)
  && typeof value.scheduler.enabled === 'boolean'
  && validPolicyCode(value.scheduler.state)
  && validPolicyCode(value.scheduler.notificationState)
  && (!value.reasonCode || validPolicyCode(value.reasonCode))
  && (!value.scheduler.lastSkipReason || validPolicyCode(value.scheduler.lastSkipReason))
  && (!value.scheduler.results || typeof value.scheduler.results === 'object' && !Array.isArray(value.scheduler.results) && Object.values(value.scheduler.results).every(validScheduledResult))

const validCheck = (value, component, channel) => value?.schemaVersion === 1
  && value.component === component
  && value.channel === channel
  && typeof value.eligible === 'boolean'
  && typeof value.mutationAvailable === 'boolean'

const validPreview = (value, component, operation, channel) => value?.schemaVersion === 1
  && value.component === component
  && value.operation === operation
  && (operation === 'rollback' ? !value.channel : value.channel === channel)
  && typeof value.previewToken === 'string'
  && value.previewToken.length > 0
  && value.previewToken.length <= 256
  && Number.isFinite(Date.parse(value.expiresAt))
  && (operation === 'rollback' ? value.previous && typeof value.previous === 'object' : value.candidate && typeof value.candidate === 'object')

const validResult = (value, pending) => value?.schemaVersion === 1
  && value.component === pending.component
  && value.operation === pending.operation
  && value.state === (pending.operation === 'rollback' ? 'rolled-back' : 'applied')

const safeCancel = (token, csrfToken) => {
  if (!token || !csrfToken) return
  void postJSON('/api/v1/components/cancel', csrfToken, { previewToken: token }).catch(() => {})
}

const mutationErrorResult = (cause, pending) => {
  const label = COMPONENT_BY_KIND.get(pending.component)?.label || 'Component'
  switch (cause.code) {
    case 'transaction-restored':
      return { tone: 'warning', title: `${label} ${pending.operation} failed`, message: 'The previous generation was verified as restored. Create a fresh Preview before another attempt.', outcome: 'restored' }
    case 'transaction-unproven':
    case 'rollback-unproven':
      return { tone: 'error', title: `${label} outcome is not proven`, message: 'Do not infer completion from inventory or health alone. Verify the appliance deliberately before creating a new Preview.', outcome: 'unknown' }
    case 'maintenance':
      return { tone: 'error', title: 'Lifecycle maintenance is active', message: 'The request did not produce a proven normal result. Read-only state remains available; do not replay automatically.', outcome: 'maintenance' }
    case 'policy-disabled':
      return { tone: 'warning', title: 'Component updates are disabled', message: 'The effective component policy is Off. Check and update are disabled; rollback remains available. Create a fresh Preview after re-enabling updates.', outcome: 'rejected' }
    case 'preview-expired':
      return { tone: 'warning', title: 'Preview expired', message: 'The one-shot intent is no longer usable. Run a fresh Preview.', outcome: 'rejected' }
    case 'preview-stale':
      return { tone: 'warning', title: 'Preview is stale', message: 'The candidate or retained rollback generation changed. Inspect state and create a fresh Preview.', outcome: 'rejected' }
    case 'no-previous':
      return { tone: 'warning', title: 'No retained generation', message: 'There is no validated one-step rollback target for this component.', outcome: 'rejected' }
    case 'busy':
      return { tone: 'warning', title: 'Lifecycle is busy', message: 'This one-shot request was not accepted. Wait for the current lifecycle work, then create a new Preview.', outcome: 'rejected' }
    case 'metadata-unavailable':
      return { tone: 'warning', title: 'Metadata unavailable', message: 'The fixed upstream metadata could not be verified. No automatic retry was started.', outcome: 'rejected' }
    case 'candidate-rejected':
      {
        const reasonCode = Object.prototype.hasOwnProperty.call(CANDIDATE_REJECTION_MESSAGES, cause.reasonCode) ? cause.reasonCode : ''
        const reason = reasonCode ? ` ${CANDIDATE_REJECTION_MESSAGES[reasonCode]}` : ' The fixed candidate did not pass the server trust boundary.'
        return { tone: 'warning', title: 'Candidate rejected', message: `${reason} No automatic retry was started.`, reasonCode, outcome: 'rejected' }
      }
    case 'invalid-request':
    case 'unavailable':
      return { tone: 'error', title: 'Component operation unavailable', message: 'The server rejected this one-shot request. Inspect current state and create a fresh Preview if appropriate.', outcome: 'rejected' }
    default:
      if (cause.kind === 'network' || cause.kind === 'malformed') {
        return { tone: 'error', title: `${label} outcome is unknown`, message: `${cause.message} Do not infer failure or restoration and do not replay automatically.`, outcome: 'unknown' }
      }
      if (cause.status === 401 || cause.status === 403) {
        return { tone: 'error', title: 'Authorization was not accepted', message: 'The request will not be replayed automatically. Sign in if required, verify state, and create a new Preview.', outcome: 'rejected' }
      }
      return { tone: 'error', title: `${label} result is unavailable`, message: 'The server did not provide a recognized component error code. Treat the result conservatively and do not replay automatically.', outcome: 'unknown' }
  }
}

const restoreFocus = (target, isCurrent = () => true) => {
  window.setTimeout(() => { if (isCurrent()) target?.focus?.() }, 0)
}

export function useComponentsController({ csrfToken, lifecycle, onUnauthorized }) {
  const [inventory, setInventory] = useState({ value: null, observedAt: '', loading: false, error: '' })
  const [policy, setPolicy] = useState({ value: null, loading: false, saving: false, error: '' })
  const [policyDraft, setPolicyDraft] = useState({ mode: 'manual', checkCadenceMinutes: 1440 })
  const [checks, setChecks] = useState({})
  const [requestState, setRequestState] = useState(null)
  const [preview, setPreview] = useState(null)
  const [pending, setPending] = useState(null)
  const [result, setResult] = useState(null)
  const [refreshError, setRefreshError] = useState('')
  const inventoryGate = useRef(false)
  const policyGate = useRef(false)
  const metadataGate = useRef(false)
  const submitGuard = useRef(false)
  const requestSequence = useRef(0)
  const sessionEpoch = useRef(0)
  const hasLoadedInventory = useRef(false)
  const hasLoadedPolicy = useRef(false)
  const previewRef = useRef(null)
  const csrfRef = useRef(csrfToken)
  const sessionCSRFRef = useRef(csrfToken)
  const focusRef = useRef(null)

  previewRef.current = preview
  csrfRef.current = csrfToken

  const lifecycleKnown = lifecycle && typeof lifecycle.maintenance === 'boolean' && typeof lifecycle.applying === 'boolean'
  const lifecycleUnavailable = !lifecycleKnown
  const lifecycleMutationBlocked = lifecycleUnavailable || lifecycle.maintenance || lifecycle.applying || Boolean(pending)

  const loadInventory = useCallback(async ({ force = false } = {}) => {
    if (inventoryGate.current || (!force && hasLoadedInventory.current)) return false
    inventoryGate.current = true
    const epoch = sessionEpoch.current
    setInventory((current) => ({ ...current, loading: true, error: '' }))
    try {
      const value = await requestJSON('/api/v1/components')
      if (!validInventory(value)) throw new ComponentRequestError('Component inventory response is invalid.', { kind: 'malformed' })
      if (epoch !== sessionEpoch.current) return false
      hasLoadedInventory.current = true
      setInventory({ value, observedAt: new Date().toISOString(), loading: false, error: '' })
      return true
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return false
      if (cause.status === 401) onUnauthorized()
      setInventory((current) => ({ ...current, loading: false, error: cause.message || 'Component inventory is unavailable.' }))
      return false
    } finally {
      inventoryGate.current = false
    }
  }, [onUnauthorized])

  const loadPolicy = useCallback(async ({ force = false } = {}) => {
    if (policyGate.current || (!force && hasLoadedPolicy.current)) return false
    policyGate.current = true
    const epoch = sessionEpoch.current
    setPolicy((current) => ({ ...current, loading: true, error: '' }))
    try {
      const value = await requestJSON('/api/v1/components/policy')
      if (!validPolicy(value)) throw new ComponentRequestError('Component policy response is invalid.', { kind: 'malformed' })
      if (epoch !== sessionEpoch.current) return false
      hasLoadedPolicy.current = true
      setPolicy({ value, loading: false, saving: false, error: '' })
      setPolicyDraft({ mode: value.mode, checkCadenceMinutes: value.checkCadenceMinutes })
      return true
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return false
      if (cause.status === 401) onUnauthorized()
      hasLoadedPolicy.current = false
      setPolicy((current) => ({ ...current, value: null, loading: false, saving: false, error: cause.message || 'Component policy is unavailable.' }))
      return false
    } finally {
      policyGate.current = false
    }
  }, [onUnauthorized])

  const savePolicy = useCallback(async () => {
    if (!policy.value || policy.saving) return false
    const cadence = Number(policyDraft.checkCadenceMinutes)
    if (!POLICY_MODES.includes(policyDraft.mode) || !Number.isInteger(cadence) || cadence < MIN_POLICY_CADENCE_MINUTES || cadence > MAX_POLICY_CADENCE_MINUTES) {
      setPolicy((current) => ({ ...current, error: `Check cadence must be an integer from ${MIN_POLICY_CADENCE_MINUTES} to ${MAX_POLICY_CADENCE_MINUTES} minutes.` }))
      return false
    }
    const epoch = sessionEpoch.current
    setPolicy((current) => ({ ...current, saving: true, error: '' }))
    try {
      const value = await postJSON('/api/v1/components/policy', csrfToken, { schemaVersion: 1, mode: policyDraft.mode, checkCadenceMinutes: cadence })
      if (epoch !== sessionEpoch.current) return false
      if (!validPolicy(value)) throw new ComponentRequestError('Component policy response is invalid.', { kind: 'malformed' })
      setPolicy({ value, loading: false, saving: false, error: '' })
      setPolicyDraft({ mode: value.mode, checkCadenceMinutes: value.checkCadenceMinutes })
      hasLoadedPolicy.current = true
      return true
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return false
      if (cause.status === 401) onUnauthorized()
      setPolicy((current) => ({ ...current, saving: false, error: cause.message || 'Component policy was not saved.' }))
      return false
    }
  }, [csrfToken, onUnauthorized, policy.saving, policy.value, policyDraft])

  const policyKnown = Boolean(policy.value)
  const policyOff = policy.value?.mode === 'off'
  const policyDiscoveryBlocked = !policyKnown || policyOff

  const finishMetadataRequest = useCallback((id) => {
    metadataGate.current = false
    setRequestState((current) => current?.id === id ? null : current)
  }, [])

  const checkComponent = useCallback(async (component, channel, trigger) => {
    if (metadataGate.current || pending || preview) return
    if (policyDiscoveryBlocked) {
      setResult({ tone: 'warning', title: policyOff ? 'Component checks are disabled' : 'Component policy unavailable', message: policyOff ? 'The effective component policy is Off. Re-enable component checks to inspect upstream metadata.' : 'The component policy could not be validated. Check is disabled until policy status is available.', outcome: 'check' })
      return
    }
    metadataGate.current = true
    focusRef.current = trigger || null
    const id = ++requestSequence.current
    const epoch = sessionEpoch.current
    setRequestState({ id, kind: 'check', component, canceled: false })
    setResult(null)
    try {
      const value = await postJSON('/api/v1/components/check', csrfToken, { component, channel })
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) return
      if (!validCheck(value, component, channel)) throw new ComponentRequestError('Component check response is invalid.', { kind: 'malformed' })
      setChecks((current) => ({ ...current, [component]: value }))
    } catch (cause) {
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) return
      if (cause.status === 401) onUnauthorized()
      setResult({ tone: 'error', title: 'Component check failed', message: cause.message || 'The explicit metadata check did not complete.', outcome: 'check' })
    } finally {
      finishMetadataRequest(id)
      restoreFocus(focusRef.current)
    }
  }, [csrfToken, finishMetadataRequest, onUnauthorized, pending, policyDiscoveryBlocked, policyOff, preview])

  const previewAction = useCallback(async (component, operation, channel, trigger) => {
    if (metadataGate.current || pending || preview || lifecycleUnavailable || lifecycle.maintenance || lifecycle.applying) return
    if (operation === 'update' && policyDiscoveryBlocked) {
      setResult({ tone: 'warning', title: policyOff ? 'Component updates are disabled' : 'Component policy unavailable', message: policyOff ? 'The effective component policy is Off. Check and update are disabled; rollback remains available.' : 'The component policy could not be validated. Update Preview is disabled until policy status is available.', outcome: 'rejected' })
      return
    }
    metadataGate.current = true
    focusRef.current = trigger || null
    const id = ++requestSequence.current
    const epoch = sessionEpoch.current
    setRequestState({ id, kind: 'preview', component, operation, canceled: false })
    setResult(null)
    try {
      const body = { component, operation, ...(operation === 'update' ? { channel } : {}) }
      const value = await postJSON('/api/v1/components/preview', csrfToken, body)
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) {
        if (typeof value?.previewToken === 'string') safeCancel(value.previewToken, csrfToken)
        return
      }
      if (!validPreview(value, component, operation, channel)) {
        if (typeof value?.previewToken === 'string') safeCancel(value.previewToken, csrfToken)
        throw new ComponentRequestError('Component Preview response is invalid.', { kind: 'malformed' })
      }
      setPreview(value)
    } catch (cause) {
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) return
      if (cause.status === 401) onUnauthorized()
      if (cause.kind === 'network' || cause.kind === 'malformed') {
        setResult({ tone: 'error', title: 'Preview unavailable', message: `${cause.message} No mutation request was submitted.`, outcome: 'preview' })
      } else {
        setResult(mutationErrorResult(cause, { component, operation }))
      }
    } finally {
      finishMetadataRequest(id)
    }
  }, [csrfToken, finishMetadataRequest, lifecycle, lifecycleUnavailable, onUnauthorized, pending, policyDiscoveryBlocked, policyOff, preview])

  const cancelPreview = useCallback((reason = 'canceled') => {
    requestSequence.current++
    const token = previewRef.current?.previewToken
    setPreview(null)
    if (requestState?.kind === 'preview') {
      setRequestState((current) => current ? { ...current, canceled: true } : current)
    }
    safeCancel(token, csrfRef.current)
    setResult(reason === 'expired'
      ? { tone: 'warning', title: 'Preview expired', message: 'The unsent token was discarded. Create a fresh Preview to continue.', outcome: 'preview' }
      : null)
    restoreFocus(focusRef.current)
  }, [requestState?.kind])

  const submitPreview = useCallback(async () => {
    const current = previewRef.current
    if (!current || submitGuard.current || lifecycleUnavailable || lifecycle.maintenance || (lifecycle.applying && !pending)) return
    submitGuard.current = true
    const epoch = sessionEpoch.current
    // The request must finish normally, but its continuations belong only to
    // the initiating authenticated shell/CSRF generation, including finally.
    const isCurrentSession = () => epoch === sessionEpoch.current && csrfRef.current === csrfToken
    const focusTarget = focusRef.current
    const token = current.previewToken
    previewRef.current = null
    const operation = { component: current.component, operation: current.operation, channel: current.channel || '', startedAt: new Date().toISOString() }
    setPreview(null)
    setPending(operation)
    setResult(null)
    setRefreshError('')
    let refreshAfterResult = false
    try {
      const path = current.operation === 'rollback' ? '/api/v1/components/rollback' : '/api/v1/components/apply'
      const value = await postJSON(path, csrfToken, { previewToken: token })
      if (!isCurrentSession()) return
      if (!validResult(value, operation)) {
        throw new ComponentRequestError('The server returned an invalid success result. The operation outcome is unknown.', { kind: 'malformed' })
      }
      const label = COMPONENT_BY_KIND.get(operation.component)?.label || 'Component'
      const target = value.version || value.generation || 'the confirmed target'
      const retention = operation.operation === 'update' ? ' This successful update replaced the retained one-step rollback generation.' : ''
      setResult({ tone: 'success', title: `${label} ${operation.operation} completed`, message: `The server verified ${target}.${retention}`, outcome: 'success' })
      refreshAfterResult = true
    } catch (cause) {
      if (!isCurrentSession()) return
      const mapped = mutationErrorResult(cause, operation)
      setResult(mapped)
      refreshAfterResult = ['unknown', 'restored', 'maintenance'].includes(mapped.outcome)
      if (cause.status === 401) onUnauthorized()
    } finally {
      if (isCurrentSession()) {
        setPending(null)
        submitGuard.current = false
        restoreFocus(focusTarget, isCurrentSession)
      }
    }
    if (refreshAfterResult && isCurrentSession()) {
      const refreshed = await loadInventory({ force: true })
      if (!isCurrentSession()) return
      if (!refreshed) setRefreshError('The operation result is preserved, but the subsequent component inventory refresh failed.')
    }
  }, [csrfToken, lifecycle, lifecycleUnavailable, loadInventory, onUnauthorized, pending])

  useEffect(() => {
    if (!preview?.expiresAt) return undefined
    const delay = Math.max(0, Date.parse(preview.expiresAt) - Date.now())
    const timer = window.setTimeout(() => {
      if (previewRef.current?.previewToken === preview.previewToken) cancelPreview('expired')
    }, Math.min(delay, 2_147_483_647))
    return () => window.clearTimeout(timer)
  }, [cancelPreview, preview])

  useEffect(() => {
    const previousCSRF = sessionCSRFRef.current
    if (previousCSRF === csrfToken) return
    sessionEpoch.current++
    requestSequence.current++
    safeCancel(previewRef.current?.previewToken, previousCSRF)
    previewRef.current = null
    submitGuard.current = false
    setPreview(null)
    setPending(null)
    setResult(null)
    setRefreshError('')
    sessionCSRFRef.current = csrfToken
  }, [csrfToken])

  useEffect(() => () => {
    sessionEpoch.current++
    requestSequence.current++
    safeCancel(previewRef.current?.previewToken, csrfRef.current)
  }, [])

  return useMemo(() => ({
    inventory,
    policy,
    policyDraft,
    checks,
    requestState,
    preview,
    pending,
    result,
    refreshError,
    lifecycleKnown,
    lifecycleUnavailable,
    lifecycleMutationBlocked,
    loadInventory,
    loadPolicy,
    savePolicy,
    setPolicyDraft,
    checkComponent,
    previewAction,
    cancelPreview,
    submitPreview,
    clearResult: () => { setResult(null); setRefreshError('') },
  }), [cancelPreview, checkComponent, checks, inventory, lifecycleKnown, lifecycleMutationBlocked, lifecycleUnavailable, loadInventory, loadPolicy, pending, policy, policyDraft, preview, previewAction, refreshError, requestState, result, savePolicy, submitPreview])
}

const formatTime = (value) => value ? new Date(value).toLocaleString() : '—'
const formatBytes = (value) => value ? `${new Intl.NumberFormat().format(Math.round(value / (1024 * 1024)))} MiB` : '—'
const shortDigest = (value) => value ? `${String(value).slice(0, 12)}…` : '—'
const componentState = (component) => component?.state || 'unknown'
const componentVersion = (component) => {
  if (component?.state === 'missing') return 'Missing'
  if (component?.state !== 'present') return 'Unknown'
  if (component.version && !component.versionUnknown) return component.version
  return 'Present · version unknown'
}

export function ComponentLifecycleNotices({ controller, lifecycle, onOpenComponents }) {
  const pending = controller.pending
  const persistentResult = controller.result?.outcome === 'unknown' || controller.result?.outcome === 'maintenance'
  return <>
    {controller.lifecycleUnavailable && <div className="lifecycle-banner warning" role="status"><div><strong>Lifecycle readiness unavailable</strong><span>Mutation controls are disabled until the coordinator projection is known.</span></div><button className="ghost" type="button" onClick={onOpenComponents}>Open Components</button></div>}
    {lifecycle?.maintenance && <div className="lifecycle-banner danger" role="alert"><div><strong>Lifecycle maintenance</strong><span>Mutation controls are disabled. Inventory and diagnostic views remain available.</span></div><button className="ghost" type="button" onClick={onOpenComponents}>Open Components</button></div>}
    {pending && <div className="lifecycle-banner running" role="status" aria-live="polite"><div><strong>{COMPONENT_BY_KIND.get(pending.component)?.label || pending.component} {pending.operation} is running</strong><span>The synchronous request remains active. Do not reload or submit another lifecycle operation.</span></div><button className="ghost" type="button" onClick={onOpenComponents}>View operation</button></div>}
    {!pending && persistentResult && <div className={`lifecycle-banner ${controller.result.tone === 'error' ? 'danger' : 'warning'}`} role="alert"><div><strong>{controller.result.title}</strong><span>{controller.result.message}</span></div><button className="ghost" type="button" onClick={onOpenComponents}>Review result</button></div>}
  </>
}

export function ComponentsUpdatesSection({ controller, lifecycle, onOpenSystem }) {
  const { inventory, policy, checks, requestState, preview, pending, result, refreshError } = controller
  const values = inventory.value
  const metadataBusy = Boolean(requestState)

  useEffect(() => {
    const timer = window.setTimeout(() => { void controller.loadPolicy() }, 0)
    return () => window.clearTimeout(timer)
  }, [controller.loadPolicy])

  return <div className="section-stack components-section">
    <section className="panel components-heading">
      <div><span className="panel-label">Components / Updates</span><h2>Manual, one component at a time</h2><p>Inventory is read only. Check inspects fixed metadata; Preview creates the exact short-lived intent used by Apply or one-step rollback.</p></div>
      <div className="components-heading-actions"><small>{inventory.observedAt ? `Observed ${formatTime(inventory.observedAt)}` : 'Not loaded this session'}</small><button type="button" className="ghost" onClick={() => controller.loadInventory({ force: true })} disabled={inventory.loading}>{inventory.loading ? 'Reading…' : 'Refresh inventory'}</button></div>
    </section>

    <ComponentPolicyPanel controller={controller} />

    {inventory.error && <div className="notice" role="status">{inventory.error}</div>}
    {result && <OperationResult result={result} refreshError={refreshError} onDismiss={controller.clearResult} />}
    {requestState?.kind === 'preview' && <div className="notice neutral" role="status">{requestState.canceled ? 'Discarding the late Preview response…' : `Preparing a fresh ${requestState.operation} Preview for ${COMPONENT_BY_KIND.get(requestState.component)?.label || requestState.component}…`} {!requestState.canceled && <button className="inline-link" type="button" onClick={() => controller.cancelPreview()}>Cancel Preview</button>}</div>}
    {pending && <div className="operation-running" role="status" aria-live="polite"><span className="spinner" aria-hidden="true"></span><div><strong>{COMPONENT_BY_KIND.get(pending.component)?.label} {pending.operation} is running</strong><p>No progress percentage is available. The server may need up to its bounded transaction and recovery window.</p></div></div>}

    {!values && !inventory.loading && !inventory.error && <div className="empty">Open this section to load a fresh component inventory.</div>}
    {inventory.loading && !values && <div className="loading">Reading local component state…</div>}
    {values && <div className="component-grid">
      {COMPONENTS.map((definition) => <ComponentCard
        key={definition.kind}
        definition={definition}
        component={values[definition.kind]}
        check={checks[definition.kind]}
        metadataBusy={metadataBusy}
        lifecycle={lifecycle}
        lifecycleUnavailable={controller.lifecycleUnavailable}
        policyDiscoveryBlocked={!policy.value || policy.value.mode === 'off'}
        pending={Boolean(pending)}
        previewOpen={Boolean(preview)}
        onCheck={controller.checkComponent}
        onPreview={controller.previewAction}
        onOpenSystem={onOpenSystem}
      />)}
    </div>}

    {preview && <ComponentConfirmation preview={preview} busy={Boolean(pending)} onCancel={() => controller.cancelPreview()} onConfirm={controller.submitPreview} />}
  </div>
}

function ComponentPolicyPanel({ controller }) {
  const { policy, policyDraft } = controller
  const value = policy.value
  const scheduler = value?.scheduler
  const updateBlocked = !value || value.mode === 'off'
  const saveDisabled = !value || policy.saving || policy.loading
    || !policyDraft
    || policyDraft.mode === value?.mode && Number(policyDraft.checkCadenceMinutes) === value?.checkCadenceMinutes
  const schedulerResults = scheduler?.results || {}
  return <section className="panel component-policy" aria-label="Component policy">
    <div className="component-policy-heading"><div><span className="panel-label">Background policy</span><h2>Component discovery stays bounded</h2><p>Policy and scheduler status are read only until this page is opened. The scheduler performs Check-only metadata reads; it never previews, applies, or rolls back.</p></div><div className="component-policy-actions"><small>{policy.loading ? 'Reading…' : value ? `Effective: ${POLICY_MODE_LABELS[value.mode]}` : 'Status unavailable'}</small><button type="button" className="ghost" onClick={() => controller.loadPolicy({ force: true })} disabled={policy.loading || policy.saving}>{policy.loading ? 'Reading…' : 'Refresh policy'}</button></div></div>
    {policy.error && <div className="notice" role="alert">{policy.error}</div>}
    {!value && !policy.loading && !policy.error && <div className="loading">Reading component policy…</div>}
    {value && <>
      <div className="component-policy-form">
        <label>Mode<select aria-label="Component policy mode" value={policyDraft.mode} disabled={policy.saving || policy.loading} onChange={(event) => controller.setPolicyDraft((current) => ({ ...current, mode: event.target.value }))}>{POLICY_MODES.map((mode) => <option key={mode} value={mode}>{POLICY_MODE_LABELS[mode]}</option>)}</select></label>
        <label>Check cadence (minutes)<input aria-label="Component check cadence" type="number" min={MIN_POLICY_CADENCE_MINUTES} max={MAX_POLICY_CADENCE_MINUTES} step="1" value={policyDraft.checkCadenceMinutes} disabled={policy.saving || policy.loading} onChange={(event) => controller.setPolicyDraft((current) => ({ ...current, checkCadenceMinutes: event.target.value }))} /></label>
        <button type="button" onClick={controller.savePolicy} disabled={saveDisabled}>{policy.saving ? 'Saving…' : 'Save policy'}</button>
      </div>
      <p className="component-policy-note">Notify checks only; updates remain manual.</p>
      {value.reasonCode && <div className="notice neutral" role="status">The persisted policy was rejected safely; the effective mode is Off. Reason code: <code>{value.reasonCode}</code> Save a valid policy to re-enable component discovery.</div>}
      <div className="component-policy-status">
        <div><span>Scheduler</span><strong>{scheduler?.enabled ? scheduler.state : value.mode === 'notify' ? 'unavailable' : 'disabled'}</strong></div>
        <div><span>Last cycle</span><strong>{formatTime(scheduler?.lastCycleAt)}</strong></div>
        <div><span>Next due</span><strong>{formatTime(scheduler?.nextDueAt)}</strong></div>
        <div><span>Notifications</span><strong>{scheduler?.notificationState || 'idle'}</strong></div>
      </div>
      {scheduler?.lastSkipReason && <small className="component-policy-skip">Last cycle status: {scheduler.lastSkipReason}</small>}
      {value.mode === 'off' && <p className="component-policy-disabled">Check and update are disabled by policy. Inventory and rollback Preview/Rollback/Cancel remain available.</p>}
      {value.mode === 'notify' && <div className="component-scheduler-results" aria-label="Scheduled check results"><span className="panel-label">Latest scheduled checks</span>{['xray', 'geodata', 'xkeen'].map((kind) => { const item = schedulerResults[kind]; return <div key={kind}><span>{COMPONENT_BY_KIND.get(kind)?.label}</span><strong>{item?.state || 'not run'}</strong><small>{item?.candidateIdentity || item?.reasonCode || '—'}</small></div> })}</div>}
      <small className="component-policy-guard">{updateBlocked ? 'Rollback remains available while updates are disabled.' : 'Updates still require a fresh explicit Preview and confirmation.'}</small>
    </>}
  </section>
}

function ComponentCard({ definition, component, check, metadataBusy, lifecycle, lifecycleUnavailable, policyDiscoveryBlocked, pending, previewOpen, onCheck, onPreview, onOpenSystem }) {
  const blocked = lifecycleUnavailable || lifecycle?.maintenance || lifecycle?.applying || pending || previewOpen
  const updateBlocked = blocked || policyDiscoveryBlocked
  const checkBlocked = metadataBusy || pending || previewOpen || policyDiscoveryBlocked
  const state = componentState(component)
  return <article className={`panel component-card state-${state}`} data-component={definition.kind}>
    <div className="component-card-heading"><div><span className="panel-label">{definition.description}</span><h2>{definition.label}</h2></div><span className={`chip ${state === 'present' ? 'green' : state === 'missing' ? 'amber' : 'neutral'}`}>{state}</span></div>
    <strong className="component-version">{componentVersion(component)}</strong>
    <dl className="component-facts">
      <div><dt>Support</dt><dd>{component?.capability || 'unknown'}</dd></div>
      <div><dt>Reason</dt><dd>{component?.reasonCode || '—'}</dd></div>
      {component?.architecture && <div><dt>Architecture</dt><dd>{component.architecture}</dd></div>}
      {component?.channel && <div><dt>Installed channel</dt><dd>{component.channel}</dd></div>}
    </dl>
    {definition.kind === 'geodata' && <GeodataItems items={component?.items || []} />}
    {check && <CheckSummary check={check} />}
    <div className="component-actions">
      {definition.kind === 'panel' && <button type="button" className="ghost" onClick={onOpenSystem}>Open System release</button>}
      {!definition.informational && <>
        <button type="button" className="ghost" disabled={checkBlocked} onClick={(event) => onCheck(definition.kind, definition.channel, event.currentTarget)}>{metadataBusy ? 'Please wait…' : `Check ${definition.channel}`}</button>
        <button type="button" disabled={metadataBusy || updateBlocked} onClick={(event) => onPreview(definition.kind, 'update', definition.channel, event.currentTarget)}>Preview update</button>
        <button type="button" className="ghost" disabled={metadataBusy || blocked} onClick={(event) => onPreview(definition.kind, 'rollback', '', event.currentTarget)}>Preview rollback</button>
      </>}
    </div>
    {definition.informational && definition.kind !== 'panel' && <small className="informational-note">Informational only. No install, repair, or update action is exposed here.</small>}
  </article>
}

function GeodataItems({ items }) {
  return <details className="component-details"><summary>Logical set · {items.length} items</summary><ul>{items.map((item) => <li key={item.id}><span>{item.name || item.id}</span><small>{item.state || 'unknown'} · {formatBytes(item.sizeBytes)} · {shortDigest(item.sha256)}</small></li>)}</ul></details>
}

function CheckSummary({ check }) {
  const target = check.candidate?.version || check.candidate?.generation || 'No candidate'
  return <div className="check-summary" aria-live="polite"><div><span className="panel-label">Last explicit check</span><strong>{target}</strong></div><span className={`chip ${check.eligible && check.mutationAvailable ? 'green' : 'amber'}`}>{check.eligible && check.mutationAvailable ? 'preview available' : check.reasonCode || 'not eligible'}</span><small>{formatTime(check.checkedAt)} · metadata hint only</small></div>
}

function ComponentConfirmation({ preview, busy, onCancel, onConfirm }) {
  const definition = COMPONENT_BY_KIND.get(preview.component)
  const target = preview.operation === 'rollback' ? preview.previous : preview.candidate
  const targetLabel = target?.version || target?.generation || target?.buildCommitSha || 'Validated generation'
  const items = target?.items || []
  return <section className="panel component-confirmation" aria-label="Component operation confirmation" aria-live="polite">
    <div className="dialog-heading"><div><span className="panel-label">Confirmation · {preview.operation}</span><h2>{definition?.label || preview.component} → {targetLabel}</h2></div><span className="chip neutral">Expires {formatTime(preview.expiresAt)}</span></div>
    <p>This exact server-issued Preview will be consumed before the request is sent. It cannot be replayed automatically.</p>
    <details className="integrity-details"><summary>Integrity and bounded details</summary>
      <dl className="component-facts">
        {target?.sizeBytes > 0 && <div><dt>Size</dt><dd>{formatBytes(target.sizeBytes)}</dd></div>}
        {target?.sha256 && <div><dt>SHA-256</dt><dd><code>{shortDigest(target.sha256)}</code></dd></div>}
        {target?.generation && <div><dt>Generation</dt><dd><code>{target.generation}</code></dd></div>}
        {target?.buildCommitSha && <div><dt>Build</dt><dd><code>{shortDigest(target.buildCommitSha)}</code></dd></div>}
      </dl>
      {items.length > 0 && <ul className="preview-items">{items.map((item) => <li key={item.id || item.name || item.assetName}><span>{item.name || item.id || item.assetName}</span><small>{formatBytes(item.sizeBytes)} · {shortDigest(item.sha256)}</small></li>)}</ul>}
    </details>
    <p className="warning" role="alert">This operation restarts managed runtime components and may briefly interrupt proxy traffic. A successful update replaces the retained one-step rollback generation.</p>
    <div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel</button><button type="button" onClick={onConfirm} disabled={busy}>Confirm {preview.operation}</button></div>
  </section>
}

function OperationResult({ result, refreshError, onDismiss }) {
  return <section className={`operation-result ${result.tone}`} role={result.outcome === 'unknown' || result.outcome === 'maintenance' ? 'alert' : 'status'}>
    <div><strong>{result.title}</strong><p>{result.message}</p>{result.reasonCode && <small>Reason code: <code>{result.reasonCode}</code></small>}{refreshError && <small>{refreshError}</small>}</div>
    <button className="ghost" type="button" onClick={onDismiss}>Dismiss</button>
  </section>
}
