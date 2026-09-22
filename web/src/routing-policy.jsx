import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

const EDITABILITY = Object.freeze(['editable', 'drift-detected', 'unavailable'])
const ACTIONS = Object.freeze(['direct', 'proxy', 'block'])
const PROTOCOLS = Object.freeze(['bittorrent', 'http', 'tls', 'quic', 'utp'])
const NETWORKS = Object.freeze(['tcp', 'udp'])
const SAFE_FACT_MAX = 100000

class RoutingRequestError extends Error {
  constructor(message, { status = 0, code = '', kind = 'response' } = {}) {
    super(message)
    this.status = status
    this.code = code
    this.kind = kind
  }
}

const requestJSON = async (path, options = {}) => {
  let response
  try {
    response = await fetch(path, {
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...(options.headers || {}) },
      ...options,
    })
  } catch {
    throw new RoutingRequestError('The response was lost.', { kind: 'network' })
  }

  const text = await response.text()
  let body = {}
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      if (response.ok) throw new RoutingRequestError('The server returned an unreadable response.', { status: response.status, kind: 'malformed' })
    }
  }
  if (!response.ok) {
    throw new RoutingRequestError('The routing policy request was rejected.', {
      status: response.status,
      code: typeof body?.code === 'string' ? body.code : '',
    })
  }
  return body
}

const postJSON = (path, csrfToken, body) => requestJSON(path, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
  body: JSON.stringify(body),
})

const safeCancel = (token, csrfToken) => {
  if (!token || !csrfToken) return
  void postJSON('/api/v1/appliance/policy/cancel', csrfToken, { previewToken: token }).catch(() => {})
}

const stringArray = (value) => Array.isArray(value) && value.every((item) => typeof item === 'string')
const safeFact = (value) => Number.isInteger(value) && value >= 0 && value <= SAFE_FACT_MAX
const safeText = (value) => typeof value === 'string' && value.length <= 256
const validPorts = (value) => Array.isArray(value) && value.every((port) => port && typeof port === 'object'
  && Number.isInteger(port.from) && port.from >= 1 && port.from <= 65535
  && Number.isInteger(port.to) && port.to >= 0 && port.to <= 65535)

const validRule = (value) => value && typeof value === 'object'
  && typeof value.name === 'string'
  && stringArray(value.domains)
  && stringArray(value.ips)
  && stringArray(value.protocols)
  && value.protocols.every((item) => PROTOCOLS.includes(item))
  && stringArray(value.networks)
  && value.networks.every((item) => NETWORKS.includes(item))
  && validPorts(value.ports)
  && ACTIONS.includes(value.action)

const validFacts = (value) => value && typeof value === 'object'
  && safeFact(value.totalRuleCount)
  && safeFact(value.prefixRuleCount)
  && safeFact(value.customRegionRuleCount)
  && safeText(value.regionPlacement)
  && typeof value.finalCatchAllPresent === 'boolean'

const validDNSFacts = (value) => value && typeof value === 'object'
  && safeFact(value.proxyResolverCount)
  && safeFact(value.baselineDomainCount)
  && safeFact(value.derivedDomainCount)

const validProjection = (value) => value && value.schemaVersion === 1
  && EDITABILITY.includes(value.editability)
  && Array.isArray(value.rules)
  && value.rules.every(validRule)
  && validFacts(value.protected)
  && validDNSFacts(value.dns)
  && value.observatory && typeof value.observatory === 'object'
  && safeText(value.observatory.probeInterval)
  && (value.editability === 'editable' || value.rules.length === 0)

const validRuleChange = (value) => value && typeof value === 'object'
  && typeof value.name === 'string'
  && ACTIONS.includes(value.action)

const validMatchCounts = (value) => value && typeof value === 'object'
  && safeFact(value.rules)
  && safeFact(value.domains)
  && safeFact(value.ips)
  && safeFact(value.protocols)
  && safeFact(value.networks)
  && safeFact(value.ports)

const validDiff = (value) => value && typeof value === 'object'
  && Array.isArray(value.added) && value.added.every(validRuleChange)
  && Array.isArray(value.removed) && value.removed.every(validRuleChange)
  && Array.isArray(value.changed) && value.changed.every(validRuleChange)
  && Array.isArray(value.reordered) && value.reordered.every(validRuleChange)
  && validMatchCounts(value.beforeMatches)
  && validMatchCounts(value.afterMatches)
  && safeFact(value.dnsDerivedDomainCountBefore)
  && safeFact(value.dnsDerivedDomainCountAfter)
  && Number.isInteger(value.dnsDerivedDomainCountDelta)
  && Math.abs(value.dnsDerivedDomainCountDelta) <= SAFE_FACT_MAX
  && typeof value.restartRequired === 'boolean'

