import { StrictMode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'
import { ComponentLifecycleNotices, ComponentsUpdatesSection, useComponentsController } from './components-updates.jsx'
import { DNSLifecycleNotice, DNSObservatorySection, useDNSObservatoryController } from './dns-observatory.jsx'
import { RoutingLifecycleNotice, RoutingPolicySection, useRoutingController } from './routing-policy.jsx'
import { SetupFlow } from './setup-flow.jsx'

import flagAE from 'flag-icons/flags/4x3/ae.svg'
import flagAM from 'flag-icons/flags/4x3/am.svg'
import flagAT from 'flag-icons/flags/4x3/at.svg'
import flagBG from 'flag-icons/flags/4x3/bg.svg'
import flagCA from 'flag-icons/flags/4x3/ca.svg'
import flagCZ from 'flag-icons/flags/4x3/cz.svg'
import flagDE from 'flag-icons/flags/4x3/de.svg'
import flagEE from 'flag-icons/flags/4x3/ee.svg'
import flagES from 'flag-icons/flags/4x3/es.svg'
import flagFI from 'flag-icons/flags/4x3/fi.svg'
import flagFR from 'flag-icons/flags/4x3/fr.svg'
import flagGB from 'flag-icons/flags/4x3/gb.svg'
import flagIL from 'flag-icons/flags/4x3/il.svg'
import flagIN from 'flag-icons/flags/4x3/in.svg'
import flagKZ from 'flag-icons/flags/4x3/kz.svg'
import flagLV from 'flag-icons/flags/4x3/lv.svg'
import flagNL from 'flag-icons/flags/4x3/nl.svg'
import flagPL from 'flag-icons/flags/4x3/pl.svg'
import flagSE from 'flag-icons/flags/4x3/se.svg'
import flagSG from 'flag-icons/flags/4x3/sg.svg'
import flagTH from 'flag-icons/flags/4x3/th.svg'
import flagTR from 'flag-icons/flags/4x3/tr.svg'
import flagUS from 'flag-icons/flags/4x3/us.svg'
import flagUZ from 'flag-icons/flags/4x3/uz.svg'

const PAGE_SIZE = 25
const MAX_RESTORE_BUNDLE_BYTES = 9 * 1024 * 1024
const MIN_BACKUP_PASSPHRASE_BYTES = 12
const MAX_BACKUP_PASSPHRASE_BYTES = 256
const FLAG_PREFIX = /^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u
const COUNTRY_FLAGS = {
  AE: flagAE, AM: flagAM, AT: flagAT, BG: flagBG, CA: flagCA, CZ: flagCZ,
  DE: flagDE, EE: flagEE, ES: flagES, FI: flagFI, FR: flagFR, GB: flagGB,
  IL: flagIL, IN: flagIN, KZ: flagKZ, LV: flagLV, NL: flagNL, PL: flagPL,
  SE: flagSE, SG: flagSG, TH: flagTH, TR: flagTR, US: flagUS, UZ: flagUZ,
}

const api = async (path, options = {}) => {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(options.headers || {}) },
    ...options,
  })
  const body = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(body.error || `Request failed (${response.status})`)
    error.status = response.status
    error.code = body.error
    throw error
  }
  return body
}

const download = async (path, options = {}, filename) => {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: { Accept: '*/*', ...(options.headers || {}) },
    ...options,
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    const error = new Error(body.error || `Download failed (${response.status})`)
    error.status = response.status
    error.code = body.error
    throw error
  }
  const blob = await response.blob()
  const objectURL = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = objectURL
  anchor.download = filename
  anchor.click()
  window.setTimeout(() => URL.revokeObjectURL(objectURL), 0)
}

const formatTime = (value) => {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isFinite(date.getTime()) && date.getTime() > 0 ? date.toLocaleString() : '—'
}

const visibleNodeName = (node) => String(node?.displayName || node?.name || '').replace(FLAG_PREFIX, '').trim() || 'Unnamed node'
const nodeSource = (node) => node.subscriptionName || (node.sourceType === 'subscription' ? 'Unnamed subscription' : node.sourceType) || 'legacy'
const nodeRole = (node) => node.isEffective ? 'effective' : node.isOverride ? 'override' : node.isNativeSelected ? 'native' : 'none'
const matchesNodeRole = (node, role) => role === 'all'
  || (role === 'native' && node.isNativeSelected)
  || (role === 'override' && node.isOverride)
  || (role === 'effective' && node.isEffective)
  || (role === 'none' && !node.isNativeSelected && !node.isOverride && !node.isEffective)
const healthRank = (node) => node.alive ? 0 : node.enabled ? 1 : 2
const roleRank = (node) => ({ effective: 0, override: 1, native: 2, none: 3 })[nodeRole(node)]
const autoRefreshStateLabels = {
  waiting: 'Refresh scheduled',
  running: 'Refreshing saved snapshot…',
  deferred: 'Refresh deferred',
  failed: 'Refresh failed',
  disabled: 'Automatic refresh disabled',
}
const autoRefreshErrorLabels = {
  'runtime-busy': 'Runtime busy',
  'authority-busy': 'Authority busy',
  'operator-preview': 'Operator preview active',
  stale: 'Registry changed; will retry',
  'fetch-failed': 'Provider fetch failed',
  'content-rejected': 'Provider content rejected',
  duplicate: 'Duplicate provider node',
  'node-rejected': 'Provider node rejected',
  'candidate-invalid': 'Candidate rejected',
  'activation-failed': 'Activation failed',
  'registry-unavailable': 'Registry unavailable',
}
const autoRefreshResultLabels = { updated: 'Updated', noop: 'No change' }
const autoRefreshState = (status) => autoRefreshStateLabels[status?.state] ? status.state : 'waiting'
const autoRefreshSummary = (status) => {
  if (!status) return ''
  const state = autoRefreshState(status)
  if (state === 'running') return 'Fetching the saved provider snapshot'
  if (state === 'disabled') return 'Disabled subscriptions do not participate'
  const parts = []
  if (status.errorCode && autoRefreshErrorLabels[status.errorCode]) parts.push(autoRefreshErrorLabels[status.errorCode])
  if (status.lastResult && autoRefreshResultLabels[status.lastResult]) parts.push(`Last: ${autoRefreshResultLabels[status.lastResult]}`)
  if (status.lastSuccessAt) parts.push(`Success: ${formatTime(status.lastSuccessAt)}`)
  if (status.nextRunAt) parts.push(`Next: ${formatTime(status.nextRunAt)}`)
  return parts.join(' · ') || 'No attempt yet'
}

const manualStateLabels = {
  idle: 'Ready',
  running: 'Running',
  completed: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
  'cleanup-pending': 'Cleanup pending',
}
const manualPhaseLabels = {
  latency: 'Latency',
  download: 'Download',
  upload: 'Upload',
  cleanup: 'Cleanup',
  done: 'Done',
}
const manualErrorLabels = {
  'invalid-target': 'The selected node is no longer enabled or available.',
  'runtime-busy': 'The runtime is busy with another operation.',
  'probe-unavailable': 'The diagnostic probe is unavailable.',
  'probe-cleanup': 'Probe cleanup is pending; another diagnostic is blocked.',
  'latency-failed': 'Fewer than two latency samples completed.',
  'download-failed': 'No complete download stage completed.',
  'upload-failed': 'No complete upload stage completed.',
  timeout: 'The diagnostic reached its time limit.',
  cancelled: 'The diagnostic was cancelled by a lifecycle operation.',
  'transport-failure': 'The fixed measurement transport failed.',
}
const manualStatusLabel = (state) => manualStateLabels[state] || 'Unavailable'
const manualPhaseLabel = (phase) => manualPhaseLabels[phase] || 'Unavailable'
const formatManualBytes = (value) => {
  const numeric = Number(value)
  return value == null || !Number.isFinite(numeric) || numeric < 0 ? '—' : `${(numeric / (1024 * 1024)).toFixed(2)} MiB`
}
const formatManualRate = (value) => {
  const numeric = Number(value)
  return value == null || !Number.isFinite(numeric) || numeric < 0 ? '—' : `${((numeric * 8) / 1000000).toFixed(1)} Mbps`
}

const SAFE_CANONICAL_TAG = /^proxy-[A-Za-z0-9._-]{1,122}$/
const safeCanonicalTag = (value) => {
  const tag = String(value || '')
  return SAFE_CANONICAL_TAG.test(tag) ? tag : ''
}
const safeCount = (value, maximum = Number.MAX_SAFE_INTEGER) => {
  const numeric = Number(value)
  return Number.isFinite(numeric) && numeric >= 0 ? Math.min(Math.floor(numeric), maximum) : 0
}
const formatAdaptiveLatency = (value) => {
  const numeric = Number(value)
  return value == null || !Number.isFinite(numeric) || numeric <= 0 ? '—' : `${Math.round(numeric)} ms`
}
const formatAdaptiveRate = (value) => {
  const numeric = Number(value)
  return value == null || !Number.isFinite(numeric) || numeric <= 0 ? '—' : `${((numeric * 8) / 1000000).toFixed(1)} Mbps`
}
const formatAdaptiveScore = (value, valid) => {
  const numeric = Number(value)
  if (!valid || value == null || !Number.isFinite(numeric) || numeric < 0) return '—'
  return `${(Math.min(1, Math.max(0, numeric)) * 100).toFixed(1)}%`
}

