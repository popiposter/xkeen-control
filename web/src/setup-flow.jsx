import { useCallback, useEffect, useMemo, useState } from 'react'

const SETUP_STATES = new Set(['fresh', 'previewable', 'applying', 'blocked', 'maintenance', 'ready'])
const SAFE_REASON = /^[a-z][a-z0-9-]{0,63}$/
const REASON_LABELS = Object.freeze({
  fresh: 'Fresh product layout',
  'already-configured': 'Setup is already complete',
  'layout-partial': 'A partial managed layout was found',
  'layout-mixed': 'A mixed or unsupported layout was found',
  'authority-present': 'Appliance authority already exists',
  'journal-pending': 'A recovery transaction is pending',
  maintenance: 'Setup is in maintenance',
  'component-source-unavailable': 'Fixed component metadata is unavailable',
  'candidate-stale': 'The fixed candidate changed',
  'candidate-rejected': 'The fixed candidate was rejected',
  'resource-insufficient': 'The appliance does not have enough free space',
  'transaction-restored': 'Setup failed and fresh state was restored',
  'transaction-unproven': 'Setup outcome is not proven',
  'runtime-unavailable': 'The managed runtime is unavailable',
  'verification-failed': 'The installed setup failed verification',
})

const requestJSON = async (path, csrfToken, body) => {
  let response
  try {
    response = await fetch(path, {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
      body: JSON.stringify(body),
    })
  } catch {
    const error = new Error('The setup response was lost. Do not retry automatically.')
    error.kind = 'network'
    throw error
  }
  const text = await response.text()
  let value = {}
  if (text) {
    try {
      value = JSON.parse(text)
    } catch {
      const error = new Error('The setup server returned an unreadable response.')
      error.kind = 'malformed'
      throw error
    }
  }
  if (!response.ok) {
    const error = new Error(typeof value.error === 'string' ? value.error : `Setup request failed (${response.status})`)
    error.status = response.status
    error.code = typeof value.code === 'string' ? value.code : ''
    error.reasonCode = typeof value.reasonCode === 'string' ? value.reasonCode : ''
    throw error
  }
  return value
}

const shortDigest = (value) => value ? `${String(value).slice(0, 12)}…` : '—'
const setupReasonLabel = (value) => {
  const reason = typeof value === 'string' && SAFE_REASON.test(value) ? value : 'maintenance'
  return REASON_LABELS[reason] || 'Setup is unavailable'
}
const formatExpiry = (value) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date.toLocaleString() : '—'
}

const safePlan = (value) => {
  if (!value || value.schemaVersion !== 1 || value.productDefault !== true || value.emptyRegistry !== true) return null
  if (!value.xray || !value.geodata || !value.xkeen || !value.lifecycle) return null
  return value
}

const setupErrorMessage = (error) => {
  switch (error?.code) {
    case 'preview-expired': return 'The setup Preview expired. Prepare a fresh setup plan.'
    case 'preview-stale': return 'The fixed candidate changed. Prepare a fresh setup plan.'
    case 'layout-blocked': return 'The current layout is not an eligible fresh setup target.'
    case 'busy': return 'Another lifecycle transaction is active. Wait for it to finish.'
    case 'component-source-unavailable': return 'Fixed component metadata is unavailable. No retry was started.'
    case 'candidate-rejected': return 'The fixed setup candidate was rejected before activation.'
    case 'resource-insufficient': return 'The appliance does not have enough bounded staging space.'
    case 'transaction-restored': return 'Setup failed; the server verified that fresh state was restored.'
    case 'transaction-unproven': return 'Setup outcome is not proven. Inspect the appliance before any new action.'
    case 'runtime-unavailable': return 'The managed runtime did not become ready.'
    case 'verification-failed': return 'The installed setup failed its final verification.'
    case 'maintenance': return 'Setup is in maintenance and cannot accept a new action.'
    default: return error?.message || 'Setup is unavailable. Inspect current state before retrying.'
  }
}