const validPreview = (value) => value && typeof value === 'object'
  && typeof value.previewToken === 'string'
  && value.previewToken.length > 0 && value.previewToken.length <= 256
  && Number.isFinite(Date.parse(value.expiresAt))
  && typeof value.noop === 'boolean'
  && validDiff(value.diff)

const validApplyResult = (value) => value && typeof value === 'object'
  && typeof value.noop === 'boolean'
  && typeof value.classification === 'string'
  && value.classification.length <= 64
  && validDiff(value.diff)

const clonePorts = (ports) => ports.map((port) => ({ from: port.from, to: port.to }))
const cloneRule = (rule, clientId = '') => ({
  ...(clientId ? { clientId } : {}),
  name: rule.name,
  domains: [...rule.domains],
  ips: [...rule.ips],
  protocols: [...rule.protocols],
  networks: [...rule.networks],
  ports: clonePorts(rule.ports),
  action: rule.action,
})
const cloneRules = (rules, clientIdForRule = () => '') => rules.map((rule) => cloneRule(rule, clientIdForRule()))

const newRuleName = (rules) => {
  const base = 'New routing rule'
  if (!rules.some((rule) => rule.name === base)) return base
  let suffix = 2
  while (rules.some((rule) => rule.name === `${base} ${suffix}`)) suffix++
  return `${base} ${suffix}`
}

const makeNewRule = (rules, clientId) => ({
  clientId,
  name: newRuleName(rules),
  domains: [],
  ips: [],
  protocols: [],
  networks: [],
  ports: [],
  action: 'proxy',
})

// clientId is render-only and intentionally omitted from the broker DTO.
const serializeRule = (rule) => ({
  name: String(rule.name || ''),
  domains: Array.isArray(rule.domains) ? rule.domains.map((value) => String(value)) : [],
  ips: Array.isArray(rule.ips) ? rule.ips.map((value) => String(value)) : [],
  protocols: Array.isArray(rule.protocols) ? rule.protocols.filter((value) => PROTOCOLS.includes(value)) : [],
  networks: Array.isArray(rule.networks) ? rule.networks.filter((value) => NETWORKS.includes(value)) : [],
  ports: Array.isArray(rule.ports) ? rule.ports.map((port) => ({
    from: Number(port.from) || 0,
    to: port.to === '' || port.to == null ? 0 : Number(port.to) || 0,
  })) : [],
  action: ACTIONS.includes(rule.action) ? rule.action : 'proxy',
})

const serializeRules = (rules) => rules.map(serializeRule)
const parseLines = (value) => String(value || '').split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
const linesFor = (value) => (Array.isArray(value) ? value : []).join('\n')
const formatTime = (value) => value ? new Date(value).toLocaleString() : '—'
const signedCount = (value) => value > 0 ? `+${value}` : String(value)
const lifecycleReady = (lifecycle) => lifecycle
  && typeof lifecycle.maintenance === 'boolean'
  && typeof lifecycle.applying === 'boolean'

const previewErrorResult = (cause) => {
  switch (cause.code) {
    case 'busy':
      return { tone: 'warning', title: 'Lifecycle is busy', message: 'The Preview was not accepted. Wait for current lifecycle work, then try Preview again.', outcome: 'rejected' }
    case 'drift-detected':
      return { tone: 'error', title: 'Routing policy drift detected', message: 'Editing is blocked until a fresh policy projection reports a compatible source-owned state.', outcome: 'drift', refreshAfter: true }
    case 'candidate-rejected':
      return { tone: 'warning', title: 'Routing candidate rejected', message: 'The server rejected the typed candidate. Correct the draft and create a fresh Preview.', outcome: 'rejected' }
    case 'preview-expired':
      return { tone: 'warning', title: 'Preview expired', message: 'Create a fresh Preview. No mutation request was submitted.', outcome: 'rejected' }
    case 'unavailable':
      return { tone: 'error', title: 'Routing policy unavailable', message: 'The server could not provide a usable routing policy. No mutation request was submitted.', outcome: 'unavailable' }
    default:
      if (cause.kind === 'network' || cause.kind === 'malformed') {
        return { tone: 'error', title: 'Preview unavailable', message: `${cause.message} No mutation request was submitted.`, outcome: 'preview' }
      }
      if (cause.status === 401 || cause.status === 403) {
        return { tone: 'error', title: 'Authorization was not accepted', message: 'Sign in again if required, then create a fresh Preview. No mutation request was submitted.', outcome: 'preview' }
      }
      return { tone: 'error', title: 'Preview unavailable', message: 'The server returned an unrecognized safe outcome. No mutation request was submitted.', outcome: 'preview' }
  }
}

