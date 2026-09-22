import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

const POLICY_FIELDS = Object.freeze([
  'schemaVersion',
  'probeIntervalSeconds',
  'failureThreshold',
  'adaptiveCadenceMinutes',
  'adaptiveChallengerLimit',
  'minimumDwellMinutes',
  'qualityHysteresisPercent',
])

const FIELD_LABELS = Object.freeze({
  probeIntervalSeconds: 'Active probe interval',
  failureThreshold: 'Failure threshold',
  adaptiveCadenceMinutes: 'Adaptive cadence',
  adaptiveChallengerLimit: 'Adaptive challengers',
  minimumDwellMinutes: 'Minimum dwell',
  qualityHysteresisPercent: 'Quality hysteresis',
})

const safeText = (value) => typeof value === 'string' && value.length > 0 && value.length <= 256
const exactKeys = (value, keys) => value && typeof value === 'object'
  && Object.keys(value).length === keys.length
  && keys.every((key) => Object.prototype.hasOwnProperty.call(value, key))

const validPolicy = (value) => exactKeys(value, POLICY_FIELDS)
  && value.schemaVersion === 1
  && Number.isInteger(value.probeIntervalSeconds) && value.probeIntervalSeconds >= 60 && value.probeIntervalSeconds <= 300 && value.probeIntervalSeconds % 30 === 0
  && Number.isInteger(value.failureThreshold) && value.failureThreshold >= 2 && value.failureThreshold <= 5
  && Number.isInteger(value.adaptiveCadenceMinutes) && value.adaptiveCadenceMinutes >= 180 && value.adaptiveCadenceMinutes <= 1440 && value.adaptiveCadenceMinutes % 30 === 0
  && Number.isInteger(value.adaptiveChallengerLimit) && value.adaptiveChallengerLimit >= 1 && value.adaptiveChallengerLimit <= 5
  && Number.isInteger(value.minimumDwellMinutes) && value.minimumDwellMinutes >= 30 && value.minimumDwellMinutes <= 1440
  && Number.isInteger(value.qualityHysteresisPercent) && value.qualityHysteresisPercent >= 10 && value.qualityHysteresisPercent <= 50

const validCeilings = (value) => value && typeof value === 'object'
  && value.maxCandidates === 6
  && value.candidateDownloadMiB === 16
  && value.candidateUploadMiB === 8
  && value.candidateMaxSeconds === 30
  && value.generationMaxMiB === 144
  && value.generationMaxSeconds === 180
  && value.transportIdentity === 'source-owned'
  && value.rttGuard === 'source-owned'
  && value.scoring === 'source-owned'

const validProjection = (value) => value && validPolicy(value.policy)
  && ['default', 'persisted'].includes(value.source)
  && (value.reasonCode == null || safeText(value.reasonCode))
  && validCeilings(value.hardCeilings)
  && value.adaptive && typeof value.adaptive === 'object' && safeText(value.adaptive.state)

const validChange = (value) => value && typeof value === 'object'
  && Object.prototype.hasOwnProperty.call(FIELD_LABELS, value.field)
  && Number.isInteger(value.before) && Number.isInteger(value.after)

const validPreview = (value) => value && safeText(value.previewToken)
  && Number.isFinite(Date.parse(value.expiresAt))
  && validPolicy(value.before) && validPolicy(value.after)
  && Array.isArray(value.changes) && value.changes.every(validChange)
  && typeof value.noop === 'boolean'
  && value.restartRequired === false
  && typeof value.nextRunTimeChanges === 'boolean'

const validApplyResult = (value) => value && validPolicy(value.policy)
  && ['default', 'persisted'].includes(value.source)
  && Array.isArray(value.changes) && value.changes.every(validChange)
  && typeof value.noop === 'boolean'
  && value.restartRequired === false
  && typeof value.nextRunTimeChanged === 'boolean'