const adaptiveStateLabels = {
  waiting: 'Waiting for next check',
  running: 'Measuring adaptive quality',
  skipped: 'Skipped',
  completed: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
  'cleanup-pending': 'Cleanup pending',
}
const adaptiveReasonLabels = {
  'manual-override': 'Manual override is active',
  busy: 'The runtime was busy',
  unavailable: 'Adaptive quality is unavailable',
  'no-current-target': 'No current target was available',
  'current-ineligible': 'The current target was not eligible',
  'insufficient-candidates': 'There were not enough candidates',
  'generation-budget': 'The adaptive budget was exhausted',
  cancelled: 'The generation was cancelled',
  'cleanup-pending': 'Probe cleanup is pending',
  'stale-generation': 'The generation became stale',
  'current-invalid': 'The current target did not produce valid evidence',
  'no-challenger': 'No eligible challenger beat the current target',
  'minimum-dwell': 'The current target minimum dwell has not elapsed',
  hysteresis: 'The quality margin was not large enough to switch',
  'no-switch': 'No target switch was needed',
  'adaptive-quality': 'Adaptive quality applied a target switch',
}
const selectionStateLabels = {
  stable: 'Automatic stable selection',
  manual: 'Explicit manual override',
  'manual-fallback': 'Manual override with native fallback',
  'fallback-leastping': 'Native fallback selection',
  starting: 'Selection starting',
  unavailable: 'Selection unavailable',
}
const selectionReasonLabels = {
  startup: 'Startup selection',
  'health-failover': 'Health failover',
  'latency-quality': 'Latency evidence',
  'throughput-benchmark': 'Legacy compatibility diagnostic',
  'adaptive-quality': 'Adaptive quality applied a target switch',
  'fallback-leastping': 'Native fallback',
  'reapply-after-restart': 'Runtime re-apply',
  'manual-override': 'Manual override',
  'manual-unavailable': 'Manual target unavailable',
  'manual-cleared': 'Manual override cleared',
}
const adaptiveState = (status) => adaptiveStateLabels[status?.state] ? status.state : 'unavailable'
const adaptiveStateLabel = (status) => adaptiveStateLabels[adaptiveState(status)] || 'Adaptive state unavailable'
const adaptiveReasonLabel = (reason) => adaptiveReasonLabels[reason] || 'Adaptive result unavailable'
const selectionStateLabel = (state) => selectionStateLabels[state] || 'Selection state unavailable'
const selectionReasonLabel = (reason) => selectionReasonLabels[reason] || 'Selection reason unavailable'
const safeAdaptiveCandidates = (status) => (Array.isArray(status?.candidates) ? status.candidates : [])
  .map((candidate) => ({ ...candidate, tag: safeCanonicalTag(candidate?.tag) }))
  .filter((candidate) => candidate.tag)
  .slice(0, 6)
const hasAdaptiveGeneration = (status, candidates) => candidates.length > 0
  || safeCount(status?.shortlistCount, 6) > 0
  || safeCount(status?.validCount, 6) > 0

const sortNodes = (nodes, key, direction) => {
  const multiplier = direction === 'desc' ? -1 : 1
  const stringValue = (value) => String(value || '').toLocaleLowerCase()
  const valueFor = (node) => {
    switch (key) {
      case 'address': return stringValue(node.address)
      case 'health': return healthRank(node)
      case 'latency': return node.alive && node.latencyMs ? node.latencyMs : Number.MAX_SAFE_INTEGER
      case 'role': return roleRank(node)
      case 'source': return stringValue(nodeSource(node))
      case 'country': return stringValue(node.countryCode)
      default: return stringValue(visibleNodeName(node))
    }
  }
  return [...nodes].sort((left, right) => {
    const leftValue = valueFor(left)
    const rightValue = valueFor(right)
    let compared = 0
    if (typeof leftValue === 'number' && typeof rightValue === 'number') compared = leftValue - rightValue
    else compared = String(leftValue).localeCompare(String(rightValue), undefined, { numeric: true, sensitivity: 'base' })
    if (compared === 0) compared = visibleNodeName(left).localeCompare(visibleNodeName(right), undefined, { numeric: true, sensitivity: 'base' })
    return compared * multiplier
  })
}

const createNodeViewState = () => ({
  query: '',
  statusFilter: 'all',
  roleFilter: 'all',
  sourceFilter: 'all',
  countryFilter: 'all',
  sort: { key: 'name', direction: 'asc' },
  page: 1,
})

function App() {
  const [session, setSession] = useState(null)
  const [password, setPassword] = useState('')
  const [dashboard, setDashboard] = useState(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  const loadDashboard = useCallback(async () => {
    try {
      const [status, nodes, performance, config, update] = await Promise.all([
        api('/api/v1/status'),
        api('/api/v1/nodes'),
        api('/api/v1/performance'),
        api('/api/v1/config-summary'),
        api('/api/v1/update'),
      ])
      setDashboard({ status, nodes, performance, config, update })
      setError('')
    } catch (cause) {
      if (cause.status === 401) {
        setDashboard(null)
        setSession(null)
      }
      setError(cause.message)
    } finally {
      setLoading(false)
    }
  }, [])

  const loadPerformance = useCallback(async () => {
    try {
      const performance = await api('/api/v1/performance')
      setDashboard((current) => current ? { ...current, performance } : current)
      setError('')
    } catch (cause) {
      if (cause.status === 401) {
        setDashboard(null)
        setSession(null)
      }
      setError(cause.message)
    }
  }, [])

  useEffect(() => {
    api('/api/v1/session')
      .then((value) => {
        setSession(value)
        return loadDashboard()
      })
      .catch((cause) => {
        if (cause.status !== 401) setError(cause.message)
        setLoading(false)
      })
  }, [loadDashboard])

  useEffect(() => {
    if (!session) return undefined
    const timer = window.setInterval(loadDashboard, 5000)
    return () => window.clearInterval(timer)
  }, [session, loadDashboard])

  const login = async (event) => {
    event.preventDefault()
    setError('')
    try {
      const value = await api('/api/v1/session/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      })
      setPassword('')
      setSession(value)
      await loadDashboard()
    } catch (cause) {
      setError(cause.message)
    }
  }

  const logout = async () => {
    try {
      await api('/api/v1/session/logout', {
        method: 'POST',
        headers: { 'X-CSRF-Token': session?.csrfToken || '' },
      })
    } catch (cause) {
      setError(cause.message)
    }
    setDashboard(null)
    setSession(null)
  }

  const invalidateSession = useCallback(() => {
    setDashboard(null)
    setSession(null)
  }, [])

  const checkUpdate = async () => {
    try {
      await api('/api/v1/update/check', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': session?.csrfToken || '' },
        body: JSON.stringify({ channel: dashboard?.update?.policy?.channel || 'stable' }),
      })
      await loadDashboard()
    } catch (cause) {
      setError(cause.message)
    }
  }

  if (!session) return <Login error={error} password={password} setPassword={setPassword} onSubmit={login} />
  if (loading && !dashboard) return <Shell><div className="loading">Reading current router state…</div></Shell>
  if (!dashboard) return <Shell><Notice message={error || 'Runtime state is unavailable.'} /></Shell>

  return <Dashboard dashboard={dashboard} session={session} error={error} onRefresh={loadDashboard} onPerformanceRefresh={loadPerformance} onLogout={logout} onCheckUpdate={checkUpdate} onUnauthorized={invalidateSession} />
}

function Login({ error, password, setPassword, onSubmit }) {
  return <main className="login-page">
    <section className="login-card">
      <h1>XKeen Control</h1>
      <form onSubmit={onSubmit}>
        <label htmlFor="password">Panel password</label>
        <input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} autoFocus />
        <button type="submit">Sign in</button>
      </form>
      {error && <Notice message={error} />}
    </section>
  </main>
}