const applyErrorResult = (cause) => {
  switch (cause.code) {
    case 'transaction-restored':
      return { tone: 'warning', title: 'Routing changes were restored', message: 'The previous generation was verified as restored. The policy was refreshed; create a fresh Preview before another attempt.', outcome: 'restored', refreshAfter: true }
    case 'transaction-unproven':
      return { tone: 'error', title: 'Routing outcome is unknown', message: 'The Apply request was submitted, but completion is not proven. The policy was refreshed for inspection; do not infer success or replay automatically.', outcome: 'unknown', refreshAfter: true, requiresFreshRead: true }
    case 'preview-stale':
      return { tone: 'warning', title: 'Preview is stale', message: 'Create a fresh policy projection and Preview. The consumed token was not replayed.', outcome: 'stale', refreshAfter: true, requiresFreshRead: true }
    case 'preview-expired':
      return { tone: 'warning', title: 'Preview expired', message: 'The one-shot token was consumed and cannot be replayed. Create a fresh Preview.', outcome: 'expired' }
    case 'busy':
      return { tone: 'warning', title: 'Lifecycle is busy', message: 'The one-shot token is no longer reusable. Wait for current lifecycle work, then create a fresh Preview.', outcome: 'busy' }
    case 'drift-detected':
      return { tone: 'error', title: 'Routing policy drift detected', message: 'The policy was refreshed into a blocked state. Do not repair or replay automatically.', outcome: 'drift', refreshAfter: true, requiresFreshRead: true }
    case 'candidate-rejected':
      return { tone: 'warning', title: 'Routing candidate rejected', message: 'The server rejected the candidate. The draft remains available for correction; create a fresh Preview.', outcome: 'rejected' }
    case 'unavailable':
      return { tone: 'error', title: 'Routing outcome is unknown', message: 'The Apply request did not produce a proven result. The policy was refreshed for inspection; do not replay automatically.', outcome: 'unknown', refreshAfter: true, requiresFreshRead: true }
    default:
      return { tone: 'error', title: 'Routing outcome is unknown', message: `${cause.message} The request was not replayed; inspect the refreshed policy before another action.`, outcome: 'unknown', refreshAfter: true, requiresFreshRead: true }
  }
}