export function SetupFlow({ setup, csrfToken, onRefresh, onUnauthorized }) {
  const [preview, setPreview] = useState(null)
  const [confirming, setConfirming] = useState(false)
  const [request, setRequest] = useState('')
  const [error, setError] = useState('')
  const [result, setResult] = useState(null)
  const currentState = SETUP_STATES.has(setup?.state) ? setup.state : ''
  const plan = useMemo(() => safePlan(preview?.plan), [preview])
  const eligible = setup?.eligible === true && (currentState === 'fresh' || currentState === 'previewable')

  useEffect(() => {
    if (currentState === 'ready' || currentState === 'applying' || currentState === 'blocked' || currentState === 'maintenance') {
      setPreview(null)
      setConfirming(false)
    }
  }, [currentState])

  const handleUnauthorized = useCallback((cause) => {
    if (cause?.status === 401 || cause?.status === 403) onUnauthorized?.()
  }, [onUnauthorized])

  const prepare = async () => {
    setRequest('preview')
    setError('')
    setResult(null)
    setConfirming(false)
    try {
      const value = await requestJSON('/api/v1/setup/preview', csrfToken, {})
      const nextPlan = safePlan(value?.plan)
      if (value?.operation !== 'setup' || typeof value.previewToken !== 'string' || !nextPlan) {
        throw new Error('The setup plan was not recognized. No action was started.')
      }
      setPreview(value)
    } catch (cause) {
      handleUnauthorized(cause)
      setError(setupErrorMessage(cause))
    } finally {
      setRequest('')
    }
  }

  const cancel = async () => {
    const token = preview?.previewToken
    setPreview(null)
    setConfirming(false)
    setError('')
    if (!token) return
    setRequest('cancel')
    try {
      await requestJSON('/api/v1/setup/cancel', csrfToken, { previewToken: token })
    } catch (cause) {
      handleUnauthorized(cause)
      setError(setupErrorMessage(cause))
    } finally {
      setRequest('')
    }
  }

  const apply = async () => {
    const token = preview?.previewToken
    if (!token || !plan) return
    setRequest('apply')
    setError('')
    setConfirming(false)
    try {
      const value = await requestJSON('/api/v1/setup/apply', csrfToken, { previewToken: token })
      if (value?.operation !== 'setup' || value?.state !== 'ready') throw new Error('The setup result was not recognized.')
      setResult(value)
      setPreview(null)
      await onRefresh?.()
    } catch (cause) {
      handleUnauthorized(cause)
      setError(setupErrorMessage(cause))
    } finally {
      setRequest('')
    }
  }

  if (!currentState) return null
  if (currentState === 'ready') return null

  return <section className="panel setup-flow" aria-label="Setup Mode">
    <div className="setup-flow-heading">
      <div><span className="panel-label">Setup Mode</span><h2>Prepare the managed appliance</h2><p>One server-owned fresh-install transaction creates the product default policy, an empty node registry, and the fixed qualified component set.</p></div>
      <span className={`chip ${eligible ? 'amber' : currentState === 'applying' ? 'blue' : 'neutral'}`}>{currentState}</span>
    </div>
    {currentState === 'blocked' || currentState === 'maintenance'
      ? <div className="setup-flow-blocked" role="status"><strong>{setupReasonLabel(setup.reasonCode)}</strong><small>Setup accepts no repair or partial-install operation. Resolve the reported state through the supported authority/recovery path.</small><code>{setup.reasonCode || 'maintenance'}</code></div>
      : currentState === 'applying'
        ? <div className="setup-flow-running" role="status" aria-live="polite"><span className="spinner" aria-hidden="true"></span><div><strong>Setup is applying</strong><small>The bounded transaction is active. Do not reload or submit another lifecycle operation.</small></div></div>
        : <>
          {error && <div className="notice" role="alert">{error}</div>}
          {result && <div className="notice success" role="status">Setup completed. The dashboard was refreshed; Nodes is ready for operator import or add.</div>}
          {!preview && <div className="setup-flow-actions"><div><strong>Fresh layout eligible</strong><small>No component bodies are downloaded during Prepare.</small></div><button type="button" onClick={prepare} disabled={!eligible || Boolean(request)}>{request === 'preview' ? 'Preparing…' : 'Prepare setup'}</button></div>}
          {preview && plan && <SetupPlan plan={plan} expiresAt={preview.expiresAt} confirming={confirming} busy={Boolean(request)} onCancel={cancel} onConfirm={() => setConfirming(true)} onApply={apply} />}
        </>}
  </section>
}

function SetupPlan({ plan, expiresAt, confirming, busy, onCancel, onConfirm, onApply }) {
  return <div className="setup-plan" aria-label="Setup plan">
    <div className="setup-plan-summary"><strong>Fixed setup plan</strong><small>Expires {formatExpiry(expiresAt)}</small></div>
    <div className="setup-plan-grid">
      <div><span>Policy</span><strong>Product default</strong><small>Typed authority · no browser input</small></div>
      <div><span>Nodes</span><strong>Empty canonical registry</strong><small>Ready for existing Nodes flows</small></div>
      <div><span>Xray</span><strong>{plan.xray.version || plan.xray.tag || 'Qualified stable candidate'}</strong><small>{shortDigest(plan.xray.sha256)}</small></div>
      <div><span>Geodata</span><strong>{plan.geodata.items?.length || 0} fixed assets</strong><small>{plan.geodata.generation || 'Qualified complete set'}</small></div>
      <div><span>XKeen</span><strong>{plan.xkeen.version || plan.xkeen.tag || 'Qualified dev candidate'}</strong><small>{shortDigest(plan.xkeen.generationSha256)}</small></div>
      <div><span>Lifecycle</span><strong>{plan.lifecycle.name}</strong><small>Fixed source-owned adapter · {shortDigest(plan.lifecycle.sha256)}</small></div>
    </div>
    {confirming && <div className="setup-confirm" role="alert"><strong>Apply this fixed setup now?</strong><small>Apply consumes the one-shot session-bound token and starts the managed runtime once after verification.</small></div>}
    <div className="setup-flow-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel</button>{confirming ? <button type="button" onClick={onApply} disabled={busy}>{busy ? 'Applying…' : 'Confirm setup'}</button> : <button type="button" onClick={onConfirm} disabled={busy}>Apply setup</button>}</div>
  </div>
}