function Dashboard({ dashboard, session, error, onRefresh, onPerformanceRefresh, onLogout, onCheckUpdate, onUnauthorized }) {
  const { status, nodes, performance, config, update } = dashboard
  const [section, setSection] = useState('overview')
  const [nodeView, setNodeView] = useState(createNodeViewState)
  const [restoreState, setRestoreState] = useState({ preview: null })
  const registryNodes = nodes.nodes || []
  const nodesByTag = useMemo(() => new Map(registryNodes.map((node) => [node.outboundTag || node.tag, node])), [registryNodes])
  const componentController = useComponentsController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized })
  const routingControllerRef = useRef(null)
  const dnsControllerRef = useRef(null)
  const invalidateRoutingPreview = useCallback(() => routingControllerRef.current?.invalidateLivePreview(), [])
  const invalidateDNSPreview = useCallback(() => dnsControllerRef.current?.invalidateLivePreview(), [])
  const refreshRoutingPeer = useCallback(() => { void routingControllerRef.current?.refreshPeerProjection() }, [])
  const refreshDNSPeer = useCallback(() => { void dnsControllerRef.current?.refreshPeerProjection() }, [])
  const routingController = useRoutingController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized, active: section === 'routing', onBeforeApply: invalidateDNSPreview, onApplied: refreshDNSPeer })
  const dnsController = useDNSObservatoryController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized, active: section === 'dns', onBeforeApply: invalidateRoutingPreview, onApplied: refreshRoutingPeer })
  routingControllerRef.current = routingController
  dnsControllerRef.current = dnsController
  const openComponents = useCallback(() => {
    setSection('components')
    void componentController.loadInventory()
  }, [componentController.loadInventory])
  const openRouting = useCallback(() => setSection('routing'), [])
  const openDNS = useCallback(() => setSection('dns'), [])
  const lifecycleBlocked = componentController.lifecycleMutationBlocked
  const manualLifecycleBlocked = !status.lifecycle || status.lifecycle.maintenance || status.lifecycle.applying
  const manualRunning = performance?.manual?.state === 'running'
  const adaptiveRunning = performance?.adaptive?.state === 'running'
  const performancePolling = (section === 'overview' && adaptiveRunning)
    || (section === 'nodes' && (manualRunning || adaptiveRunning))
  const performancePollInFlight = useRef(false)

  useEffect(() => {
    if (!performancePolling || !onPerformanceRefresh) return undefined
    let active = true
    const poll = async () => {
      if (!active || performancePollInFlight.current) return
      performancePollInFlight.current = true
      try {
        await onPerformanceRefresh()
      } finally {
        performancePollInFlight.current = false
      }
    }
    const timer = window.setInterval(() => { void poll() }, 1000)
    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [performancePolling, onPerformanceRefresh])

  return <Shell>
    <header className="topbar">
      <strong className="product-name">XKeen Control</strong>
      <div className="top-actions">
        <span className="chip neutral">{status.controlPlane?.version || 'dev'}</span>
        <button className="ghost" type="button" onClick={onLogout}>Sign out</button>
      </div>
    </header>
    <nav className="section-nav" aria-label="Dashboard sections">
      <button type="button" className={section === 'overview' ? 'active' : ''} onClick={() => setSection('overview')}>Overview</button>
      <button type="button" className={section === 'nodes' ? 'active' : ''} onClick={() => setSection('nodes')}>Nodes <span>{nodes.total || 0}</span></button>
      <button type="button" className={section === 'routing' ? 'active' : ''} onClick={openRouting}>Routing</button>
      <button type="button" className={section === 'dns' ? 'active' : ''} onClick={openDNS}>DNS</button>
      <button type="button" className={section === 'components' ? 'active' : ''} onClick={openComponents}>Components / Updates</button>
      <button type="button" className={section === 'system' ? 'active' : ''} onClick={() => setSection('system')}>System</button>
      <button type="button" className={section === 'backup' ? 'active' : ''} onClick={() => setSection('backup')}>Backup &amp; Restore</button>
    </nav>
    <ComponentLifecycleNotices controller={componentController} lifecycle={status.lifecycle} onOpenComponents={openComponents} />
    <RoutingLifecycleNotice controller={routingController} active={section === 'routing'} onOpenRouting={openRouting} />
    <DNSLifecycleNotice controller={dnsController} active={section === 'dns'} onOpenDNS={openDNS} />
    {error && <Notice message={error} />}
    {section === 'overview' && <Overview status={status} performance={performance} nodeTotal={nodes.total || 0} nodesByTag={nodesByTag} csrfToken={session.csrfToken} onRefresh={onRefresh} onUnauthorized={onUnauthorized} />}
    {section === 'nodes' && <NodeWorkspace nodes={registryNodes} subscriptions={nodes.subscriptions || []} performance={performance} manualOverride={status.selection?.manualOverride || ''} benchmarkRunning={Boolean(status.benchmark?.controlPlane?.running)} csrf={session.csrfToken} onRefresh={onRefresh} onPerformanceRefresh={onPerformanceRefresh} viewState={nodeView} onViewStateChange={setNodeView} lifecycleBlocked={lifecycleBlocked} manualLifecycleBlocked={manualLifecycleBlocked} />}
    {section === 'routing' && <RoutingPolicySection controller={routingController} lifecycle={status.lifecycle} />}
    {section === 'dns' && <DNSObservatorySection controller={dnsController} />}
    {section === 'components' && <ComponentsUpdatesSection controller={componentController} lifecycle={status.lifecycle} onOpenSystem={() => setSection('system')} />}
    {section === 'system' && <SystemSection status={status} config={config} nodesByTag={nodesByTag} update={update} onCheckUpdate={onCheckUpdate} />}
    {section === 'backup' && <BackupRestoreSection csrf={session.csrfToken} restoreState={restoreState} setRestoreState={setRestoreState} onRefresh={onRefresh} onUnauthorized={onUnauthorized} lifecycleBlocked={lifecycleBlocked} />}
  </Shell>
}

function Overview({ status, performance, nodeTotal, nodesByTag, csrfToken, onRefresh, onUnauthorized }) {
  const healthy = status.observatory?.healthy || 0
  const total = status.observatory?.total || nodeTotal
  const healthText = total ? `${healthy}/${total} healthy` : 'No node data'
  return <div className="section-stack">
    <section className="panel setup-banner"><div><span className="panel-label">Panel readiness</span><strong>{status.setup?.runtime || 'setup'} · credential {status.setup?.credential || 'unknown'}</strong><small>XKeen {status.setup?.xkeen || 'missing'} · Xray {status.setup?.xray || 'missing'} · configuration {status.setup?.configuration || 'missing'}</small></div></section>
    <SetupFlow setup={status.setup} csrfToken={csrfToken} onRefresh={onRefresh} onUnauthorized={onUnauthorized} />
    <section className="hero-grid">
      <HealthCard label="Xray" ok={status.xray?.running && status.xray?.apiReachable} detail={status.xray?.apiReachable ? 'API reachable' : 'Degraded'} />
      <HealthCard label="Probe" ok={status.xray?.probeReachable} detail={status.xray?.probeReachable ? '127.0.0.1:10808' : 'Unavailable'} />
      <HealthCard label="Observatory" ok={status.observatory?.apiReachable} detail={healthText} />
      <HealthCard label="XKeen" ok={status.xkeen?.running} detail={status.xkeen?.running ? 'Running' : 'Not detected'} />
    </section>
    <section className="selection-grid">
      <SelectionCard label="Native leastPing" node={nodesByTag.get(status.balancer?.nativeSelected)} tone="blue" />
      <SelectionCard label="Manual override" node={nodesByTag.get(status.selection?.manualOverride)} tone="amber" emptyText="Automatic selection" />
      <SelectionCard label="Effective" node={nodesByTag.get(status.balancer?.effective)} tone="green" />
    </section>
    <AutomaticQualityOverview status={status} performance={performance} nodesByTag={nodesByTag} />
  </div>
}

function targetPresentation(tag, nodesByTag) {
  const safeTag = safeCanonicalTag(tag)
  const node = safeTag ? nodesByTag.get(safeTag) : null
  return { tag: safeTag, node, label: node ? visibleNodeName(node) : safeTag || 'Unavailable' }
}

function adaptiveOutcomeLabel(status, nodesByTag) {
  const state = adaptiveState(status)
  if (state === 'waiting') return 'No adaptive generation has completed yet.'
  if (state === 'running') return 'The current shortlist is being measured.'
  if (status?.switchApplied === true) {
    const target = targetPresentation(status.selectedTarget, nodesByTag)
    return target.tag ? `Actual switch applied to ${target.label}.` : 'Actual switch applied; the safe target identity is unavailable.'
  }
  const reason = adaptiveReasonLabel(status?.reasonCode)
  if (state === 'skipped') return `Adaptive generation skipped: ${reason}.`
  if (state === 'completed') return `No target switch: ${reason}.`
  return `${adaptiveStateLabel(status)}: ${reason}.`
}

function AdaptiveGenerationFacts({ status, candidates }) {
  const nextRunAt = formatTime(status?.nextRunAt)
  const completedAt = formatTime(status?.completedAt)
  const hasGeneration = hasAdaptiveGeneration(status, candidates)
  return <>
    {hasGeneration && <div><span>Generation evidence</span><strong>{safeCount(status?.shortlistCount, 6)} shortlisted · {safeCount(status?.validCount, 6)} valid</strong></div>}
    {nextRunAt !== '—' && <div><span>Next adaptive check</span><strong>{nextRunAt}</strong></div>}
    {completedAt !== '—' && <div><span>Last completion</span><strong>{completedAt}</strong></div>}
  </>
}

function AutomaticQualityOverview({ status, performance, nodesByTag }) {
  const selection = status.selection || {}
  const adaptive = performance?.adaptive || { state: 'waiting' }
  const candidates = safeAdaptiveCandidates(adaptive)
  const effective = targetPresentation(selection.effectiveTarget || status.balancer?.effective, nodesByTag)
  const manual = targetPresentation(selection.manualOverride, nodesByTag)
  const selectionState = selectionStateLabel(selection.state || 'starting')
  const selectionReason = selection.lastSwitchReason ? selectionReasonLabel(selection.lastSwitchReason) : 'No selection change recorded'
  const switchedTarget = targetPresentation(adaptive.selectedTarget, nodesByTag)
  return <section className={`panel automatic-quality-overview adaptive-${adaptiveState(adaptive)}`} data-testid="automatic-quality-overview">
    <div className="automatic-quality-heading"><div><span className="panel-label">Automatic quality</span><h2>{selectionState}</h2><p>{selectionReason}</p></div><span className="chip neutral">{adaptiveStateLabel(adaptive)}</span></div>
    <div className="automatic-quality-grid">
      <div><span>Effective target</span><strong>{effective.label}</strong>{effective.tag && <code>{effective.tag}</code>}</div>
      <div><span>Manual override</span><strong>{manual.tag ? manual.label : 'Not active'}</strong>{manual.tag && <code>{manual.tag}</code>}</div>
      <div><span>Adaptive state</span><strong>{adaptiveStateLabel(adaptive)}</strong><small>{adaptiveOutcomeLabel(adaptive, nodesByTag)}</small></div>
      <AdaptiveGenerationFacts status={adaptive} candidates={candidates} />
      {adaptive.switchApplied === true && <div><span>Actual switched target</span><strong>{switchedTarget.label}</strong>{switchedTarget.tag && <code>{switchedTarget.tag}</code>}</div>}
    </div>
    {manual.tag && <p className="automatic-quality-note">Automatic quality is paused by the explicit manual override.</p>}
  </section>
}