export function useRoutingController({ csrfToken, lifecycle, onUnauthorized, active = true, onBeforeApply, onApplied }) {
  const [projection, setProjection] = useState({ value: null, observedAt: '', loading: false, error: '' })
  const [draft, setDraftState] = useState([])
  const [dirty, setDirty] = useState(false)
  const [refreshConfirmation, setRefreshConfirmation] = useState(false)
  const [requestState, setRequestState] = useState(null)
  const [preview, setPreview] = useState(null)
  const [pending, setPending] = useState(null)
  const [result, setResult] = useState(null)
  const [refreshError, setRefreshError] = useState('')
  const policyGate = useRef(false)
  const previewGate = useRef(false)
  const submitGuard = useRef(false)
  const requestSequence = useRef(0)
  const sessionEpoch = useRef(0)
  const hasLoaded = useRef(false)
  const draftInitialized = useRef(false)
  const dirtyRef = useRef(false)
  const draftRef = useRef(draft)
  const projectionRef = useRef(projection.value)
  const previewRef = useRef(preview)
  const csrfRef = useRef(csrfToken)
  const sessionCSRFRef = useRef(csrfToken)
  const activeRef = useRef(active)
  const clientRuleId = useRef(0)

  draftRef.current = draft
  projectionRef.current = projection.value
  previewRef.current = preview
  csrfRef.current = csrfToken

  const lifecycleKnown = lifecycleReady(lifecycle)
  const lifecycleBlocked = !lifecycleKnown || lifecycle.maintenance || lifecycle.applying
  const outcomeRequiresFreshRead = Boolean(result?.requiresFreshRead)
  const nextClientRuleId = useCallback(() => `routing-rule-${clientRuleId.current++}`, [])
  const invalidateLivePreview = useCallback(() => {
    requestSequence.current++
    const token = previewRef.current?.previewToken
    previewRef.current = null
    setPreview(null)
    setRequestState((current) => current?.kind === 'preview' ? { ...current, canceled: true } : current)
    safeCancel(token, csrfRef.current)
  }, [])

  const loadPolicy = useCallback(async ({ force = false, rebase = true } = {}) => {
    if (policyGate.current || (!force && hasLoaded.current)) return false
    if (rebase) invalidateLivePreview()
    policyGate.current = true
    const epoch = sessionEpoch.current
    setProjection((current) => ({ ...current, loading: true, error: '' }))
    try {
      const value = await requestJSON('/api/v1/appliance/policy')
      if (!validProjection(value)) throw new RoutingRequestError('The routing policy projection is invalid.', { kind: 'malformed' })
      if (epoch !== sessionEpoch.current) return false
      hasLoaded.current = true
      setProjection({ value, observedAt: new Date().toISOString(), loading: false, error: '' })
      if (value.editability === 'editable' && (rebase || !draftInitialized.current)) {
        draftInitialized.current = true
        dirtyRef.current = false
        setDraftState(cloneRules(value.rules, nextClientRuleId))
        setDirty(false)
      }
      setResult((current) => current?.requiresFreshRead ? { ...current, requiresFreshRead: false } : current)
      return true
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return false
      if (cause.status === 401) onUnauthorized()
      hasLoaded.current = false
      setProjection((current) => ({ ...current, loading: false, error: 'The routing policy projection is unavailable.' }))
      return false
    } finally {
      policyGate.current = false
    }
  }, [invalidateLivePreview, nextClientRuleId, onUnauthorized, result?.requiresFreshRead])

  const activate = useCallback(() => loadPolicy(), [loadPolicy])
  const refreshPeerProjection = useCallback(() => {
    if (!activeRef.current) return false
    return loadPolicy({ force: true, rebase: !dirtyRef.current })
  }, [loadPolicy])

  const updateDraft = useCallback((updater) => {
    setDraftState((current) => {
      const next = typeof updater === 'function' ? updater(current) : updater
      return Array.isArray(next) ? next : current
    })
    dirtyRef.current = true
    setDirty(true)
    setResult((current) => current?.outcome === 'rejected' || current?.outcome === 'expired' || current?.outcome === 'busy' ? null : current)
  }, [])

  const updateRule = useCallback((index, patch) => updateDraft((current) => current.map((rule, position) => position === index ? { ...rule, ...patch } : rule)), [updateDraft])
  const addRule = useCallback(() => {
    const clientId = nextClientRuleId()
    updateDraft((current) => [...current, makeNewRule(current, clientId)])
  }, [nextClientRuleId, updateDraft])
  const removeRule = useCallback((index) => updateDraft((current) => current.filter((_, position) => position !== index)), [updateDraft])
  const moveRule = useCallback((index, direction) => updateDraft((current) => {
    const target = index + direction
    if (target < 0 || target >= current.length) return current
    const next = [...current]
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    return next
  }), [updateDraft])

  const requestRefresh = useCallback(() => {
    if (dirtyRef.current) {
      setRefreshConfirmation(true)
      return false
    }
    return loadPolicy({ force: true, rebase: true })
  }, [loadPolicy])

  const discardAndRefresh = useCallback(() => {
    setRefreshConfirmation(false)
    dirtyRef.current = false
    setDirty(false)
    return loadPolicy({ force: true, rebase: true })
  }, [loadPolicy])

  const cancelRefresh = useCallback(() => setRefreshConfirmation(false), [])

  const previewDraft = useCallback(async () => {
    if (previewGate.current || submitGuard.current || previewRef.current || pending || lifecycleBlocked || outcomeRequiresFreshRead || projectionRef.current?.editability !== 'editable') return false
    previewGate.current = true
    const id = ++requestSequence.current
    const epoch = sessionEpoch.current
    const rules = serializeRules(draftRef.current)
    setRequestState({ id, kind: 'preview', canceled: false })
    setResult(null)
    try {
      const value = await postJSON('/api/v1/appliance/policy/preview', csrfToken, { rules })
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) {
        if (typeof value?.previewToken === 'string') safeCancel(value.previewToken, csrfToken)
        return false
      }
      if (!validPreview(value)) {
        if (typeof value?.previewToken === 'string') safeCancel(value.previewToken, csrfToken)
        throw new RoutingRequestError('The Preview response is invalid.', { kind: 'malformed' })
      }
      previewRef.current = value
      setPreview(value)
      return true
    } catch (cause) {
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) return false
      if (cause.status === 401) onUnauthorized()
      const mapped = previewErrorResult(cause)
      setResult(mapped)
      if (mapped.refreshAfter) await loadPolicy({ force: true, rebase: true })
      return false
    } finally {
      previewGate.current = false
      setRequestState((current) => current?.id === id ? null : current)
    }
  }, [csrfToken, lifecycleBlocked, loadPolicy, onUnauthorized, outcomeRequiresFreshRead, pending])

  const cancelPreview = useCallback((reason = 'canceled') => {
    requestSequence.current++
    const token = previewRef.current?.previewToken
    previewRef.current = null
    setPreview(null)
    setRequestState((current) => current?.kind === 'preview' ? { ...current, canceled: true } : current)
    safeCancel(token, csrfRef.current)
    if (reason === 'expired') {
      setResult({ tone: 'warning', title: 'Preview expired', message: 'The unsent one-shot token was discarded. Create a fresh Preview to continue.', outcome: 'expired' })
    }
  }, [])

  const submitPreview = useCallback(async () => {
    const current = previewRef.current
    if (!current || current.noop || submitGuard.current || lifecycleBlocked || outcomeRequiresFreshRead) return false
    submitGuard.current = true
    onBeforeApply?.()
    const epoch = sessionEpoch.current
    const token = current.previewToken
    const operation = { startedAt: new Date().toISOString() }
    previewRef.current = null
    setPreview(null)
    setPending(operation)
    setResult(null)
    setRefreshError('')
    let refreshAfter = false
    let rebase = true
    const isCurrentSession = () => epoch === sessionEpoch.current && csrfRef.current === csrfToken
    try {
      const value = await postJSON('/api/v1/appliance/policy/apply', csrfToken, { previewToken: token })
      if (!isCurrentSession()) return false
      if (!validApplyResult(value)) throw new RoutingRequestError('The Apply response is invalid.', { kind: 'malformed' })
      setResult({
        tone: 'success',
        title: value.noop ? 'No effective routing changes' : 'Routing changes applied',
        message: value.noop ? 'The broker confirmed that the requested policy was already effective.' : 'The broker verified the settings transaction and runtime restart.',
        outcome: 'success',
      })
      refreshAfter = true
      onApplied?.()
    } catch (cause) {
      if (!isCurrentSession()) return false
      if (cause.status === 401) onUnauthorized()
      const mapped = applyErrorResult(cause)
      setResult(mapped)
      refreshAfter = Boolean(mapped.refreshAfter)
      rebase = true
    } finally {
      if (isCurrentSession()) {
        setPending(null)
        submitGuard.current = false
      }
    }
    if (refreshAfter && isCurrentSession()) {
      const refreshed = await loadPolicy({ force: true, rebase })
      if (!isCurrentSession()) return false
      if (!refreshed) setRefreshError('The routing outcome is preserved, but the subsequent policy refresh failed.')
    }
    return true
  }, [csrfToken, lifecycleBlocked, loadPolicy, onApplied, onBeforeApply, onUnauthorized, outcomeRequiresFreshRead])

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
    hasLoaded.current = false
    draftInitialized.current = false
    dirtyRef.current = false
    setProjection({ value: null, observedAt: '', loading: false, error: '' })
    setDraftState([])
    setDirty(false)
    setRefreshConfirmation(false)
    setPreview(null)
    setPending(null)
    setRequestState(null)
    setResult(null)
    setRefreshError('')
    sessionCSRFRef.current = csrfToken
  }, [csrfToken])

  useEffect(() => {
    const previousActive = activeRef.current
    activeRef.current = active
    if (previousActive && !active) invalidateLivePreview()
  }, [active, invalidateLivePreview])

  useEffect(() => () => {
    sessionEpoch.current++
    requestSequence.current++
    safeCancel(previewRef.current?.previewToken, csrfRef.current)
  }, [])

  return useMemo(() => ({
    projection,
    draft,
    dirty,
    refreshConfirmation,
    requestState,
    preview,
    pending,
    result,
    refreshError,
    lifecycleKnown,
    lifecycleBlocked,
    outcomeRequiresFreshRead,
    loadPolicy,
    refreshPeerProjection,
    invalidateLivePreview,
    activate,
    requestRefresh,
    discardAndRefresh,
    cancelRefresh,
    updateRule,
    updateDraft,
    addRule,
    removeRule,
    moveRule,
    previewDraft,
    cancelPreview,
    submitPreview,
  }), [activate, addRule, cancelPreview, cancelRefresh, discardAndRefresh, dirty, draft, invalidateLivePreview, lifecycleBlocked, lifecycleKnown, loadPolicy, moveRule, outcomeRequiresFreshRead, pending, preview, previewDraft, projection, refreshConfirmation, refreshError, refreshPeerProjection, removeRule, requestRefresh, requestState, result, submitPreview, updateDraft, updateRule])
}

