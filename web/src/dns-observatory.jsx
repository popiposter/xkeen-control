import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

const EDITABILITY = Object.freeze(['editable', 'drift-detected', 'unavailable'])
const FALLBACK_MODES = Object.freeze(['system', 'disabled'])
const MIN_STALE_TTL_SECONDS = 60
const MAX_STALE_TTL_SECONDS = 86400
const SAFE_COUNT_MAX = 100000

class DNSRequestError extends Error {
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
    throw new DNSRequestError('The response was lost.', { kind: 'network' })
  }
  const text = await response.text()
  let body = {}
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      if (response.ok) throw new DNSRequestError('The server returned an unreadable response.', { status: response.status, kind: 'malformed' })
    }
  }
  if (!response.ok) {
    throw new DNSRequestError('The DNS and Observatory request was rejected.', {
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
  void postJSON('/api/v1/appliance/dns-observatory/cancel', csrfToken, { previewToken: token }).catch(() => {})
}

const safeText = (value) => typeof value === 'string' && value.length > 0 && value.length <= 256
const safeCount = (value) => Number.isInteger(value) && value >= 0 && value <= SAFE_COUNT_MAX
const stringArray = (value) => Array.isArray(value) && value.every(safeText)
const validResolverCatalog = (value) => Array.isArray(value)
  && value.every((item) => item && typeof item === 'object' && safeText(item.id) && safeText(item.label))
  && new Set(value.map((item) => item.id)).size === value.length

const validDNSSettings = (value, resolverCatalog) => value && typeof value === 'object'
  && stringArray(value.proxyResolverIds)
  && value.proxyResolverIds.length > 0
  && new Set(value.proxyResolverIds).size === value.proxyResolverIds.length
  && value.proxyResolverIds.every((id) => resolverCatalog.some((item) => item.id === id))
  && FALLBACK_MODES.includes(value.fallbackMode)
  && typeof value.cacheEnabled === 'boolean'
  && typeof value.serveStale === 'boolean'
  && Number.isInteger(value.staleTTLSeconds)
  && typeof value.parallelQueries === 'boolean'
  && (value.cacheEnabled || (!value.serveStale && value.staleTTLSeconds === 0))
  && (value.serveStale
    ? value.cacheEnabled && value.staleTTLSeconds >= MIN_STALE_TTL_SECONDS && value.staleTTLSeconds <= MAX_STALE_TTL_SECONDS
    : value.staleTTLSeconds === 0)

const validProjection = (value) => {
  const catalog = value?.dns?.resolverCatalog
  const observatory = value?.observatory
  const locked = value?.dns?.locked
  const counts = value?.dns?.proxyDomainCounts
  return value && value.schemaVersion === 1
    && EDITABILITY.includes(value.editability)
    && validResolverCatalog(catalog)
    && locked && safeText(locked.queryStrategy)
    && typeof locked.leakPreventionEnabled === 'boolean'
    && typeof locked.systemFallbackPresent === 'boolean'
    && counts && safeCount(counts.baseline) && safeCount(counts.derived)
    && observatory && Number.isInteger(observatory.minIntervalMinutes) && Number.isInteger(observatory.maxIntervalMinutes)
    && observatory.minIntervalMinutes >= 1 && observatory.maxIntervalMinutes <= 5
    && observatory.minIntervalMinutes <= observatory.maxIntervalMinutes
    && Number.isInteger(observatory.probeIntervalMinutes)
    && (value.editability !== 'editable'
      || (observatory.probeIntervalMinutes >= observatory.minIntervalMinutes
        && observatory.probeIntervalMinutes <= observatory.maxIntervalMinutes))
    && (value.editability !== 'editable' || validDNSSettings(value.dns, catalog))
}

const validDiff = (value) => {
  const dns = value?.dns
  const observatory = value?.observatory
  return value && typeof value === 'object'
    && dns && stringArray(dns.resolverIdsAdded) && stringArray(dns.resolverIdsRemoved) && stringArray(dns.resolverIdsReordered)
    && FALLBACK_MODES.includes(dns.fallbackModeBefore) && FALLBACK_MODES.includes(dns.fallbackModeAfter)
    && typeof dns.cacheEnabledBefore === 'boolean' && typeof dns.cacheEnabledAfter === 'boolean'
    && typeof dns.serveStaleBefore === 'boolean' && typeof dns.serveStaleAfter === 'boolean'
    && Number.isInteger(dns.staleTTLSecondsBefore) && Number.isInteger(dns.staleTTLSecondsAfter)
    && typeof dns.parallelQueriesBefore === 'boolean' && typeof dns.parallelQueriesAfter === 'boolean'
    && observatory && Number.isInteger(observatory.probeIntervalMinutesBefore) && Number.isInteger(observatory.probeIntervalMinutesAfter)
    && safeCount(value.derivedProxyDomainCountBefore) && safeCount(value.derivedProxyDomainCountAfter)
    && Number.isInteger(value.derivedProxyDomainCountDelta) && Math.abs(value.derivedProxyDomainCountDelta) <= SAFE_COUNT_MAX
    && typeof value.restartRequired === 'boolean'
}

const validPreview = (value) => value && safeText(value.previewToken)
  && Number.isFinite(Date.parse(value.expiresAt))
  && typeof value.noop === 'boolean'
  && validDiff(value.diff)

const validApplyResult = (value) => value && typeof value.noop === 'boolean'
  && safeText(value.classification)
  && validDiff(value.diff)

const lifecycleReady = (lifecycle) => lifecycle
  && typeof lifecycle.maintenance === 'boolean'
  && typeof lifecycle.applying === 'boolean'

const cloneDraft = (projection) => ({
  dns: {
    proxyResolverIds: [...projection.dns.proxyResolverIds],
    fallbackMode: projection.dns.fallbackMode,
    cacheEnabled: projection.dns.cacheEnabled,
    serveStale: projection.dns.serveStale,
    staleTTLSeconds: projection.dns.staleTTLSeconds,
    parallelQueries: projection.dns.parallelQueries,
  },
  observatory: { probeIntervalMinutes: projection.observatory.probeIntervalMinutes },
})

const serializeDraft = (draft) => ({
  dns: {
    proxyResolverIds: [...draft.dns.proxyResolverIds],
    fallbackMode: draft.dns.fallbackMode,
    cacheEnabled: Boolean(draft.dns.cacheEnabled),
    serveStale: Boolean(draft.dns.serveStale),
    staleTTLSeconds: Number(draft.dns.staleTTLSeconds),
    parallelQueries: Boolean(draft.dns.parallelQueries),
  },
  observatory: { probeIntervalMinutes: Number(draft.observatory.probeIntervalMinutes) },
})

const previewErrorResult = (cause) => {
  switch (cause.code) {
    case 'busy': return { tone: 'warning', title: 'Lifecycle is busy', message: 'The Preview was not accepted. Wait for current lifecycle work, then try again.', outcome: 'busy' }
    case 'drift-detected': return { tone: 'error', title: 'DNS policy drift detected', message: 'Editing is blocked until a fresh safe projection proves compatible source-owned state.', outcome: 'drift', refreshAfter: true }
    case 'candidate-rejected': return { tone: 'warning', title: 'DNS candidate rejected', message: 'Correct the typed draft and create a fresh Preview.', outcome: 'rejected' }
    case 'preview-expired': return { tone: 'warning', title: 'Preview expired', message: 'Create a fresh Preview. No mutation request was submitted.', outcome: 'expired' }
    case 'unavailable': return { tone: 'error', title: 'DNS policy unavailable', message: 'The server could not provide a usable DNS and Observatory policy.', outcome: 'unavailable' }
    default:
      if (cause.status === 401 || cause.status === 403) return { tone: 'error', title: 'Authorization was not accepted', message: 'Sign in again if required, then create a fresh Preview.', outcome: 'preview' }
      return { tone: 'error', title: 'Preview unavailable', message: `${cause.message} No mutation request was submitted.`, outcome: 'preview' }
  }
}

const applyErrorResult = (cause) => {
  switch (cause.code) {
    case 'transaction-restored': return { tone: 'warning', title: 'DNS changes were restored', message: 'The previous generation was verified restored. The policy was refreshed; create a fresh Preview before another attempt.', outcome: 'restored', refreshAfter: true }
    case 'transaction-unproven': return { tone: 'error', title: 'DNS outcome is unknown', message: 'Apply completion is not proven. Refresh and inspect before another mutation; the request was not replayed.', outcome: 'unknown', refreshAfter: true, requiresFreshRead: true }
    case 'preview-stale': return { tone: 'warning', title: 'Preview is stale', message: 'The consumed token was not replayed. Refresh the policy and create a fresh Preview.', outcome: 'stale', refreshAfter: true, requiresFreshRead: true }
    case 'preview-expired': return { tone: 'warning', title: 'Preview expired', message: 'The one-shot token was consumed. Create a fresh Preview.', outcome: 'expired' }
    case 'busy': return { tone: 'warning', title: 'Lifecycle is busy', message: 'The token is no longer reusable. Wait, then create a fresh Preview.', outcome: 'busy' }
    case 'drift-detected': return { tone: 'error', title: 'DNS policy drift detected', message: 'The policy was refreshed into a blocked state. No repair or replay was attempted.', outcome: 'drift', refreshAfter: true, requiresFreshRead: true }
    case 'candidate-rejected': return { tone: 'warning', title: 'DNS candidate rejected', message: 'The draft remains available for correction; create a fresh Preview.', outcome: 'rejected' }
    default: return { tone: 'error', title: 'DNS outcome is unknown', message: `${cause.message} Refresh and inspect before another mutation; the request was not replayed.`, outcome: 'unknown', refreshAfter: true, requiresFreshRead: true }
  }
}

export function useDNSObservatoryController({ csrfToken, lifecycle, onUnauthorized, active = true, appliancePolicyUncertain = false, onBeforeApply, onApplied, onUnprovenApply, onFreshReadAfterUnprovenApply }) {
  const [projection, setProjection] = useState({ value: null, observedAt: '', loading: false, error: '' })
  const [draft, setDraft] = useState(null)
  const [dirty, setDirty] = useState(false)
  const [refreshConfirmation, setRefreshConfirmation] = useState(false)
  const [requestState, setRequestState] = useState(null)
  const [preview, setPreview] = useState(null)
  const [pending, setPending] = useState(null)
  const [result, setResult] = useState(null)
  const [refreshError, setRefreshError] = useState('')
  const readGate = useRef(false)
  const previewGate = useRef(false)
  const submitGate = useRef(false)
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
  const unprovenReadGeneration = useRef(0)
  const unprovenReadPending = useRef(false)

  draftRef.current = draft
  projectionRef.current = projection.value
  previewRef.current = preview
  csrfRef.current = csrfToken

  const lifecycleKnown = lifecycleReady(lifecycle)
  const lifecycleBlocked = !lifecycleKnown || lifecycle.maintenance || lifecycle.applying
  const outcomeRequiresFreshRead = Boolean(result?.requiresFreshRead)
  const invalidateLivePreview = useCallback(() => {
    requestSequence.current++
    const token = previewRef.current?.previewToken
    previewRef.current = null
    setPreview(null)
    setRequestState((current) => current?.kind === 'preview' ? { ...current, canceled: true } : current)
    safeCancel(token, csrfRef.current)
  }, [])

  const loadPolicy = useCallback(async ({ force = false, rebase = true } = {}) => {
    if (readGate.current || (!force && hasLoaded.current)) return false
    if (rebase) invalidateLivePreview()
    readGate.current = true
    const epoch = sessionEpoch.current
    const uncertaintyGeneration = unprovenReadGeneration.current
    setProjection((current) => ({ ...current, loading: true, error: '' }))
    try {
      const value = await requestJSON('/api/v1/appliance/dns-observatory')
      if (!validProjection(value)) throw new DNSRequestError('The DNS and Observatory projection is invalid.', { kind: 'malformed' })
      if (epoch !== sessionEpoch.current) return false
      hasLoaded.current = true
      setProjection({ value, observedAt: new Date().toISOString(), loading: false, error: '' })
      if (value.editability === 'editable' && (rebase || !draftInitialized.current)) {
        const next = cloneDraft(value)
        draftInitialized.current = true
        dirtyRef.current = false
        draftRef.current = next
        setDraft(next)
        setDirty(false)
      } else if (value.editability !== 'editable') {
        draftInitialized.current = false
        dirtyRef.current = false
        draftRef.current = null
        setDraft(null)
        setDirty(false)
      }
      setResult((current) => current?.requiresFreshRead ? { ...current, requiresFreshRead: false } : current)
      if (unprovenReadPending.current && uncertaintyGeneration === unprovenReadGeneration.current) {
        unprovenReadPending.current = false
        onFreshReadAfterUnprovenApply?.()
      }
      return true
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return false
      if (cause.status === 401) onUnauthorized()
      hasLoaded.current = false
      setProjection((current) => ({ ...current, loading: false, error: 'The DNS and Observatory projection is unavailable.' }))
      return false
    } finally {
      readGate.current = false
    }
  }, [invalidateLivePreview, onFreshReadAfterUnprovenApply, onUnauthorized])

  const activate = useCallback(() => loadPolicy(), [loadPolicy])
  const refreshPeerProjection = useCallback(() => {
    if (!activeRef.current) return false
    return loadPolicy({ force: true, rebase: !dirtyRef.current })
  }, [loadPolicy])
  const updateDraft = useCallback((updater) => {
    setDraft((current) => {
      if (!current) return current
      const next = updater(current)
      draftRef.current = next
      return next
    })
    dirtyRef.current = true
    setDirty(true)
    setResult((current) => ['rejected', 'expired', 'busy'].includes(current?.outcome) ? null : current)
  }, [])
  const updateDNS = useCallback((patch) => updateDraft((current) => ({ ...current, dns: { ...current.dns, ...patch } })), [updateDraft])
  const addResolver = useCallback((id) => updateDraft((current) => current.dns.proxyResolverIds.includes(id) ? current : ({ ...current, dns: { ...current.dns, proxyResolverIds: [...current.dns.proxyResolverIds, id] } })), [updateDraft])
  const removeResolver = useCallback((index) => updateDraft((current) => current.dns.proxyResolverIds.length <= 1 ? current : ({ ...current, dns: { ...current.dns, proxyResolverIds: current.dns.proxyResolverIds.filter((_, position) => position !== index) } })), [updateDraft])
  const moveResolver = useCallback((index, direction) => updateDraft((current) => {
    const target = index + direction
    if (target < 0 || target >= current.dns.proxyResolverIds.length) return current
    const ids = [...current.dns.proxyResolverIds]
    const [id] = ids.splice(index, 1)
    ids.splice(target, 0, id)
    return { ...current, dns: { ...current.dns, proxyResolverIds: ids } }
  }), [updateDraft])
  const setCacheEnabled = useCallback((enabled) => updateDNS(enabled ? { cacheEnabled: true } : { cacheEnabled: false, serveStale: false, staleTTLSeconds: 0 }), [updateDNS])
  const setServeStale = useCallback((enabled) => updateDNS(enabled ? { serveStale: true, staleTTLSeconds: MIN_STALE_TTL_SECONDS } : { serveStale: false, staleTTLSeconds: 0 }), [updateDNS])
  const setObservatoryInterval = useCallback((value) => updateDraft((current) => ({ ...current, observatory: { probeIntervalMinutes: Number(value) } })), [updateDraft])

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

  const draftValid = Boolean(draft && projection.value && validDNSSettings(draft.dns, projection.value.dns.resolverCatalog)
    && Number.isInteger(draft.observatory.probeIntervalMinutes)
    && draft.observatory.probeIntervalMinutes >= projection.value.observatory.minIntervalMinutes
    && draft.observatory.probeIntervalMinutes <= projection.value.observatory.maxIntervalMinutes)

  const previewDraft = useCallback(async () => {
    if (previewGate.current || submitGate.current || previewRef.current || pending || lifecycleBlocked || outcomeRequiresFreshRead || appliancePolicyUncertain || projectionRef.current?.editability !== 'editable') return false
    const currentDraft = draftRef.current
    if (!currentDraft || !validDNSSettings(currentDraft.dns, projectionRef.current.dns.resolverCatalog)) return false
    previewGate.current = true
    const id = ++requestSequence.current
    const epoch = sessionEpoch.current
    setRequestState({ id, kind: 'preview', canceled: false })
    setResult(null)
    try {
      const value = await postJSON('/api/v1/appliance/dns-observatory/preview', csrfToken, serializeDraft(currentDraft))
      if (id !== requestSequence.current || epoch !== sessionEpoch.current) {
        if (safeText(value?.previewToken)) safeCancel(value.previewToken, csrfToken)
        return false
      }
      if (!validPreview(value)) {
        if (safeText(value?.previewToken)) safeCancel(value.previewToken, csrfToken)
        throw new DNSRequestError('The Preview response is invalid.', { kind: 'malformed' })
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
  }, [appliancePolicyUncertain, csrfToken, lifecycleBlocked, loadPolicy, onUnauthorized, outcomeRequiresFreshRead, pending])

  const cancelPreview = useCallback((reason = 'canceled') => {
    requestSequence.current++
    const token = previewRef.current?.previewToken
    previewRef.current = null
    setPreview(null)
    setRequestState((current) => current?.kind === 'preview' ? { ...current, canceled: true } : current)
    safeCancel(token, csrfRef.current)
    if (reason === 'expired') setResult({ tone: 'warning', title: 'Preview expired', message: 'The unsent token was discarded. Create a fresh Preview.', outcome: 'expired' })
  }, [])

  const submitPreview = useCallback(async () => {
    const current = previewRef.current
    if (!current || current.noop || submitGate.current || lifecycleBlocked || outcomeRequiresFreshRead || appliancePolicyUncertain) return false
    submitGate.current = true
    onBeforeApply?.()
    const epoch = sessionEpoch.current
    const token = current.previewToken
    previewRef.current = null
    setPreview(null)
    setPending({ startedAt: new Date().toISOString() })
    setResult(null)
    setRefreshError('')
    let refreshAfter = false
    const isCurrentSession = () => epoch === sessionEpoch.current && csrfRef.current === csrfToken
    try {
      const value = await postJSON('/api/v1/appliance/dns-observatory/apply', csrfToken, { previewToken: token })
      if (!isCurrentSession()) return false
      if (!validApplyResult(value)) throw new DNSRequestError('The Apply response is invalid.', { kind: 'malformed' })
      setResult({
        tone: 'success',
        title: value.noop ? 'No effective DNS changes' : 'DNS and Observatory changes applied',
        message: value.noop ? 'The broker confirmed the policy was already effective.' : 'The broker verified the settings transaction and runtime restart.',
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
      if (mapped.outcome === 'unknown') {
        unprovenReadGeneration.current++
        unprovenReadPending.current = true
        onUnprovenApply?.()
      }
    } finally {
      if (isCurrentSession()) {
        setPending(null)
        submitGate.current = false
      }
    }
    if (refreshAfter && isCurrentSession()) {
      const refreshed = await loadPolicy({ force: true, rebase: true })
      if (!refreshed && isCurrentSession()) setRefreshError('The outcome is preserved, but the subsequent DNS policy refresh failed.')
    }
    return true
  }, [appliancePolicyUncertain, csrfToken, lifecycleBlocked, loadPolicy, onApplied, onBeforeApply, onUnauthorized, onUnprovenApply, outcomeRequiresFreshRead])

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
    submitGate.current = false
    hasLoaded.current = false
    draftInitialized.current = false
    dirtyRef.current = false
    draftRef.current = null
    unprovenReadGeneration.current++
    unprovenReadPending.current = false
    setProjection({ value: null, observedAt: '', loading: false, error: '' })
    setDraft(null)
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
    const wasActive = activeRef.current
    activeRef.current = active
    if (wasActive && !active) invalidateLivePreview()
  }, [active, invalidateLivePreview])

  useEffect(() => () => {
    sessionEpoch.current++
    requestSequence.current++
    safeCancel(previewRef.current?.previewToken, csrfRef.current)
  }, [])

  return useMemo(() => ({
    projection, draft, dirty, draftValid, refreshConfirmation, requestState, preview, pending, result, refreshError,
    lifecycleKnown, lifecycleBlocked, appliancePolicyUncertain, outcomeRequiresFreshRead, activate, loadPolicy, refreshPeerProjection,
    invalidateLivePreview, requestRefresh, discardAndRefresh, cancelRefresh, updateDNS, addResolver, removeResolver,
    moveResolver, setCacheEnabled, setServeStale, setObservatoryInterval, previewDraft, cancelPreview, submitPreview,
  }), [activate, addResolver, appliancePolicyUncertain, cancelPreview, cancelRefresh, dirty, discardAndRefresh, draft, draftValid, invalidateLivePreview, lifecycleBlocked, lifecycleKnown, loadPolicy, moveResolver, outcomeRequiresFreshRead, pending, preview, previewDraft, projection, refreshConfirmation, refreshError, removeResolver, requestRefresh, requestState, result, setCacheEnabled, setObservatoryInterval, setServeStale, submitPreview, updateDNS, refreshPeerProjection])
}

export function DNSLifecycleNotice({ controller, active, onOpenDNS }) {
  if (active) return null
  if (controller.pending) return <div className="lifecycle-banner running" role="status" aria-live="polite"><div><strong>DNS Apply is running</strong><span>The synchronous broker request remains active. Do not reload or submit another policy operation.</span></div><button className="ghost" type="button" onClick={onOpenDNS}>Open DNS</button></div>
  if (controller.result?.outcome === 'unknown') return <div className="lifecycle-banner danger" role="alert"><div><strong>{controller.result.title}</strong><span>{controller.result.message}</span></div><button className="ghost" type="button" onClick={onOpenDNS}>Open DNS</button></div>
  return null
}

const formatTime = (value) => value ? new Date(value).toLocaleString() : '—'
const booleanLabel = (value) => value ? 'Enabled' : 'Disabled'
const fallbackLabel = (value) => value === 'system' ? 'System fallback enabled' : 'System fallback disabled'
const signedCount = (value) => value > 0 ? `+${value}` : String(value)

export function DNSObservatorySection({ controller }) {
  const { projection, draft, dirty, refreshConfirmation, requestState, preview, pending, result, refreshError } = controller
  const value = projection.value
  const editable = value?.editability === 'editable' && draft
  const editorDisabled = !editable || controller.lifecycleBlocked || controller.outcomeRequiresFreshRead || controller.appliancePolicyUncertain || Boolean(preview) || Boolean(pending) || requestState?.kind === 'preview'
  const availableResolvers = editable ? value.dns.resolverCatalog.filter((item) => !draft.dns.proxyResolverIds.includes(item.id)) : []
  const labelFor = (id) => value?.dns?.resolverCatalog.find((item) => item.id === id)?.label || 'Unknown resolver selection'

  useEffect(() => { void controller.activate() }, [controller.activate])

  return <div className="section-stack dns-section">
    <section className="panel dns-heading">
      <div><span className="panel-label">DNS + Observatory</span><h2>Typed resolver policy</h2><p>Edit only safe resolver selections and bounded runtime behavior. Resolver endpoints, protected objects, domains and raw configuration remain hidden and source-owned.</p></div>
      <div className="dns-heading-actions"><small>{projection.observedAt ? `Observed ${formatTime(projection.observedAt)}` : 'Not loaded this session'}</small><button className="ghost" type="button" onClick={controller.requestRefresh} disabled={projection.loading || Boolean(pending)}>{projection.loading ? 'Reading…' : 'Refresh DNS policy'}</button></div>
    </section>
    {refreshConfirmation && <section className="dns-refresh-confirm" role="alert"><div><strong>Unsaved DNS changes</strong><span>Refreshing will replace the current draft with the latest server projection.</span></div><div className="dns-refresh-actions"><button type="button" onClick={controller.discardAndRefresh}>Discard changes and refresh</button><button className="ghost" type="button" onClick={controller.cancelRefresh}>Keep editing</button></div></section>}
    {projection.error && <div className="notice" role="alert">{projection.error}</div>}
    {refreshError && <div className="notice" role="alert">{refreshError}</div>}
    {projection.loading && !value && <div className="loading">Reading the current safe DNS and Observatory policy…</div>}
    {value && <DNSFacts projection={value} />}
    {value?.editability === 'drift-detected' && <div className="dns-blocked" role="alert"><strong>DNS editing is blocked by protected-state drift.</strong><span>Explicit Refresh may restore editability after the server proves compatibility. No repair or adoption is offered here.</span></div>}
    {value?.editability === 'unavailable' && <div className="dns-blocked danger" role="alert"><strong>DNS and Observatory policy is unavailable.</strong><span>No editable defaults were synthesized. Mutation controls remain disabled.</span></div>}

    {editable && <section className="panel dns-editor" aria-label="DNS and Observatory editor">
      <div className="dns-editor-heading"><div><span className="panel-label">Editable policy</span><h2>Resolvers and runtime behavior {dirty && <span className="chip amber">Unsaved</span>}</h2></div></div>
      <div className="dns-editor-grid">
        <fieldset className="dns-resolvers"><legend>Ordered proxy resolvers</legend><div className="dns-resolver-list">{draft.dns.proxyResolverIds.map((id, index) => <div className="dns-resolver-row" key={id}><strong>{labelFor(id)}</strong><div><button className="ghost" type="button" aria-label={`Move resolver up: ${labelFor(id)}`} onClick={() => controller.moveResolver(index, -1)} disabled={editorDisabled || index === 0}>Move up</button><button className="ghost" type="button" aria-label={`Move resolver down: ${labelFor(id)}`} onClick={() => controller.moveResolver(index, 1)} disabled={editorDisabled || index === draft.dns.proxyResolverIds.length - 1}>Move down</button><button className="ghost danger-action" type="button" aria-label={`Remove resolver: ${labelFor(id)}`} onClick={() => controller.removeResolver(index)} disabled={editorDisabled || draft.dns.proxyResolverIds.length <= 1}>Remove</button></div></div>)}</div>{availableResolvers.length > 0 && <div className="dns-resolver-add"><span>Available safe catalog</span>{availableResolvers.map((item) => <button className="ghost" type="button" key={item.id} onClick={() => controller.addResolver(item.id)} disabled={editorDisabled}>Add {item.label}</button>)}</div>}<small>At least one resolver must remain selected. Only server-provided labels are displayed.</small></fieldset>
        <fieldset><legend>Fallback</legend><label>Fallback mode<select aria-label="DNS fallback mode" value={draft.dns.fallbackMode} disabled={editorDisabled} onChange={(event) => controller.updateDNS({ fallbackMode: event.target.value })}><option value="system">System fallback enabled</option><option value="disabled">System fallback disabled</option></select></label><small>The fixed localhost fallback object remains present and source-owned; this mode controls whether it is used.</small></fieldset>
        <fieldset><legend>Cache and stale answers</legend><label className="dns-toggle"><input aria-label="Cache enabled" type="checkbox" checked={draft.dns.cacheEnabled} disabled={editorDisabled} onChange={(event) => controller.setCacheEnabled(event.target.checked)} /> Cache enabled</label><label className="dns-toggle"><input aria-label="Serve stale" type="checkbox" checked={draft.dns.serveStale} disabled={editorDisabled || !draft.dns.cacheEnabled} onChange={(event) => controller.setServeStale(event.target.checked)} /> Serve stale</label><label>Stale TTL seconds<input aria-label="Stale TTL seconds" type="number" min={MIN_STALE_TTL_SECONDS} max={MAX_STALE_TTL_SECONDS} step="1" value={draft.dns.staleTTLSeconds} disabled={editorDisabled || !draft.dns.cacheEnabled || !draft.dns.serveStale} onChange={(event) => controller.updateDNS({ staleTTLSeconds: Number(event.target.value) })} /></label></fieldset>
        <fieldset><legend>Query behavior</legend><label className="dns-toggle"><input aria-label="Parallel queries" type="checkbox" checked={draft.dns.parallelQueries} disabled={editorDisabled} onChange={(event) => controller.updateDNS({ parallelQueries: event.target.checked })} /> Parallel queries</label></fieldset>
        <fieldset><legend>Observatory cadence</legend><label>RTT sampling interval<select aria-label="Observatory cadence" value={draft.observatory.probeIntervalMinutes} disabled={editorDisabled} onChange={(event) => controller.setObservatoryInterval(event.target.value)}>{Array.from({ length: value.observatory.maxIntervalMinutes - value.observatory.minIntervalMinutes + 1 }, (_, offset) => value.observatory.minIntervalMinutes + offset).map((minutes) => <option key={minutes} value={minutes}>{minutes} minute{minutes === 1 ? '' : 's'}</option>)}</select></label><small>This cadence controls Observatory RTT sampling. The faster active-liveness plane remains separate.</small></fieldset>
      </div>
      {!controller.draftValid && <div className="notice warning" role="alert">Keep at least one resolver selected and use a whole stale TTL from 60 to 86400 seconds when Serve stale is enabled.</div>}
      <div className="dns-editor-actions"><div>{controller.lifecycleBlocked && <small className="dns-disabled-note">Lifecycle readiness is unavailable or maintenance/applying is active; new mutation requests are disabled.</small>}{controller.outcomeRequiresFreshRead && <small className="dns-disabled-note">Refresh and inspect the policy before creating another Preview.</small>}{controller.appliancePolicyUncertain && <small className="dns-disabled-note">An appliance-policy Apply has an unknown outcome. New mutations remain blocked until its controller completes a fresh read.</small>}</div><button type="button" onClick={controller.previewDraft} disabled={editorDisabled || projection.loading || !controller.draftValid}>{requestState?.kind === 'preview' ? 'Preparing Preview…' : 'Preview DNS changes'}</button></div>
    </section>}

    {requestState?.kind === 'preview' && <div className="notice neutral" role="status">{requestState.canceled ? 'Discarding the late DNS Preview response…' : 'Preparing a fresh DNS semantic Preview…'} {!requestState.canceled && <button className="inline-link" type="button" onClick={() => controller.cancelPreview()}>Cancel Preview</button>}</div>}
    {pending && <div className="operation-running dns-operation" role="status" aria-live="polite"><span className="spinner" aria-hidden="true"></span><div><strong>DNS Apply is running</strong><p>No progress percentage is available. The broker may need its bounded transaction and recovery window.</p></div></div>}
    {result && <div className={`operation-result dns-result ${result.tone}`} role={result.tone === 'error' ? 'alert' : 'status'} data-testid="dns-result"><div><strong>{result.title}</strong><p>{result.message}</p></div></div>}
    {preview && <DNSPreview preview={preview} labelFor={labelFor} busy={Boolean(pending)} applyDisabled={controller.lifecycleBlocked || controller.outcomeRequiresFreshRead || controller.appliancePolicyUncertain} onCancel={() => controller.cancelPreview()} onConfirm={controller.submitPreview} />}
  </div>
}

function DNSFacts({ projection }) {
  const label = projection.editability === 'editable' ? 'Editable' : projection.editability === 'drift-detected' ? 'Drift detected' : 'Unavailable'
  return <section className="panel dns-facts" aria-label="DNS source-owned facts"><div className="dns-facts-heading"><div><span className="panel-label">Source-owned context</span><h2>Locked safety boundary</h2></div><span className={`chip ${projection.editability === 'editable' ? 'green' : 'amber'}`}>{label}</span></div><div className="dns-facts-grid"><Fact label="Query strategy" value={projection.dns.locked.queryStrategy} /><Fact label="Leak prevention" value={booleanLabel(projection.dns.locked.leakPreventionEnabled)} /><Fact label="Fixed system fallback object" value={projection.dns.locked.systemFallbackPresent ? 'Present / locked' : 'Not proven'} /><Fact label="Baseline proxy domains" value={projection.dns.proxyDomainCounts.baseline} /><Fact label="Routing-derived proxy domains" value={projection.dns.proxyDomainCounts.derived} /><Fact label="Allowed Observatory cadence" value={`${projection.observatory.minIntervalMinutes}–${projection.observatory.maxIntervalMinutes} minutes`} /></div></section>
}

function Fact({ label, value }) { return <div><span>{label}</span><strong>{value}</strong></div> }

function DNSPreview({ preview, labelFor, busy, applyDisabled, onCancel, onConfirm }) {
  const regionRef = useRef(null)
  const diff = preview.diff
  const resolverList = (values) => values.length ? values.map((id) => labelFor(id)).join(', ') : 'None'
  useEffect(() => { regionRef.current?.focus() }, [])
  return <section ref={regionRef} className="panel dns-preview" aria-label="DNS Preview confirmation" aria-live="polite" tabIndex="-1"><div className="dns-preview-heading"><div><span className="panel-label">Semantic Preview</span><h2>{preview.noop ? 'No effective changes' : 'Review DNS and Observatory changes'}</h2><p>Only server-derived semantic differences are shown; protected policy bodies remain hidden.</p></div><small>Expires {formatTime(preview.expiresAt)}</small></div><div className="dns-diff-grid"><Fact label="Resolvers added" value={resolverList(diff.dns.resolverIdsAdded)} /><Fact label="Resolvers removed" value={resolverList(diff.dns.resolverIdsRemoved)} /><Fact label="Resolver order" value={resolverList(diff.dns.resolverIdsReordered)} /><Fact label="Fallback" value={`${fallbackLabel(diff.dns.fallbackModeBefore)} → ${fallbackLabel(diff.dns.fallbackModeAfter)}`} /><Fact label="Cache" value={`${booleanLabel(diff.dns.cacheEnabledBefore)} → ${booleanLabel(diff.dns.cacheEnabledAfter)}`} /><Fact label="Serve stale" value={`${booleanLabel(diff.dns.serveStaleBefore)} → ${booleanLabel(diff.dns.serveStaleAfter)}`} /><Fact label="Stale TTL seconds" value={`${diff.dns.staleTTLSecondsBefore} → ${diff.dns.staleTTLSecondsAfter}`} /><Fact label="Parallel queries" value={`${booleanLabel(diff.dns.parallelQueriesBefore)} → ${booleanLabel(diff.dns.parallelQueriesAfter)}`} /><Fact label="Observatory cadence" value={`${diff.observatory.probeIntervalMinutesBefore} → ${diff.observatory.probeIntervalMinutesAfter} minutes`} /><Fact label="Derived proxy domains" value={`${diff.derivedProxyDomainCountBefore} → ${diff.derivedProxyDomainCountAfter} (${signedCount(diff.derivedProxyDomainCountDelta)})`} /><Fact label="Runtime restart" value={diff.restartRequired ? 'Required' : 'Not required'} /></div>{!preview.noop && <p className="dns-preview-warning">Confirming Apply sends only the one-shot Preview token. Managed Xray/XKeen runtime will restart and proxy traffic may be briefly interrupted.</p>}{preview.noop && <p className="dns-preview-note">No Apply action is available for a no-op Preview. Cancel it to consume the token.</p>}<div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel Preview</button>{!preview.noop && <button type="button" onClick={onConfirm} disabled={busy || applyDisabled}>Apply DNS changes</button>}</div></section>
}