function NodeWorkspace({ nodes, subscriptions, performance, manualOverride, benchmarkRunning, csrf, onRefresh, onPerformanceRefresh, viewState, onViewStateChange, lifecycleBlocked, manualLifecycleBlocked }) {
  const [profiles, setProfiles] = useState('')
  const [subscriptionUrl, setSubscriptionUrl] = useState('')
  const [subscriptionName, setSubscriptionName] = useState('')
  const [subscriptionID, setSubscriptionID] = useState('')
  const [replacement, setReplacement] = useState('')
  const [editingID, setEditingID] = useState('')
  const [selectedIDs, setSelectedIDs] = useState(() => new Set())
  const [composer, setComposer] = useState('')
  const [preview, setPreview] = useState(null)
  const [notice, setNotice] = useState(null)
  const [busy, setBusy] = useState(false)
  const [manualRequestBusy, setManualRequestBusy] = useState(false)
  const manualStatus = performance?.manual || { state: 'idle', phase: 'done', plannedStages: 11, bytesPlanned: 48 * 1024 * 1024, completedStages: 0, bytesTransferred: 0 }
  const manualRunning = manualStatus.state === 'running'
  const adaptiveStatus = performance?.adaptive || { state: 'waiting' }
  const adaptiveRunning = adaptiveStatus.state === 'running'
  const { query, statusFilter, roleFilter, sourceFilter, countryFilter, sort, page } = viewState

  const filtered = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    return nodes.filter((node) => {
      if (needle && ![visibleNodeName(node), node.name, node.address, node.subscriptionName, node.sourceType, node.countryCode].some((value) => String(value || '').toLocaleLowerCase().includes(needle))) return false
      if (statusFilter === 'alive' && !node.alive) return false
      if (statusFilter === 'unhealthy' && (!node.enabled || node.alive)) return false
      if (statusFilter === 'disabled' && node.enabled) return false
      if (statusFilter === 'stale' && !node.stale && !node.missing) return false
      if (!matchesNodeRole(node, roleFilter)) return false
      if (sourceFilter !== 'all' && node.sourceType !== sourceFilter) return false
      if (countryFilter !== 'all' && node.countryCode !== countryFilter) return false
      return true
    })
  }, [nodes, query, statusFilter, roleFilter, sourceFilter, countryFilter])
  const ordered = useMemo(() => sortNodes(filtered, sort.key, sort.direction), [filtered, sort])
  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const visibleNodes = ordered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
  const selectedNodes = useMemo(() => nodes.filter((node) => node.id && selectedIDs.has(node.id)), [nodes, selectedIDs])
  const selectedNodeIDs = useMemo(() => selectedNodes.map((node) => node.id), [selectedNodes])
  const selectedFilteredCount = useMemo(() => filtered.reduce((count, node) => count + (node.id && selectedIDs.has(node.id) ? 1 : 0), 0), [filtered, selectedIDs])
  const allFilteredSelected = filtered.length > 0 && selectedFilteredCount === filtered.length
  const selectedNode = selectedNodes.length === 1 ? selectedNodes[0] : null
  const selectedManual = selectedNode && manualOverride === (selectedNode.outboundTag || selectedNode.tag)
  const countryOptions = useMemo(() => [...new Set(nodes.map((node) => node.countryCode).filter(Boolean))].sort(), [nodes])
  const sourceOptions = useMemo(() => [...new Set(nodes.map((node) => node.sourceType).filter(Boolean))].sort(), [nodes])
  const statusCounts = useMemo(() => ({
    all: nodes.length,
    alive: nodes.filter((node) => node.alive).length,
    unhealthy: nodes.filter((node) => node.enabled && !node.alive).length,
    disabled: nodes.filter((node) => !node.enabled).length,
    stale: nodes.filter((node) => node.stale || node.missing).length,
  }), [nodes])
  const filtersActive = Boolean(query.trim()) || statusFilter !== 'all' || roleFilter !== 'all' || sourceFilter !== 'all' || countryFilter !== 'all'

  useEffect(() => {
    if (page > totalPages) onViewStateChange((current) => current.page > totalPages ? { ...current, page: totalPages } : current)
  }, [page, totalPages, onViewStateChange])

  useEffect(() => {
    const allowed = new Set(filtered.map((node) => node.id).filter(Boolean))
    setSelectedIDs((current) => {
      let changed = false
      const next = new Set()
      for (const id of current) {
        if (allowed.has(id)) next.add(id)
        else changed = true
      }
      return changed ? next : current
    })
  }, [filtered])

  useEffect(() => {
    if (editingID && (!selectedNode || selectedNode.id !== editingID)) {
      setEditingID('')
      setReplacement('')
    }
  }, [editingID, selectedNode])

  const chooseFilter = (key, value) => {
    onViewStateChange((current) => ({ ...current, [key]: value, page: 1 }))
  }

  const clearFilters = () => {
    onViewStateChange((current) => ({ ...current, ...createNodeViewState(), sort: current.sort, page: 1 }))
  }

  const closeComposer = () => {
    setComposer('')
    setProfiles('')
    setSubscriptionID('')
    setSubscriptionName('')
    setSubscriptionUrl('')
  }

  const startNewSubscription = () => {
    setSubscriptionID('')
    setSubscriptionName('')
    setSubscriptionUrl('')
    setComposer('subscription')
  }

  const openSubscriptionEditor = (subscription) => {
    setSubscriptionID(subscription.id)
    setSubscriptionName(subscription.name || '')
    setSubscriptionUrl('')
    setComposer('subscription')
  }

  const changeSort = (key) => {
    onViewStateChange((current) => ({
      ...current,
      sort: current.sort.key === key ? { key, direction: current.sort.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: 'asc' },
      page: 1,
    }))
  }

  const toggleSelection = (id) => {
    if (!id) return
    setSelectedIDs((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleAllFiltered = () => {
    const filteredIDs = filtered.map((node) => node.id).filter(Boolean)
    setSelectedIDs((current) => {
      const next = new Set(current)
      const allSelected = filteredIDs.length > 0 && filteredIDs.every((id) => next.has(id))
      for (const id of filteredIDs) {
        if (allSelected) next.delete(id)
        else next.add(id)
      }
      return next
    })
  }

  const clearSelection = () => setSelectedIDs(new Set())

  const requestPreview = async (path, payload, effectiveImpact = '') => {
    if (lifecycleBlocked) return
    setBusy(true)
    setNotice(null)
    try {
      const value = await api(path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify(payload) })
      setPreview({ ...value, effectiveImpact })
    } catch (cause) {
      setNotice({ tone: 'error', message: cause.message })
    } finally {
      setBusy(false)
    }
  }

  const applyPreview = async () => {
    if (!preview || lifecycleBlocked) return
    setBusy(true)
    setNotice(null)
    try {
      await api('/api/v1/node-changes/apply', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify({ previewToken: preview.previewToken, acceptMissing: preview.operation === 'subscription-refresh' ? false : Boolean(preview.requiresAcceptance) }) })
      setPreview(null)
      setReplacement('')
      setEditingID('')
      closeComposer()
      await onRefresh()
      setNotice({ tone: 'success', message: 'Change applied; the full Xray candidate and active inventory were validated.' })
    } catch (cause) {
      setNotice({ tone: 'error', message: cause.message })
    } finally {
      setBusy(false)
    }
  }

  const cancelPreview = async () => {
    const token = preview?.previewToken
    setPreview(null)
    if (!token) return
    try {
      await api('/api/v1/node-changes/cancel', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify({ previewToken: token }) })
    } catch (cause) {
      setNotice({ tone: 'error', message: cause.message })
    }
  }

  const openEditor = () => {
    if (!selectedNode) return
    setEditingID(editingID === selectedNode.id ? '' : selectedNode.id)
    setReplacement('')
  }

  const setManualOverride = async (target) => {
    if (lifecycleBlocked) return
    setBusy(true)
    setNotice(null)
    try {
      await api('/api/v1/selection/override', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify({ target }) })
      await onRefresh()
      setNotice({ tone: 'success', message: target ? 'Manual override set. Automatic quality selection is paused for this node.' : 'Manual override cleared. Automatic selection is active again.' })
    } catch (cause) {
      setNotice({ tone: 'error', message: cause.message })
    } finally {
      setBusy(false)
    }
  }

  const runManualNode = async () => {
    if (!selectedNode || selectedNodes.length !== 1 || !selectedNode.enabled || manualLifecycleBlocked || benchmarkRunning || manualRunning || adaptiveRunning || manualRequestBusy) return
    setManualRequestBusy(true)
    setNotice(null)
    try {
      await api('/api/v1/performance/manual-node', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ nodeId: selectedNode.id }),
      })
      if (onPerformanceRefresh) await onPerformanceRefresh()
    } catch (cause) {
      setNotice({ tone: 'error', message: cause.message })
    } finally {
      setManualRequestBusy(false)
    }
  }

  return <section className="panel nodes-workspace">
    <div className="workspace-heading">
      <h2>Nodes <span className="count">{nodes.length}</span></h2>
      <div className="workspace-actions">
        <IconButton icon="plus" label="Add VLESS profiles" disabled={lifecycleBlocked} onClick={() => setComposer(composer === 'profiles' ? '' : 'profiles')} />
        <IconButton icon="link" label="Add subscription" disabled={lifecycleBlocked} onClick={() => composer === 'subscription' ? closeComposer() : startNewSubscription()} />
        <IconButton icon="refresh" label="Refresh dashboard" onClick={onRefresh} />
      </div>
    </div>

    {notice && <Notice message={notice.message} tone={notice.tone} />}

    {composer === 'profiles' && <form className="composer" onSubmit={(event) => { event.preventDefault(); requestPreview('/api/v1/nodes/import/preview', { profiles }) }}>
      <div><span className="panel-label">Add profiles</span><h3>Import VLESS + REALITY links</h3><p>Names are read from link fragments. Keys stay inside the authenticated request and RAM-only preview.</p></div>
      <textarea value={profiles} onChange={(event) => setProfiles(event.target.value)} placeholder="Paste one or more vless:// links" autoFocus />
      <div className="composer-actions"><button className="ghost" type="button" onClick={closeComposer}>Cancel</button><button type="submit" disabled={busy || lifecycleBlocked || !profiles.trim()}>Preview add</button></div>
    </form>}

    {composer === 'subscription' && <form className="composer compact" onSubmit={(event) => { event.preventDefault(); requestPreview('/api/v1/subscriptions/refresh/preview', { ...(subscriptionID ? { subscriptionId: subscriptionID } : {}), name: subscriptionName, url: subscriptionUrl }) }}>
      <div><span className="panel-label">{subscriptionID ? 'Update subscription' : 'New subscription'}</span><h3>Fetch explicitly</h3><p>HTTPS only, with DNS/IP SSRF checks and bounded response parsing.</p></div>
      <label htmlFor="subscription-name">Display name</label>
      <input id="subscription-name" value={subscriptionName} onChange={(event) => setSubscriptionName(event.target.value)} placeholder="Home provider" />
      <label htmlFor="subscription-url">Subscription URL</label>
      <input id="subscription-url" type="password" value={subscriptionUrl} onChange={(event) => setSubscriptionUrl(event.target.value)} placeholder={subscriptionID ? 'Leave blank to keep the saved URL' : 'https://…'} autoComplete="off" />
      <div className="composer-actions"><button className="ghost" type="button" onClick={closeComposer}>Cancel</button><button type="submit" disabled={busy || lifecycleBlocked || (!subscriptionID && !subscriptionUrl.trim())}>{subscriptionID ? 'Preview update' : 'Preview subscription'}</button></div>
    </form>}

    {!!subscriptions.length && <div className="subscription-strip">
      {subscriptions.map((subscription) => { const enabled = subscription.enabled !== false; const name = subscription.name || 'Unnamed subscription'; const autoStatus = subscription.autoRefresh; const autoState = autoRefreshState(autoStatus); return <div className={`subscription-card ${enabled ? '' : 'disabled'}`} key={subscription.id}>
        <div><strong>{name}</strong><small>{enabled ? 'Enabled' : 'Disabled'} · {subscription.nodeCount} nodes{subscription.staleCount ? ` · ${subscription.staleCount} stale` : ''}</small>{autoStatus && <div className={`subscription-auto-refresh ${autoState}`} data-testid={`subscription-auto-refresh-${subscription.id}`}><span>{autoRefreshStateLabels[autoState]}</span><small>{autoRefreshSummary(autoStatus)}</small></div>}</div>
        <div className="subscription-actions">
          <IconButton icon="refresh" label={`Refresh ${name}`} disabled={busy || lifecycleBlocked} onClick={() => requestPreview('/api/v1/subscriptions/refresh/preview', { subscriptionId: subscription.id })} />
          <IconButton icon="edit" label={`Edit ${name}`} disabled={busy} onClick={() => openSubscriptionEditor(subscription)} />
          <IconButton icon="power" label={enabled ? `Disable ${name}` : `Enable ${name}`} active={!enabled} disabled={busy || lifecycleBlocked} onClick={() => requestPreview('/api/v1/subscriptions/state/preview', { subscriptionId: subscription.id, enabled: !enabled })} />
          <IconButton icon="trash" label={`Remove ${name}`} tone="danger" disabled={busy || lifecycleBlocked} onClick={() => requestPreview('/api/v1/subscriptions/remove/preview', { subscriptionId: subscription.id })} />
        </div>
      </div> })}
    </div>}

    <div className="node-toolbar">
      <div className="toolbar-main">
        <label className="search-box"><Icon name="search" /><input value={query} onChange={(event) => onViewStateChange((current) => ({ ...current, query: event.target.value, page: 1 }))} placeholder="Search name, address, or source" aria-label="Search nodes" /></label>
        <span>{filtered.length} / {nodes.length}</span>
      </div>
      <div className="filter-row">
        <div className="filter-group" role="group" aria-label="Health filters">
          {[['all', 'All'], ['alive', 'Alive'], ['unhealthy', 'Not alive'], ['disabled', 'Disabled'], ['stale', 'Stale']].map(([value, label]) => <button type="button" key={value} className={statusFilter === value ? 'active' : ''} onClick={() => chooseFilter('statusFilter', value)}>{label} <span>{statusCounts[value]}</span></button>)}
        </div>
        <div className="filter-group" role="group" aria-label="Role filters">
          {[['all', 'Any role'], ['native', 'Native'], ['override', 'Override'], ['effective', 'Effective'], ['none', 'No role']].map(([value, label]) => <button type="button" key={value} className={roleFilter === value ? 'active' : ''} onClick={() => chooseFilter('roleFilter', value)}>{label}</button>)}
        </div>
        <label className="filter-select">Source<select aria-label="Filter by source" value={sourceFilter} onChange={(event) => chooseFilter('sourceFilter', event.target.value)}><option value="all">All</option>{sourceOptions.map((source) => <option value={source} key={source}>{source}</option>)}</select></label>
        <label className="filter-select">Country<select aria-label="Filter by country" value={countryFilter} onChange={(event) => chooseFilter('countryFilter', event.target.value)}><option value="all">All</option>{countryOptions.map((country) => <option value={country} key={country}>{country}</option>)}</select></label>
        {filtersActive && <button className="clear-filters" type="button" onClick={clearFilters}>Clear</button>}
      </div>
    </div>

    <div className="node-selection-toolbar" role="toolbar" aria-label="Selected node actions">
      <div className="selection-summary"><strong data-testid="selected-count">{selectedIDs.size} selected</strong><button className="clear-filters" type="button" onClick={toggleAllFiltered} disabled={busy || lifecycleBlocked || !filtered.length || allFilteredSelected}>Select all {filtered.length} filtered</button><button className="clear-filters" type="button" onClick={clearSelection} disabled={busy || lifecycleBlocked || !selectedIDs.size}>Clear selection</button></div>
      <div className="selection-actions">
        <button type="button" onClick={() => setManualOverride(selectedManual ? '' : (selectedNode.outboundTag || selectedNode.tag))} disabled={busy || lifecycleBlocked || !selectedNode || (!selectedManual && !selectedNode.enabled)}>{selectedManual ? 'Clear manual override' : 'Set manual override'}</button>
        <button type="button" onClick={runManualNode} disabled={busy || manualRequestBusy || manualLifecycleBlocked || benchmarkRunning || manualRunning || adaptiveRunning || selectedNodes.length !== 1 || !selectedNode?.enabled}>{manualRequestBusy ? 'Starting speed test…' : 'Full speed test'}</button>
        <button type="button" onClick={openEditor} disabled={busy || lifecycleBlocked || selectedNodes.length !== 1}>Edit / replace profile</button>
        <button type="button" onClick={() => requestPreview('/api/v1/nodes/batch/state/preview', { nodeIds: selectedNodeIDs, enabled: true })} disabled={busy || lifecycleBlocked || !selectedNodes.length || selectedNodes.every((node) => node.enabled)}>Enable</button>
        <button type="button" onClick={() => requestPreview('/api/v1/nodes/batch/state/preview', { nodeIds: selectedNodeIDs, enabled: false })} disabled={busy || lifecycleBlocked || !selectedNodes.length || selectedNodes.every((node) => !node.enabled)}>Disable</button>
        <button type="button" className="danger-action" onClick={() => requestPreview('/api/v1/nodes/batch/remove/preview', { nodeIds: selectedNodeIDs })} disabled={busy || lifecycleBlocked || !selectedNodes.length}>Delete</button>
      </div>
    </div>

    {manualStatus.state !== 'idle' && <ManualPerformanceCard status={manualStatus} node={nodes.find((node) => node.id === manualStatus.targetNodeId)} />}
    <AdaptiveQualityCard status={adaptiveStatus} nodes={nodes} />

    {selectedNode && editingID === selectedNode.id && <div className="selection-editor">
      <div><span className="panel-label">Replace profile</span><NodeName node={selectedNode} /><small>Stable tag: <code>{selectedNode.outboundTag || selectedNode.tag}</code></small></div>
      <input aria-label="Replacement VLESS profile" value={replacement} onChange={(event) => setReplacement(event.target.value)} placeholder="Replacement vless:// profile" type="password" autoComplete="off" autoFocus />
      <div className="inline-editor-actions"><button className="ghost" type="button" onClick={() => { setEditingID(''); setReplacement('') }}>Cancel</button><button type="button" disabled={busy || lifecycleBlocked || !replacement.trim()} onClick={() => requestPreview('/api/v1/nodes/replace/preview', { id: selectedNode.id, profile: replacement })}>Preview replacement</button></div>
    </div>}

    <div className="table-wrap"><table className="nodes-table"><thead><tr><th className="selection-column"><SelectionCheckbox label="Select all filtered nodes" checked={allFilteredSelected} indeterminate={selectedFilteredCount > 0 && !allFilteredSelected} onChange={toggleAllFiltered} /></th><SortHeader label="Name" sortKey="name" sort={sort} onSort={changeSort} /><SortHeader label="Address" sortKey="address" sort={sort} onSort={changeSort} /><SortHeader label="Health" sortKey="health" sort={sort} onSort={changeSort} /><SortHeader label="Latency" sortKey="latency" sort={sort} onSort={changeSort} /><SortHeader label="Role" sortKey="role" sort={sort} onSort={changeSort} /><SortHeader label="Source" sortKey="source" sort={sort} onSort={changeSort} /></tr></thead><tbody>
      {visibleNodes.map((node) => <NodeRows key={node.id || node.tag} node={node} selected={selectedIDs.has(node.id)} onToggle={() => toggleSelection(node.id)} />)}
      {!visibleNodes.length && <tr><td colSpan="7" className="empty">No nodes match this view.</td></tr>}
    </tbody></table></div>

    <Pagination page={page} totalPages={totalPages} onPage={(value) => onViewStateChange((current) => ({ ...current, page: value }))} />
    {preview && <PreviewDialog preview={preview} nodes={nodes} manualOverride={manualOverride} busy={busy || lifecycleBlocked} onCancel={cancelPreview} onApply={applyPreview} />}
  </section>
}