export function RoutingLifecycleNotice({ controller, onOpenRouting, active }) {
  if (active) return null
  if (controller.pending) {
    return <div className="lifecycle-banner running" role="status" aria-live="polite"><div><strong>Routing Apply is running</strong><span>The synchronous broker request remains active. Do not reload or submit another routing operation.</span></div><button className="ghost" type="button" onClick={onOpenRouting}>Open Routing</button></div>
  }
  if (controller.result?.outcome === 'unknown') {
    return <div className="lifecycle-banner danger" role="alert"><div><strong>{controller.result.title}</strong><span>{controller.result.message}</span></div><button className="ghost" type="button" onClick={onOpenRouting}>Open Routing</button></div>
  }
  return null
}

export function RoutingPolicySection({ controller, lifecycle }) {
  const { projection, draft, dirty, refreshConfirmation, requestState, preview, pending, result, refreshError } = controller
  const value = projection.value
  const editable = value?.editability === 'editable'
  const editorDisabled = !editable || controller.lifecycleBlocked || controller.outcomeRequiresFreshRead || Boolean(preview) || Boolean(pending) || requestState?.kind === 'preview'
  const previewDisabled = editorDisabled || projection.loading

  useEffect(() => {
    void controller.activate()
  }, [controller.activate])

  return <div className="section-stack routing-section">
    <section className="panel routing-heading">
      <div><span className="panel-label">Routing</span><h2>Typed custom rules</h2><p>Only the supported custom region is editable. Protected routing, proxy DNS relationships, and Observatory settings remain source-owned and read only.</p></div>
      <div className="routing-heading-actions"><small>{projection.observedAt ? `Observed ${formatTime(projection.observedAt)}` : 'Not loaded this session'}</small><button className="ghost" type="button" onClick={controller.requestRefresh} disabled={projection.loading || Boolean(pending)}>{projection.loading ? 'Reading…' : 'Refresh policy'}</button></div>
    </section>

    {refreshConfirmation && <section className="routing-refresh-confirm" role="alert"><div><strong>Unsaved routing changes</strong><span>Refreshing will replace the current draft with the latest server projection.</span></div><div className="routing-refresh-actions"><button type="button" onClick={controller.discardAndRefresh}>Discard changes and refresh</button><button className="ghost" type="button" onClick={controller.cancelRefresh}>Keep editing</button></div></section>}
    {projection.error && <div className="notice" role="alert">{projection.error}</div>}
    {refreshError && <div className="notice" role="alert">{refreshError}</div>}
    {projection.loading && !value && <div className="loading">Reading the current typed routing policy…</div>}
    {!value && !projection.loading && !projection.error && <div className="empty">Open Routing to load the current typed policy projection.</div>}

    {value && <RoutingFacts projection={value} />}
    {value?.editability === 'drift-detected' && <div className="routing-blocked" role="alert"><strong>Routing editing is blocked by protected-state drift.</strong><span>Refresh may restore editability after the server proves the source-owned policy is compatible. No repair or adoption is offered here.</span></div>}
    {value?.editability === 'unavailable' && <div className="routing-blocked danger" role="alert"><strong>Routing policy is unavailable.</strong><span>The safe projection could not establish an editable authority. Mutation controls remain disabled.</span></div>}

    {editable && <>
      <section className="panel routing-editor" aria-label="Custom routing rule editor">
        <div className="routing-editor-heading"><div><span className="panel-label">Custom region</span><h2>Ordered rules {dirty && <span className="chip amber">Unsaved</span>}</h2><p>List order is significant. Match members are sent as typed expressions; the server remains the validation and canonicalization authority.</p></div><button type="button" onClick={controller.addRule} disabled={editorDisabled}>Add rule</button></div>
        {draft.length === 0 && <div className="routing-empty">No custom rules. Add a rule before the protected final direct catch-all.</div>}
        <div className="routing-rule-list">{draft.map((rule, index) => <RoutingRuleEditor key={rule.clientId} rule={rule} index={index} total={draft.length} disabled={editorDisabled} onChange={(patch) => controller.updateRule(index, patch)} onRemove={() => controller.removeRule(index)} onMove={(direction) => controller.moveRule(index, direction)} />)}</div>
        <div className="routing-editor-actions"><div>{controller.lifecycleBlocked && <small className="routing-disabled-note">Lifecycle readiness is unavailable or maintenance/applying is active; new mutation requests are disabled.</small>}{controller.outcomeRequiresFreshRead && <small className="routing-disabled-note">Refresh the policy and verify the outcome before creating another Preview.</small>}</div><button type="button" onClick={controller.previewDraft} disabled={previewDisabled}>{requestState?.kind === 'preview' ? 'Preparing Preview…' : 'Preview changes'}</button></div>
      </section>
    </>}

    {requestState?.kind === 'preview' && <div className="notice neutral" role="status">{requestState.canceled ? 'Discarding the late Preview response…' : 'Preparing a fresh semantic Preview…'} {!requestState.canceled && <button className="inline-link" type="button" onClick={() => controller.cancelPreview()}>Cancel Preview</button>}</div>}
    {pending && <div className="operation-running routing-operation" role="status" aria-live="polite"><span className="spinner" aria-hidden="true"></span><div><strong>Routing Apply is running</strong><p>No progress percentage is available. The broker may need its bounded transaction and recovery window.</p></div></div>}
    {result && <RoutingResult result={result} refreshError={refreshError} />}
    {preview && <RoutingPreview preview={preview} busy={Boolean(pending)} applyDisabled={controller.lifecycleBlocked || controller.outcomeRequiresFreshRead} onCancel={() => controller.cancelPreview()} onConfirm={controller.submitPreview} />}
  </div>
}