class PerformancePolicyError extends Error {
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
    throw new PerformancePolicyError('The response was lost.', { kind: 'network' })
  }
  const text = await response.text()
  let body = {}
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      if (response.ok) throw new PerformancePolicyError('The server returned an unreadable response.', { status: response.status, kind: 'malformed' })
    }
  }
  if (!response.ok) {
    throw new PerformancePolicyError('The performance policy request was rejected.', {
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
  void postJSON('/api/v1/performance/policy/cancel', csrfToken, { previewToken: token }).catch(() => {})
}

const clonePolicy = (value) => POLICY_FIELDS.reduce((result, key) => ({ ...result, [key]: value[key] }), {})
const serializePolicy = clonePolicy
const formatTime = (value) => value ? new Date(value).toLocaleString() : '—'

const errorResult = (cause, apply) => {
  switch (cause.code) {
    case 'busy': return { tone: 'warning', title: 'Performance owner is busy', message: apply ? 'The one-shot token was consumed. Wait for current performance or lifecycle work, then create a fresh Preview.' : 'Wait for current performance or lifecycle work, then try again.' }
    case 'preview-stale': return { tone: 'warning', title: 'Preview is stale', message: 'The policy authority changed. The token was not replayed; refresh and create a fresh Preview.', refresh: true }
    case 'preview-expired': return { tone: 'warning', title: 'Preview expired', message: 'Create a fresh Preview before applying.' }
    case 'save-failed': return { tone: 'error', title: 'Policy was not saved', message: 'The bounded authority could not be replaced. Refresh before another attempt.', refresh: true }
    case 'invalid-request': return { tone: 'warning', title: 'Policy was rejected', message: 'Use only the six bounded whole-number fields and create a fresh Preview.' }
    case 'unavailable': return { tone: 'error', title: 'Performance policy unavailable', message: 'The server could not prove the policy authority or runtime owner.' }
    default: return { tone: 'error', title: apply ? 'Apply outcome unavailable' : 'Preview unavailable', message: apply ? 'The request was not replayed. Refresh and inspect before another mutation.' : 'No mutation request was submitted.' }
  }
}

export function usePerformancePolicyController({ csrfToken, lifecycle, performanceBusy, onUnauthorized, active }) {
  const [projection, setProjection] = useState({ value: null, observedAt: '', loading: false, error: '' })
  const [draft, setDraft] = useState(null)
  const [dirty, setDirty] = useState(false)
  const [refreshConfirmation, setRefreshConfirmation] = useState(false)
  const [preview, setPreview] = useState(null)
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState(null)
  const readGate = useRef(false)
  const submitGate = useRef(false)
  const loaded = useRef(false)
  const epoch = useRef(0)
  const csrfRef = useRef(csrfToken)
  const sessionCSRFRef = useRef(csrfToken)
  const activeRef = useRef(active)
  const previewRef = useRef(preview)
  const draftRef = useRef(draft)
  const dirtyRef = useRef(dirty)

  csrfRef.current = csrfToken
  previewRef.current = preview
  draftRef.current = draft
  dirtyRef.current = dirty

  const lifecycleBlocked = !lifecycle || lifecycle.maintenance || lifecycle.applying
  const mutationBlocked = lifecycleBlocked || performanceBusy

  const clearPreview = useCallback((cancel = true) => {
    const token = previewRef.current?.previewToken
    previewRef.current = null
    setPreview(null)
    if (cancel) safeCancel(token, csrfRef.current)
  }, [])

  const loadPolicy = useCallback(async ({ force = false, rebase = true } = {}) => {
    if (readGate.current || (!force && loaded.current)) return false
    readGate.current = true
    const requestEpoch = epoch.current
    setProjection((current) => ({ ...current, loading: true, error: '' }))
    try {
      const value = await requestJSON('/api/v1/performance/policy')
      if (!validProjection(value)) throw new PerformancePolicyError('The performance policy projection is invalid.', { kind: 'malformed' })
      if (requestEpoch !== epoch.current) return false
      loaded.current = true
      setProjection({ value, observedAt: new Date().toISOString(), loading: false, error: '' })
      if (rebase || !draftRef.current) {
        const next = clonePolicy(value.policy)
        draftRef.current = next
        dirtyRef.current = false
        setDraft(next)
        setDirty(false)
      }
      return true
    } catch (cause) {
      if (requestEpoch !== epoch.current) return false
      if (cause.status === 401) onUnauthorized()
      loaded.current = false
      setProjection((current) => ({ ...current, loading: false, error: 'The performance policy projection is unavailable.' }))
      return false
    } finally {
      readGate.current = false
    }
  }, [onUnauthorized])

  const updateField = useCallback((field, value) => {
    if (!Object.prototype.hasOwnProperty.call(FIELD_LABELS, field)) return
    clearPreview()
    const numeric = Number(value)
    setDraft((current) => {
      if (!current) return current
      const next = { ...current, [field]: numeric }
      draftRef.current = next
      return next
    })
    dirtyRef.current = true
    setDirty(true)
    setResult(null)
  }, [clearPreview])

  const requestRefresh = useCallback(() => {
    if (dirtyRef.current) {
      setRefreshConfirmation(true)
      return
    }
    clearPreview()
    void loadPolicy({ force: true, rebase: true })
  }, [clearPreview, loadPolicy])

  const discardAndRefresh = useCallback(() => {
    setRefreshConfirmation(false)
    dirtyRef.current = false
    setDirty(false)
    clearPreview()
    void loadPolicy({ force: true, rebase: true })
  }, [clearPreview, loadPolicy])

  const previewDraft = useCallback(async () => {
    if (submitGate.current || previewRef.current || mutationBlocked || !validPolicy(draftRef.current)) return
    submitGate.current = true
    setResult(null)
    const requestEpoch = epoch.current
    try {
      const value = await postJSON('/api/v1/performance/policy/preview', csrfToken, serializePolicy(draftRef.current))
      if (requestEpoch !== epoch.current) {
        safeCancel(value?.previewToken, csrfToken)
        return
      }
      if (!validPreview(value)) {
        safeCancel(value?.previewToken, csrfToken)
        throw new PerformancePolicyError('The Preview response is invalid.', { kind: 'malformed' })
      }
      previewRef.current = value
      setPreview(value)
    } catch (cause) {
      if (requestEpoch !== epoch.current) return
      if (cause.status === 401) onUnauthorized()
      setResult(errorResult(cause, false))
    } finally {
      submitGate.current = false
    }
  }, [csrfToken, mutationBlocked, onUnauthorized])

  const applyPreview = useCallback(async () => {
    const current = previewRef.current
    if (!current || submitGate.current || mutationBlocked) return
    submitGate.current = true
    previewRef.current = null
    setPreview(null)
    setPending(true)
    setResult(null)
    const requestEpoch = epoch.current
    let shouldRefresh = false
    try {
      const value = await postJSON('/api/v1/performance/policy/apply', csrfToken, { previewToken: current.previewToken })
      if (requestEpoch !== epoch.current) return
      if (!validApplyResult(value)) throw new PerformancePolicyError('The Apply response is invalid.', { kind: 'malformed' })
      setResult({ tone: 'success', title: value.noop ? 'Policy already effective' : 'Performance policy applied', message: value.noop ? 'No file or runtime schedule was changed.' : 'Subsequent liveness and adaptive cycles will use the new policy. No benchmark or restart was triggered.' })
      dirtyRef.current = false
      setDirty(false)
      shouldRefresh = true
    } catch (cause) {
      if (requestEpoch !== epoch.current) return
      if (cause.status === 401) onUnauthorized()
      const mapped = errorResult(cause, true)
      setResult(mapped)
      shouldRefresh = Boolean(mapped.refresh)
    } finally {
      if (requestEpoch === epoch.current) {
        setPending(false)
        submitGate.current = false
      }
    }
    if (shouldRefresh && requestEpoch === epoch.current) await loadPolicy({ force: true, rebase: true })
  }, [csrfToken, loadPolicy, mutationBlocked, onUnauthorized])

  useEffect(() => {
    if (active) void loadPolicy()
  }, [active, loadPolicy])

  useEffect(() => {
    if (!preview?.expiresAt) return undefined
    const delay = Math.max(0, Date.parse(preview.expiresAt) - Date.now())
    const timer = window.setTimeout(() => {
      if (previewRef.current?.previewToken === preview.previewToken) {
        clearPreview()
        setResult({ tone: 'warning', title: 'Preview expired', message: 'The token was discarded. Create a fresh Preview.' })
      }
    }, Math.min(delay, 2_147_483_647))
    return () => window.clearTimeout(timer)
  }, [clearPreview, preview])

  useEffect(() => {
    const previousCSRF = sessionCSRFRef.current
    if (previousCSRF === csrfToken) return
    epoch.current++
    loaded.current = false
    safeCancel(previewRef.current?.previewToken, previousCSRF)
    previewRef.current = null
    setPreview(null)
    setProjection({ value: null, observedAt: '', loading: false, error: '' })
    setDraft(null)
    setDirty(false)
    setRefreshConfirmation(false)
    setPending(false)
    setResult(null)
    sessionCSRFRef.current = csrfToken
  }, [clearPreview, csrfToken])

  useEffect(() => {
    const wasActive = activeRef.current
    activeRef.current = active
    if (wasActive && !active) clearPreview()
  }, [active, clearPreview])

  useEffect(() => () => {
    epoch.current++
    safeCancel(previewRef.current?.previewToken, csrfRef.current)
  }, [])

  return useMemo(() => ({
    projection, draft, dirty, refreshConfirmation, preview, pending, result,
    lifecycleBlocked, performanceBusy, mutationBlocked, updateField, requestRefresh,
    discardAndRefresh, cancelRefresh: () => setRefreshConfirmation(false), previewDraft,
    cancelPreview: clearPreview, applyPreview,
  }), [applyPreview, clearPreview, dirty, discardAndRefresh, draft, lifecycleBlocked, mutationBlocked, pending, performanceBusy, preview, previewDraft, projection, refreshConfirmation, requestRefresh, result, updateField])
}

export function PerformancePolicySection({ controller }) {
  const { projection, draft, preview, pending, result } = controller
  const value = projection.value
  const disabled = controller.mutationBlocked || pending || Boolean(preview) || projection.loading

  return <div className="section-stack performance-policy-section">
    <section className="panel performance-policy-heading">
      <div><span className="panel-label">Performance</span><h2>Bounded selection policy</h2><p>Configure only conservative liveness and adaptive-selection bounds. Transport identity, traffic ceilings, timeouts, RTT guards and scoring stay source-owned.</p></div>
      <div><small>{projection.observedAt ? `Observed ${formatTime(projection.observedAt)}` : 'Not loaded this session'}</small><button className="ghost" type="button" onClick={controller.requestRefresh} disabled={projection.loading || pending}>{projection.loading ? 'Reading…' : 'Refresh policy'}</button></div>
    </section>

    {controller.refreshConfirmation && <section className="performance-refresh-confirm" role="alert"><div><strong>Unsaved performance changes</strong><span>Refreshing will replace the current draft with the latest server projection.</span></div><div><button type="button" onClick={controller.discardAndRefresh}>Discard changes and refresh</button><button className="ghost" type="button" onClick={controller.cancelRefresh}>Keep editing</button></div></section>}
    {projection.error && <div className="notice" role="alert">{projection.error}</div>}
    {projection.loading && !value && <div className="loading">Reading the bounded performance policy…</div>}

    {value && <>
      {value.reasonCode && <div className="performance-fail-closed" role="alert"><strong>Persisted policy failed closed to source defaults.</strong><span>Reason: {value.reasonCode}. Review the six defaults below, then explicitly Preview and Apply to replace the invalid file.</span></div>}
      <section className="panel performance-ceilings" aria-label="Source-owned performance ceilings">
        <div className="performance-card-heading"><div><span className="panel-label">Source-owned ceilings</span><h2>Fixed traffic and time envelope</h2></div><span className="chip neutral">Source-owned</span></div>
        <div className="performance-facts-grid">
          <Fact label="Maximum candidates" value={value.hardCeilings.maxCandidates} />
          <Fact label="Candidate download" value={`${value.hardCeilings.candidateDownloadMiB} MiB`} />
          <Fact label="Candidate upload" value={`${value.hardCeilings.candidateUploadMiB} MiB`} />
          <Fact label="Candidate wall time" value={`${value.hardCeilings.candidateMaxSeconds} seconds`} />
          <Fact label="Generation traffic" value={`${value.hardCeilings.generationMaxMiB} MiB`} />
          <Fact label="Generation wall time" value={`${value.hardCeilings.generationMaxSeconds} seconds`} />
          <Fact label="Transport identity" value="Source-owned" />
          <Fact label="RTT guard and scoring" value="Source-owned" />
        </div>
      </section>

      <section className="panel performance-policy-editor" aria-label="Performance policy editor">
        <div className="performance-card-heading"><div><span className="panel-label">Editable policy</span><h2>Subsequent-cycle behavior {controller.dirty && <span className="chip amber">Unsaved</span>}</h2><p>Current running operations keep their frozen policy. Apply never runs a benchmark, writes selection state or restarts the runtime.</p></div><span className={`chip ${value.source === 'persisted' ? 'green' : 'neutral'}`}>{value.source === 'persisted' ? 'Persisted' : 'Source defaults'}</span></div>
        {draft && <div className="performance-policy-grid">
          <NumberField label="Active probe interval" suffix="seconds" value={draft.probeIntervalSeconds} min={60} max={300} step={30} disabled={disabled} onChange={(next) => controller.updateField('probeIntervalSeconds', next)} />
          <NumberField label="Failure threshold" suffix="failed cycles" value={draft.failureThreshold} min={2} max={5} step={1} disabled={disabled} onChange={(next) => controller.updateField('failureThreshold', next)} />
          <NumberField label="Adaptive cadence" suffix="minutes" value={draft.adaptiveCadenceMinutes} min={180} max={1440} step={30} disabled={disabled} onChange={(next) => controller.updateField('adaptiveCadenceMinutes', next)} />
          <NumberField label="Adaptive challengers" suffix="plus current, max 6 total" value={draft.adaptiveChallengerLimit} min={1} max={5} step={1} disabled={disabled} onChange={(next) => controller.updateField('adaptiveChallengerLimit', next)} />
          <NumberField label="Minimum dwell" suffix="minutes" value={draft.minimumDwellMinutes} min={30} max={1440} step={1} disabled={disabled} onChange={(next) => controller.updateField('minimumDwellMinutes', next)} />
          <NumberField label="Quality hysteresis" suffix="percent" value={draft.qualityHysteresisPercent} min={10} max={50} step={1} disabled={disabled} onChange={(next) => controller.updateField('qualityHysteresisPercent', next)} />
        </div>}
        <div className="performance-policy-actions"><div>{controller.lifecycleBlocked && <small>Lifecycle readiness is unavailable or maintenance/applying is active.</small>}{controller.performanceBusy && <small>A manual, adaptive or legacy performance owner is active.</small>}</div><button type="button" onClick={controller.previewDraft} disabled={disabled || !validPolicy(draft)}>Preview performance changes</button></div>
      </section>

      <section className="panel performance-adaptive-fact" aria-label="Current adaptive status"><span className="panel-label">Current adaptive status</span><div className="performance-facts-grid"><Fact label="State" value={value.adaptive.state} /><Fact label="Next run" value={formatTime(value.adaptive.nextRunAt)} /><Fact label="Generation" value={value.adaptive.generation ?? 0} /><Fact label="Last reason" value={value.adaptive.reasonCode || '—'} /></div></section>
    </>}

    {pending && <div className="operation-running" role="status"><span className="spinner" aria-hidden="true"></span><div><strong>Performance policy Apply is running</strong><p>The request will not be replayed after a lost response.</p></div></div>}
    {result && <div className={`operation-result performance-policy-result ${result.tone}`} role={result.tone === 'error' ? 'alert' : 'status'} data-testid="performance-policy-result"><div><strong>{result.title}</strong><p>{result.message}</p></div></div>}
    {preview && <PerformancePreview preview={preview} disabled={controller.mutationBlocked || pending} onCancel={() => controller.cancelPreview()} onApply={controller.applyPreview} />}
  </div>
}

function NumberField({ label, suffix, value, min, max, step, disabled, onChange }) {
  return <label><span>{label}</span><input aria-label={label} type="number" value={value} min={min} max={max} step={step} disabled={disabled} onChange={(event) => onChange(event.target.value)} /><small>{min}–{max} {suffix}</small></label>
}

function PerformancePreview({ preview, disabled, onCancel, onApply }) {
  const region = useRef(null)
  useEffect(() => { region.current?.focus() }, [])
  return <section ref={region} className="panel performance-preview" tabIndex="-1" aria-live="polite" aria-label="Performance policy Preview">
    <div className="performance-card-heading"><div><span className="panel-label">Semantic Preview</span><h2>{preview.noop ? 'No effective policy change' : 'Review performance policy changes'}</h2><p>No restart or benchmark is required.</p></div><small>Expires {formatTime(preview.expiresAt)}</small></div>
    <div className="performance-change-list">{preview.changes.length ? preview.changes.map((change) => <div key={change.field}><span>{FIELD_LABELS[change.field]}</span><strong>{change.before} → {change.after}</strong></div>) : <div><span>Effective policy</span><strong>Unchanged</strong></div>}</div>
    <div className="performance-preview-facts"><span>Runtime restart: <strong>No</strong></span><span>Next adaptive run changes: <strong>{preview.nextRunTimeChanges ? 'Yes' : 'No'}</strong></span></div>
    <div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={disabled}>Cancel Preview</button><button type="button" onClick={onApply} disabled={disabled}>{preview.noop ? 'Confirm no-op' : 'Apply performance policy'}</button></div>
  </section>
}

function Fact({ label, value }) { return <div><span>{label}</span><strong>{value}</strong></div> }