function AdaptiveQualityCard({ status, nodes }) {
  const nodesByTag = new Map(nodes.map((node) => [safeCanonicalTag(node.outboundTag || node.tag), node]))
  const candidates = safeAdaptiveCandidates(status)
  const current = targetPresentation(status?.currentTarget, nodesByTag)
  const switchedTag = status?.switchApplied === true ? safeCanonicalTag(status.selectedTarget) : ''
  const nextRunAt = formatTime(status?.nextRunAt)
  const completedAt = formatTime(status?.completedAt)
  const state = adaptiveState(status)
  return <section className={`adaptive-performance ${state}`} data-testid="adaptive-performance" aria-live="polite">
    <div className="adaptive-performance-heading"><div><span className="panel-label">Automatic quality</span><h3>{adaptiveStateLabel(status)}</h3></div><span className="chip neutral">{state === 'unavailable' ? 'Unavailable' : adaptiveStateLabel(status)}</span></div>
    <div className="adaptive-performance-target"><span>Current target</span><strong>{current.label}</strong>{current.tag && <code>{current.tag}</code>}</div>
    <div className="adaptive-facts"><div><span>Result</span><strong>{adaptiveOutcomeLabel(status, nodesByTag)}</strong></div>{nextRunAt !== '—' && <div><span>Next adaptive check</span><strong>{nextRunAt}</strong></div>}{completedAt !== '—' && <div><span>Last completion</span><strong>{completedAt}</strong></div>}{hasAdaptiveGeneration(status, candidates) && <div><span>Generation evidence</span><strong>{safeCount(status?.shortlistCount, 6)} shortlisted · {safeCount(status?.validCount, 6)} valid</strong></div>}</div>
    {candidates.length > 0 ? <div className="adaptive-candidates" aria-label="Adaptive candidates">
      {candidates.map((candidate, index) => {
        const target = targetPresentation(candidate.tag, nodesByTag)
        const valid = candidate.valid === true
        const isCurrent = candidate.tag === current.tag
        const isSwitched = Boolean(switchedTag) && candidate.tag === switchedTag
        return <article className={`adaptive-candidate ${valid ? 'valid' : 'invalid'}`} data-testid="adaptive-candidate" key={`${candidate.tag}-${index}`}>
          <div className="adaptive-candidate-heading"><div><strong>{target.label}</strong><code>{target.tag}</code></div><div className="adaptive-candidate-badges">{isCurrent && <span className="chip blue">Current target</span>}{isSwitched && <span className="chip green">Switched target</span>}<span className={`chip ${valid ? 'green' : 'amber'}`}>{valid ? 'Valid' : 'Invalid'}</span></div></div>
          <div className="adaptive-metric-grid"><div><span>RTT</span><strong>{formatAdaptiveLatency(candidate.rttMs)}</strong></div><div><span>Download</span><strong>{formatAdaptiveRate(candidate.downloadBps)}</strong></div><div><span>Upload</span><strong>{formatAdaptiveRate(candidate.uploadBps)}</strong></div><div><span>Quality</span><strong>{formatAdaptiveScore(candidate.score, valid)}</strong></div></div>
        </article>
      })}
    </div> : <p className="adaptive-empty">{state === 'waiting' ? 'Waiting for the next scheduled adaptive check.' : adaptiveOutcomeLabel(status, nodesByTag)}</p>}
  </section>
}