function RoutingFacts({ projection }) {
  const { protected: protectedFacts, dns, observatory } = projection
  const editabilityLabel = projection.editability === 'editable' ? 'Editable' : projection.editability === 'drift-detected' ? 'Drift detected' : 'Unavailable'
  return <section className="panel routing-facts" aria-label="Routing source-owned facts">
    <div className="routing-facts-heading"><div><span className="panel-label">Source-owned context</span><h2>Policy boundary</h2></div><span className={`chip ${projection.editability === 'editable' ? 'green' : 'amber'}`}>{editabilityLabel}</span></div>
    <div className="routing-facts-grid">
      <Fact label="Protected prefix rules" value={protectedFacts.prefixRuleCount} />
      <Fact label="Custom region rules" value={protectedFacts.customRegionRuleCount} />
      <Fact label="Custom region placement" value={protectedFacts.regionPlacement} />
      <Fact label="Final direct catch-all" value={protectedFacts.finalCatchAllPresent ? 'Locked / present' : 'Not proven'} />
      <Fact label="Proxy DNS resolvers" value={dns.proxyResolverCount} />
      <Fact label="Baseline proxy domains" value={dns.baselineDomainCount} />
      <Fact label="Derived proxy domains" value={dns.derivedDomainCount} />
      <Fact label="Observatory interval" value={observatory.probeInterval || '—'} />
    </div>
  </section>
}