function ManualPerformanceCard({ status, node }) {
  const error = status.errorCode ? manualErrorLabels[status.errorCode] || 'The diagnostic ended with a safe error.' : ''
  return <section className={`manual-performance ${status.state}`} data-testid="manual-performance" aria-live="polite">
    <div className="manual-performance-heading">
      <div><span className="panel-label">Full speed test</span><h3>{manualStatusLabel(status.state)} · {manualPhaseLabel(status.phase)}</h3></div>
      {status.elapsedMs != null && <span className="chip neutral">{(Number(status.elapsedMs || 0) / 1000).toFixed(1)}s</span>}
    </div>
    <div className="manual-performance-target"><span>Target</span><strong>{visibleNodeName(node) || 'Unavailable'}</strong>{status.targetNodeId && <code>{status.targetNodeId}</code>}{status.targetTag && <code>{status.targetTag}</code>}</div>
    <div className="manual-progress-grid">
      <div><span>Stages</span><strong>{status.completedStages || 0} / {status.plannedStages || 0}</strong></div>
      <div><span>Bytes</span><strong>{formatManualBytes(status.bytesTransferred)} / {formatManualBytes(status.bytesPlanned)}</strong></div>
      <div><span>Latency</span><strong>{formatAdaptiveLatency(status.latencyMs)}</strong></div>
      <div><span>Download</span><strong>{formatManualRate(status.downloadBps)}</strong></div>
      <div><span>Upload</span><strong>{formatManualRate(status.uploadBps)}</strong></div>
    </div>
    {status.currentStage && <small className="manual-current-stage">Stage: {status.currentStage}</small>}
    {error && <p className="warning">{error}</p>}
  </section>
}

function NodeRows({ node, selected, onToggle }) {
  const health = node.alive ? 'Alive' : (node.enabled ? (node.lastError || 'No data') : 'Disabled')
  return <>
    <tr>
      <td className="selection-column"><SelectionCheckbox label={`Select ${visibleNodeName(node)}`} checked={selected} onChange={onToggle} /></td>
      <td><NodeName node={node} />{node.stale && <span className="chip amber">stale</span>}</td>
      <td><code className="address">{node.address || '—'}</code></td>
      <td><span className={`status-dot ${node.alive ? 'up' : 'down'}`}></span>{health}</td>
      <td>{formatAdaptiveLatency(node.latencyMs)}</td>
      <td><NodeBadges node={node} /></td>
      <td><span>{node.subscriptionName || node.sourceType || 'legacy'}</span></td>
    </tr>
  </>
}

function PreviewDialog({ preview, nodes, manualOverride, busy, onCancel, onApply }) {
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const changes = preview.changes || []
  const effectiveChanged = changes.some((change) => byID.get(change.id)?.isEffective)
  const manualChanged = changes.some((change) => {
    const node = byID.get(change.id)
    return node && (node.isOverride || manualOverride === (node.outboundTag || node.tag))
  })
  const subscriptionRemovalCount = preview.operation === 'subscription-refresh'
    ? changes.filter((change) => change.after === 'removed' && change.sourceType === 'subscription').length
    : 0
  const manualSubscriptionRemovals = ['remove', 'batch-remove'].includes(preview.operation)
    && changes.some((change) => change.after === 'removed' && change.sourceType === 'subscription')
  return <div className="modal-backdrop" role="presentation">
    <div className="preview-dialog" role="dialog" aria-modal="true" aria-label="Preview node change">
      <div className="dialog-heading"><div><span className="panel-label">Preview · {preview.operation}</span><h3>{preview.noop ? 'No persistent change' : `${preview.changes?.length || 0} node changes`}</h3></div><IconButton icon="close" label="Close preview" onClick={onCancel} disabled={busy} /></div>
      <div className="diff-list">
        {changes.map((change) => <div className="diff-row" key={`${change.action}-${change.id}`}><strong>{change.name}</strong><span>{change.before} → {change.after}</span></div>)}
        {preview.noop && <p className="muted">The fetched or requested state matches the current registry.</p>}
      </div>
      {subscriptionRemovalCount > 0 && <p className="warning" role="alert">Provider snapshot removes {subscriptionRemovalCount} {subscriptionRemovalCount === 1 ? 'node that is' : 'nodes that are'} no longer present upstream.</p>}
      {effectiveChanged && <p className="warning" role="alert">The currently effective node changes in this preview. Active proxy traffic will be reselected after Apply.</p>}
      {manualChanged && <p className="warning" role="alert">The current manual-override node changes in this preview. The supervisor owns subsequent reconciliation; this batch mutation does not write override state.</p>}
      {manualSubscriptionRemovals && <p className="warning" role="alert">A removed subscription-owned node may return on a later subscription refresh while it remains upstream.</p>}
      {preview.effectiveImpact && !effectiveChanged && <p className="warning" role="alert">This operation will {preview.effectiveImpact} the currently effective node. Active proxy traffic will be reselected after Apply.</p>}
      <div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel</button><button type="button" onClick={onApply} disabled={busy}>{busy ? 'Applying…' : (preview.noop ? 'Confirm no-op' : 'Apply and validate')}</button></div>
    </div>
  </div>
}

function Pagination({ page, totalPages, onPage }) {
  if (totalPages <= 1) return null
  return <nav className="pagination" aria-label="Node pages">
    <IconButton icon="left" label="Previous page" disabled={page <= 1} onClick={() => onPage(page - 1)} />
    <div>{Array.from({ length: totalPages }, (_, index) => index + 1).map((value) => <button type="button" key={value} className={value === page ? 'active' : ''} aria-current={value === page ? 'page' : undefined} onClick={() => onPage(value)}>{value}</button>)}</div>
    <IconButton icon="right" label="Next page" disabled={page >= totalPages} onClick={() => onPage(page + 1)} />
  </nav>
}

const restoreBlockerMessages = {
  'appliance-authority-not-adopted': 'The local appliance authority is not ready for restore.',
  'appliance-authority-unavailable': 'The local appliance authority is unavailable.',
  'appliance-authority-invalid': 'The current appliance authority is invalid.',
  'nodes-authority-unavailable': 'The current node registry is unavailable.',
  'nodes-authority-invalid': 'The current node registry is invalid.',
  'nodes-authority-unsupported': 'The current node registry cannot accept this restore mode.',
  'runtime-verifier-unavailable': 'The runtime verifier is unavailable.',
  'candidate-validator-unavailable': 'The restore candidate cannot be validated.',
}

const restoreBlockerMessage = (code) => restoreBlockerMessages[code] || 'Restore is blocked by a compatibility check.'

function BackupRestoreSection({ csrf, restoreState, setRestoreState, onRefresh, onUnauthorized, lifecycleBlocked }) {
  const [safeBusy, setSafeBusy] = useState(false)
  const [secretBusy, setSecretBusy] = useState(false)
  const [restoreBusy, setRestoreBusy] = useState(false)
  const [notice, setNotice] = useState(null)
  const [secretForm, setSecretForm] = useState({ currentPassword: '', passphrase: '', confirmation: '' })
  const [mode, setMode] = useState('settings-only')
  const [file, setFile] = useState(null)
  const [passphrase, setPassphrase] = useState('')
  const [destructiveConfirmed, setDestructiveConfirmed] = useState(false)
  const fileInput = useRef(null)
  const preview = restoreState?.preview
  const effectiveMode = preview?.mode || mode
  const destructive = effectiveMode !== 'settings-only'
  const blockers = preview?.compatibility?.blockers || []
  const previewToken = preview?.previewToken
  const canApply = Boolean(previewToken) && blockers.length === 0 && (!destructive || destructiveConfirmed)

  const clearSecretForm = () => setSecretForm({ currentPassword: '', passphrase: '', confirmation: '' })

  const handleError = (cause) => {
    if (cause.status === 401 && cause.code === 'reauthentication failed') {
      clearSecretForm()
      setNotice({ tone: 'error', message: 'Current panel password was not accepted.' })
      return
    }
    if (cause.status === 401) {
      onUnauthorized()
      return
    }
    setNotice({ tone: 'error', message: cause.message || 'Backup or restore request failed.' })
  }

  const exportSafe = async () => {
    setSafeBusy(true)
    setNotice(null)
    try {
      await download('/api/v1/backup/export', {}, 'xkeen-control-backup.json')
      setNotice({ tone: 'success', message: 'Safe settings backup downloaded.' })
    } catch (cause) {
      handleError(cause)
    } finally {
      setSafeBusy(false)
    }
  }

  const exportSecret = async (event) => {
    event.preventDefault()
    const { currentPassword, passphrase: secretPassphrase, confirmation } = secretForm
    setNotice(null)
    const passphraseBytes = new TextEncoder().encode(secretPassphrase).length
    if (!currentPassword || passphraseBytes < MIN_BACKUP_PASSPHRASE_BYTES || passphraseBytes > MAX_BACKUP_PASSPHRASE_BYTES || secretPassphrase !== confirmation) {
      clearSecretForm()
      setNotice({ tone: 'error', message: 'Enter a matching passphrase between 12 and 256 bytes.' })
      return
    }
    setSecretBusy(true)
    try {
      await download('/api/v1/backup/export-secret', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ currentPassword, passphrase: secretPassphrase }),
      }, 'xkeen-control-backup-encrypted.json')
      setNotice({ tone: 'success', message: 'Encrypted secret-bearing backup downloaded. Keep its passphrase separate.' })
    } catch (cause) {
      handleError(cause)
    } finally {
      clearSecretForm()
      setSecretBusy(false)
    }
  }

  const chooseFile = (event) => {
    const selected = event.target.files?.[0] || null
    setFile(selected)
    setRestoreState({ preview: null })
    setDestructiveConfirmed(false)
    setNotice(null)
  }

  const previewRestore = async () => {
    if (!file || lifecycleBlocked) return
    if (file.size > MAX_RESTORE_BUNDLE_BYTES) {
      setNotice({ tone: 'error', message: 'The selected backup exceeds the 9 MiB bundle limit.' })
      return
    }
    if (destructive && !destructiveConfirmed) {
      setNotice({ tone: 'error', message: 'Confirm the destructive registry restore before previewing it.' })
      return
    }
    setRestoreBusy(true)
    setNotice(null)
    try {
      const form = new FormData()
      form.append('bundle', file)
      if (passphrase) form.append('passphrase', passphrase)
      const value = await api(`/api/v1/backup/import/preview?mode=${encodeURIComponent(mode)}`, {
        method: 'POST',
        headers: { 'X-CSRF-Token': csrf },
        body: form,
      })
      setRestoreState({ preview: value })
      setFile(null)
      setPassphrase('')
      if (fileInput.current) fileInput.current.value = ''
      setNotice({ tone: 'success', message: 'Preview ready. The upload and passphrase were cleared; Apply uses only the short-lived preview token.' })
    } catch (cause) {
      handleError(cause)
    } finally {
      setRestoreBusy(false)
    }
  }

  const applyRestore = async () => {
    const token = previewToken
    if (!canApply || lifecycleBlocked) return
    setRestoreBusy(true)
    setNotice(null)
    try {
      await api('/api/v1/backup/import/apply', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ previewToken: token }),
      })
      setRestoreState({ preview: null })
      await onRefresh()
      setNotice({ tone: 'success', message: 'Restore applied and the dashboard was refreshed.' })
    } catch (cause) {
      handleError(cause)
    } finally {
      setRestoreBusy(false)
    }
  }

  const cancelRestore = async () => {
    const token = preview?.previewToken
    setRestoreState({ preview: null })
    if (!token) return
    setRestoreBusy(true)
    try {
      await api('/api/v1/backup/import/cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf },
        body: JSON.stringify({ previewToken: token }),
      })
      setNotice({ tone: 'success', message: 'Restore preview canceled.' })
    } catch (cause) {
      handleError(cause)
    } finally {
      setRestoreBusy(false)
    }
  }

  return <div className="section-stack backup-restore-section">
    {notice && <Notice message={notice.message} tone={notice.tone} />}
    <section className="panel backup-card">
      <div className="backup-card-heading"><div><span className="panel-label">Backup</span><h2>Download current settings</h2><p className="muted">Safe export contains appliance policy and no node secrets.</p></div><button type="button" onClick={exportSafe} disabled={safeBusy}>{safeBusy ? 'Preparing…' : 'Download safe backup'}</button></div>
      <form className="secret-export" onSubmit={exportSecret}>
        <div><span className="panel-label">Encrypted export</span><h3>Include the node registry</h3><p className="muted">This download contains secret-bearing node material. It is never stored in browser storage.</p></div>
        <label>Current panel password<input type="password" autoComplete="current-password" value={secretForm.currentPassword} onChange={(event) => setSecretForm((current) => ({ ...current, currentPassword: event.target.value }))} /></label>
        <label>Encryption passphrase<input type="password" autoComplete="new-password" value={secretForm.passphrase} onChange={(event) => setSecretForm((current) => ({ ...current, passphrase: event.target.value }))} /></label>
        <label>Confirm passphrase<input type="password" autoComplete="new-password" value={secretForm.confirmation} onChange={(event) => setSecretForm((current) => ({ ...current, confirmation: event.target.value }))} /></label>
        <button type="submit" disabled={secretBusy}>{secretBusy ? 'Preparing…' : 'Download encrypted backup'}</button>
      </form>
    </section>

    <section className="panel backup-card">
      <div><span className="panel-label">Restore</span><h2>Import a local backup</h2><p className="muted">Choose one JSON bundle. The server enforces the 10 MiB request and 9 MiB bundle limits.</p></div>
      <div className="restore-form">
        <label>Restore mode<select value={effectiveMode} onChange={(event) => { setMode(event.target.value); setRestoreState({ preview: null }); setDestructiveConfirmed(false) }} disabled={restoreBusy || lifecycleBlocked || Boolean(preview)}><option value="settings-only">Settings only</option><option value="replace-registry">Replace registry (destructive)</option><option value="merge-registry">Merge registry (destructive)</option></select></label>
        <label>Backup bundle<input ref={fileInput} type="file" accept="application/json,.json" onChange={chooseFile} disabled={restoreBusy || lifecycleBlocked || Boolean(preview)} /></label>
        <label>Passphrase (encrypted backup only)<input type="password" autoComplete="off" value={passphrase} onChange={(event) => setPassphrase(event.target.value)} disabled={restoreBusy || lifecycleBlocked || Boolean(preview)} /></label>
      </div>
      {destructive && <label className="restore-confirm"><input type="checkbox" checked={destructiveConfirmed} onChange={(event) => setDestructiveConfirmed(event.target.checked)} disabled={restoreBusy || lifecycleBlocked} /> I understand this restore can replace or merge secret-bearing node registry state.</label>}
      {!preview && <div className="preview-actions"><button type="button" onClick={previewRestore} disabled={restoreBusy || lifecycleBlocked || !file || (destructive && !destructiveConfirmed)}>{restoreBusy ? 'Previewing…' : 'Preview restore'}</button></div>}
      {preview && <RestorePreviewSummary preview={preview} blockers={blockers} busy={restoreBusy || lifecycleBlocked} canApply={canApply && !lifecycleBlocked} onCancel={cancelRestore} onApply={applyRestore} />}
    </section>
  </div>
}