function Fact({ label, value }) {
  return <div><span>{label}</span><strong>{value}</strong></div>
}

function RoutingRuleEditor({ rule, index, total, disabled, onChange, onRemove, onMove }) {
  const updateList = (field, value) => onChange({ [field]: parseLines(value) })
  const toggle = (field, value) => onChange({ [field]: rule[field].includes(value) ? rule[field].filter((item) => item !== value) : [...rule[field], value] })
  const updatePort = (portIndex, field, value) => onChange({ ports: rule.ports.map((port, position) => position === portIndex ? { ...port, [field]: value } : port) })
  const addPort = () => onChange({ ports: [...rule.ports, { from: '', to: '' }] })
  const removePort = (portIndex) => onChange({ ports: rule.ports.filter((_, position) => position !== portIndex) })

  return <fieldset className="routing-rule" aria-label={`Routing rule ${index + 1}: ${rule.name || 'unnamed'}`}>
    <legend><span>Rule {index + 1}</span><strong>{rule.name || 'Unnamed rule'}</strong></legend>
    <div className="routing-rule-main">
      <label>Display name<input maxLength={48} aria-label={`Rule ${index + 1} display name`} value={rule.name} disabled={disabled} onChange={(event) => onChange({ name: event.target.value })} /></label>
      <label>Action<select aria-label={`Rule ${index + 1} action`} value={rule.action} disabled={disabled} onChange={(event) => onChange({ action: event.target.value })}>{ACTIONS.map((action) => <option key={action} value={action}>{action}</option>)}</select></label>
    </div>
    <div className="routing-rule-matches">
      <label>Domain expressions<textarea rows="3" aria-label={`Rule ${index + 1} domain expressions`} value={linesFor(rule.domains)} disabled={disabled} onChange={(event) => updateList('domains', event.target.value)} placeholder="one expression per line" /></label>
      <label>IP / geodata expressions<textarea rows="3" aria-label={`Rule ${index + 1} IP and geodata expressions`} value={linesFor(rule.ips)} disabled={disabled} onChange={(event) => updateList('ips', event.target.value)} placeholder="one expression per line" /></label>
    </div>
    <div className="routing-rule-options">
      <fieldset><legend>Protocols</legend><div className="routing-checks">{PROTOCOLS.map((protocol) => <label key={protocol}><input aria-label={`Rule ${index + 1} protocol ${protocol}`} type="checkbox" checked={rule.protocols.includes(protocol)} disabled={disabled} onChange={() => toggle('protocols', protocol)} />{protocol}</label>)}</div></fieldset>
      <fieldset><legend>Networks</legend><div className="routing-checks">{NETWORKS.map((network) => <label key={network}><input aria-label={`Rule ${index + 1} network ${network}`} type="checkbox" checked={rule.networks.includes(network)} disabled={disabled} onChange={() => toggle('networks', network)} />{network}</label>)}</div></fieldset>
    </div>
    <div className="routing-ports" aria-label={`Rule ${index + 1} port ranges`}>
      <div className="routing-subheading"><span>Port ranges</span><button className="ghost" type="button" onClick={addPort} disabled={disabled}>Add port range</button></div>
      {rule.ports.length === 0 && <small className="muted">No port restriction</small>}
      {rule.ports.map((port, portIndex) => <div className="routing-port-row" key={portIndex}><label>From<input aria-label={`Rule ${index + 1} port ${portIndex + 1} from`} type="number" min="1" max="65535" step="1" value={port.from} disabled={disabled} onChange={(event) => updatePort(portIndex, 'from', event.target.value)} /></label><label>To <span className="muted">(optional)</span><input aria-label={`Rule ${index + 1} port ${portIndex + 1} to`} type="number" min="1" max="65535" step="1" value={port.to} disabled={disabled} onChange={(event) => updatePort(portIndex, 'to', event.target.value)} /></label><button className="ghost" type="button" onClick={() => removePort(portIndex)} disabled={disabled}>Remove port</button></div>)}
    </div>
    <div className="routing-rule-actions"><button className="ghost" type="button" onClick={() => onMove(-1)} disabled={disabled || index === 0} aria-label={`Move rule up: ${rule.name || `rule ${index + 1}`}`}>Move up</button><button className="ghost" type="button" onClick={() => onMove(1)} disabled={disabled || index === total - 1} aria-label={`Move rule down: ${rule.name || `rule ${index + 1}`}`}>Move down</button><button className="ghost danger-action" type="button" onClick={onRemove} disabled={disabled} aria-label={`Remove rule: ${rule.name || `rule ${index + 1}`}`}>Remove rule</button></div>
  </fieldset>
}