function RestorePreviewSummary({ preview, blockers, busy, canApply, onCancel, onApply }) {
  const changes = preview.changes || {}
  return <div className="restore-preview" aria-live="polite">
    <div className="dialog-heading"><div><span className="panel-label">Restore preview</span><h3>{preview.noop ? 'No persistent change' : 'Ready for confirmation'}</h3></div><span className="chip neutral">Expires {formatTime(preview.expiresAt)}</span></div>
    <div className="restore-summary-grid">
      <div><span>Mode</span><strong>{preview.mode || '—'}</strong></div>
      <div><span>Contains secrets</span><strong>{preview.containsSecrets ? 'Yes' : 'No'}</strong></div>
      <div><span>Appliance changed</span><strong>{changes.applianceChanged ? 'Yes' : 'No'}</strong></div>
      <div><span>Subscriptions</span><strong>+{changes.subscriptionsAdded || 0} / −{changes.subscriptionsRemoved || 0} / ~{changes.subscriptionsChanged || 0}</strong></div>
      <div><span>Nodes</span><strong>+{changes.nodesAdded || 0} / −{changes.nodesRemoved || 0} / ~{changes.nodesChanged || 0}</strong></div>
      <div><span>Result</span><strong>{preview.noop ? 'No-op' : 'Changes detected'}</strong></div>
    </div>
    {blockers.length > 0 && <div className="restore-blockers"><strong>Compatibility blockers</strong>{blockers.map((code, index) => <p className="warning" key={`${code}-${index}`}>{restoreBlockerMessage(code)}</p>)}</div>}
    <div className="preview-actions"><button className="ghost" type="button" onClick={onCancel} disabled={busy}>Cancel</button><button type="button" onClick={onApply} disabled={busy || !canApply}>{busy ? 'Applying…' : 'Apply restore'}</button></div>
  </div>
}

function SystemSection({ status, config, nodesByTag, update, onCheckUpdate }) {
  const dnsServers = (config.dns?.upstreams || []).map((upstream) => upstream.host || upstream.tag).filter(Boolean)
  const healthSelectors = config.observatory?.subjectSelectors || []
  const activeTag = status.selection?.effectiveTarget || status.balancer?.effective
  const activeNode = nodesByTag.get(activeTag)

  return <section className="lower-grid system-section">
    <div className="panel"><span className="panel-label">Network policy</span><h2>How traffic is handled</h2><div className="metric-row"><span>Routing rules</span><strong>{config.routing?.ruleCount ?? '—'}</strong></div><div className="metric-row"><span>DNS servers</span><div className="metric-value-list">{dnsServers.length ? dnsServers.map((server) => <code key={server}>{server}</code>) : <strong>—</strong>}</div></div><div className="metric-row"><span>Proxy pool</span><strong>Unified proxy pool</strong></div><div className="metric-row"><span>Health-check scope</span><div className="metric-value-list">{healthSelectors.length ? healthSelectors.map((selector) => <code key={selector}>{selector}</code>) : <strong>—</strong>}</div></div></div>
    <div className="panel"><span className="panel-label">Runtime</span><h2>Current state</h2><div className="metric-row"><span>Active proxy</span><div className="metric-value">{activeNode ? <strong>{visibleNodeName(activeNode)}</strong> : <strong>{safeCanonicalTag(activeTag) || 'Automatic selection'}</strong>}{activeNode?.address && <small>{activeNode.address}</small>}</div></div><div className="metric-row"><span>Healthy proxies</span><strong>{status.observatory?.healthy ?? 0} / {status.observatory?.total ?? 0}</strong></div><div className="metric-row"><span>Control plane uptime</span><strong>{formatUptime(status.controlPlane?.uptimeSeconds)}</strong></div></div>
    <div className="panel"><span className="panel-label">Signed panel release</span><h2>{update?.installed?.version || 'development'}</h2><div className="metric-row"><span>Channel</span><strong>{update?.channel || 'stable'}</strong></div><div className="metric-row"><span>Latest compatible</span><strong>{update?.latestCompatibleVersion || 'Not checked'}</strong></div><div className="metric-row"><span>Rollback</span><strong>{update?.rollbackAvailable ? 'Available' : 'None'}</strong></div><button type="button" onClick={onCheckUpdate}>Check fixed GitHub release</button></div>
  </section>
}

function SortHeader({ label, sortKey, sort, onSort }) {
  const active = sort.key === sortKey
  const direction = active ? sort.direction : 'none'
  return <th aria-sort={direction === 'none' ? 'none' : direction === 'asc' ? 'ascending' : 'descending'}><button className={`sort-button ${active ? 'active' : ''}`} type="button" onClick={() => onSort(sortKey)}>{label}<span aria-hidden="true">{active ? (sort.direction === 'asc' ? '↑' : '↓') : '↕'}</span></button></th>
}

function SelectionCheckbox({ checked, indeterminate = false, label, onChange }) {
  const inputRef = useRef(null)
  useEffect(() => {
    if (inputRef.current) inputRef.current.indeterminate = indeterminate
  }, [indeterminate])
  return <input ref={inputRef} type="checkbox" aria-label={label} checked={checked} onChange={onChange} />
}

function NodeName({ node }) {
  const flag = COUNTRY_FLAGS[node?.countryCode]
  return <strong className="display-name">{flag && <img className="country-flag" src={flag} alt="" aria-hidden="true" />}<span>{visibleNodeName(node)}</span></strong>
}

function IconButton({ icon, label, tone = '', active = false, ...props }) {
  return <button type="button" className={`icon-button ${tone} ${active ? 'active' : ''}`} aria-label={label} title={label} data-tooltip={label} {...props}><Icon name={icon} /></button>
}

function Icon({ name }) {
  const paths = {
    plus: <><path d="M12 5v14M5 12h14" /></>,
    link: <><path d="M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.1 1.1" /><path d="M14 11a5 5 0 0 0-7.1-.1l-2 2A5 5 0 0 0 12 20l1.1-1.1" /></>,
    refresh: <><path d="M20 11a8 8 0 1 0-2.3 5.7" /><path d="M20 4v7h-7" /></>,
    edit: <><path d="M4 20h4l11-11-4-4L4 16v4Z" /><path d="m13.5 6.5 4 4" /></>,
    power: <><path d="M12 2v10" /><path d="M6.3 5.7a8 8 0 1 0 11.4 0" /></>,
    trash: <><path d="M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13" /></>,
    close: <><path d="m6 6 12 12M18 6 6 18" /></>,
    left: <><path d="m15 18-6-6 6-6" /></>,
    right: <><path d="m9 18 6-6-6-6" /></>,
    search: <><circle cx="11" cy="11" r="7" /><path d="m20 20-4-4" /></>,
    gauge: <><path d="M4 16a8 8 0 1 1 16 0" /><path d="M12 12l4-4" /><path d="M6 19h12" /></>,
    target: <><circle cx="12" cy="12" r="7" /><circle cx="12" cy="12" r="2" /><path d="M12 2v3M12 19v3M2 12h3M19 12h3" /></>,
  }
  return <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">{paths[name]}</svg>
}

function Shell({ children }) { return <main className="app-shell">{children}</main> }
function Notice({ message, tone = 'error' }) { return <div className={`notice ${tone}`} role="status">{message}</div> }
function HealthCard({ label, ok, detail }) { return <div className="health-card"><div className={`health-icon ${ok ? 'ok' : 'bad'}`}>{ok ? '✓' : '!'}</div><div><span className="panel-label">{label}</span><strong>{ok ? 'Healthy' : 'Degraded'}</strong><small>{detail}</small></div></div> }
function SelectionCard({ label, node, tone, emptyText = 'No current target' }) { return <div className={`panel selection-card ${tone}`}><span className="panel-label">{label}</span>{node ? <NodeName node={node} /> : <strong>{emptyText}</strong>}<small>{node?.address || (node ? 'No address' : '')}</small></div> }
function NodeBadges({ node }) { return <div className="badges">{node.isNativeSelected && <span className="chip blue">native</span>}{node.isOverride && <span className="chip amber">override</span>}{node.isEffective && <span className="chip green">effective</span>}</div> }
function formatUptime(seconds) { if (!seconds) return '—'; const hours = Math.floor(seconds / 3600); const minutes = Math.floor((seconds % 3600) / 60); return `${hours}h ${minutes}m` }

createRoot(document.getElementById('root')).render(<StrictMode><App /></StrictMode>)