function RoutingPreview({ preview, busy, applyDisabled, onCancel, onConfirm }) {
  const diff = preview.diff
  const groups = [
    ['Added', diff.added],
    ['Removed', diff.removed],
    ['Changed', diff.changed],
    ['Reordered', diff.reordered],
  ]
  return <section className="panel routing-preview" aria-label="Routing Preview confirmation" aria-live="polite">
    <div className="routing-preview-heading"><div><span className="panel-label">Semantic Preview</span><h2>{preview.noop ? 'No effective changes' : 'Review routing changes'}</h2><p>Server-derived facts only; candidate configuration and protected rule bodies stay hidden.</p></div><small>Expires {formatTime(preview.expiresAt)}</small></div>
    <div className="routing-diff-groups">{groups.map(([label, changes]) => <div key={label}><span>{label}</span>{changes.length === 0 ? <small>None</small> : <ul>{changes.map((change, index) => <li key={`${change.name}-${index}`}><strong>{change.name}</strong><small>{change.action}</small></li>)}</ul>}</div>)}</div>
    <div className="routing-diff-facts">
      <Fact label="Rules before → after" value={`${diff.beforeMatches.rules} → ${diff.afterMatches.rules}`} />
      <Fact label="Match members before → after" value={`${diff.beforeMatches.domains + diff.beforeMatches.ips + diff.beforeMatches.protocols + diff.beforeMatches.networks + diff.beforeMatches.ports} → ${diff.afterMatches.domains + diff.afterMatches.ips + diff.afterMatches.protocols + diff.afterMatches.networks + diff.afterMatches.ports}`} />
      <Fact label="Derived proxy domains" value={`${diff.dnsDerivedDomainCountBefore} → ${diff.dnsDerivedDomainCountAfter} (${signedCount(diff.dnsDerivedDomainCountDelta)})`} />
      <Fact label="Runtime restart" value={diff.restartRequired ? 'Required' : 'Not required'} />
    </div>
    {!preview.noop && <p className="routing-preview-warning">Confirming Apply sends only the one-shot Preview token. Managed Xray/XKeen runtime will restart and proxy traffic may be briefly interrupted.</p>}
    {preview.noop && <p className="routing-preview-note">No Apply action is available for a no-op Preview. Cancel it to consume the token.</p>}
    <div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel Preview</button>{!preview.noop && <button type="button" onClick={onConfirm} disabled={busy || applyDisabled}>Apply routing changes</button>}</div>
  </section>
}

function RoutingResult({ result, refreshError }) {
  return <div className={`operation-result routing-result ${result.tone}`} role={result.tone === 'error' ? 'alert' : 'status'} data-testid="routing-result"><div><strong>{result.title}</strong><p>{result.message}</p>{refreshError && <small>{refreshError}</small>}</div></div>
}
