import { StrictMode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { createDashboardReader } from './dashboard-reader.js'
import './theme.css'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Checkbox } from '@/components/ui/checkbox'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Disclosure, MobileNavigationDrawer, Modal } from './ui.jsx'
import { IconHome, IconServer, IconSitemap, IconWorld, IconChartBar, IconCube, IconHistory, IconSettings, IconLogout, IconMenu2 } from '@tabler/icons-react'
import { IconPlus, IconLink, IconRefresh, IconPencil, IconPower, IconTrash, IconX, IconChevronLeft, IconChevronRight, IconSearch, IconGauge, IconFocus2, IconArrowUp, IconArrowDown, IconArrowsSort, IconSquareCheck, IconPlayerPlay, IconPlayerPause } from '@tabler/icons-react'
import { DNSLifecycleNotice, DNSObservatorySection, useDNSObservatoryController } from './dns-observatory.jsx'
import { PerformancePolicySection, usePerformancePolicyController } from './performance-policy.jsx'
import { RoutingLifecycleNotice, RoutingPolicySection, useRoutingController } from './routing-policy.jsx'
import { NativeXkeenStatus, NativeXkeenSection } from './native-xkeen.jsx'
import { NativeConfigSection } from './native-config.jsx'
import { SystemPanelSection, useSystemPanelController } from './system-panel.jsx'

import flagAE from 'flag-icons/flags/4x3/ae.svg'
import flagAM from 'flag-icons/flags/4x3/am.svg'
import flagAT from 'flag-icons/flags/4x3/at.svg'
import flagBG from 'flag-icons/flags/4x3/bg.svg'
import flagBY from 'flag-icons/flags/4x3/by.svg'
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
import flagRU from 'flag-icons/flags/4x3/ru.svg'
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
  AE: flagAE, AM: flagAM, AT: flagAT, BG: flagBG, BY: flagBY, CA: flagCA, CZ: flagCZ,
  DE: flagDE, EE: flagEE, ES: flagES, FI: flagFI, FR: flagFR, GB: flagGB,
  IL: flagIL, IN: flagIN, KZ: flagKZ, LV: flagLV, NL: flagNL, PL: flagPL,
  RU: flagRU, SE: flagSE, SG: flagSG, TH: flagTH, TR: flagTR, US: flagUS, UZ: flagUZ,
}

const api = async (path, options = {}) => {
  const response = await fetch(path, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json', ...(options.headers || {}) },
    ...options,
  })
  let body
  try { body = await response.json() } catch {
    const error = new Error(response.ok ? 'Invalid response from the panel. Refresh its status before retrying.' : `Request failed (${response.status})`)
    if (!response.ok) error.status = response.status
    throw error
  }
  if (!response.ok) {
    const error = new Error(body?.error || `Request failed (${response.status})`)
    error.status = response.status
    error.code = body?.error
    throw error
  }
  if (!body || typeof body !== 'object' || Array.isArray(body)) throw new Error('Invalid response from the panel.')
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
      case 'source': return stringValue(node.sourceType)
      case 'subscription': return stringValue(node.subscriptionName)
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
  subscriptionFilter: 'all',
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
  const sessionEpoch = useRef(0)

  const reader = useRef(null)
  if (!reader.current) reader.current = createDashboardReader({
    api,
    onData: (data, full) => {
      setDashboard((current) => full ? data : current ? { ...current, ...data } : current)
      setError('')
    },
    onError: (cause) => {
      if (cause.status === 401) {
        sessionEpoch.current++
        reader.current.invalidate()
        setDashboard(null)
        setSession(null)
      }
      setError(cause.message)
    },
    onDone: () => setLoading(false),
  })
  const loadDashboard = useCallback(() => reader.current.refresh(true), [])
  const loadPerformance = useCallback(() => reader.current.refresh(false), [])
  useEffect(() => {
    let current = true
    const epoch = sessionEpoch.current
    api('/api/v1/session')
      .then((value) => {
        if (!current || epoch !== sessionEpoch.current) return
        setSession(value)
        return loadDashboard()
      })
      .catch((cause) => {
        if (!current || epoch !== sessionEpoch.current) return
        if (cause.status !== 401) setError(cause.message)
        setLoading(false)
      })
    return () => { current = false; reader.current.invalidate() }
  }, [loadDashboard])

  useEffect(() => {
    if (!session) return undefined
    const refreshVisible = () => { if (!document.hidden) loadDashboard() }
    const timer = window.setInterval(refreshVisible, 5000)
    document.addEventListener('visibilitychange', refreshVisible)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', refreshVisible)
    }
  }, [session, loadDashboard])

  const login = async (event) => {
    event.preventDefault()
    const epoch = ++sessionEpoch.current
    reader.current.invalidate()
    setError('')
    try {
      const value = await api('/api/v1/session/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      })
      if (epoch !== sessionEpoch.current) return
      reader.current.invalidate()
      setPassword('')
      setSession(value)
      await loadDashboard()
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return
      setError(cause.message)
    }
  }

  const logout = async () => {
    const epoch = ++sessionEpoch.current
    reader.current.invalidate()
    try {
      await api('/api/v1/session/logout', {
        method: 'POST',
        headers: { 'X-CSRF-Token': session?.csrfToken || '' },
      })
    } catch (cause) {
      if (epoch !== sessionEpoch.current) return
      setError(cause.message)
    }
    if (epoch !== sessionEpoch.current) return
    reader.current.invalidate()
    setDashboard(null)
    setSession(null)
  }

  const invalidateSession = useCallback(() => {
    sessionEpoch.current++
    reader.current.invalidate()
    setDashboard(null)
    setSession(null)
  }, [])

  if (!session) return <Login error={error} password={password} setPassword={setPassword} onSubmit={login} />
  if (loading && !dashboard) return <Shell><div className="loading">Reading current router state…</div></Shell>
  if (!dashboard) return <Shell><Notice message={error || 'Runtime state is unavailable.'} /></Shell>

  return <Dashboard dashboard={dashboard} session={session} error={error} onRefresh={loadDashboard} onPerformanceRefresh={loadPerformance} onLogout={logout} onUnauthorized={invalidateSession} />
}

function Login({ error, password, setPassword, onSubmit }) {
  return <main className="flex min-h-svh items-center justify-center p-6">
    <Card className="w-full max-w-sm">
      <CardHeader><CardTitle><h1>XKeen Control</h1></CardTitle><CardDescription>Sign in to manage your VPN.</CardDescription></CardHeader>
      <CardContent><form onSubmit={onSubmit}><FieldGroup>
        <Field><FieldLabel htmlFor="password">Panel password</FieldLabel>
          <Input id="password" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} autoFocus /></Field>
        <Button type="submit">Sign in</Button>
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      </FieldGroup></form></CardContent>
    </Card>
  </main>
}

function Dashboard({ dashboard, session, error, onRefresh, onPerformanceRefresh, onLogout, onUnauthorized }) {
  const { status, nodes, performance } = dashboard
  const [section, setSection] = useState('overview')
  const [componentsVisited, setComponentsVisited] = useState(false)
  const [nativeConfigJob, setNativeConfigJob] = useState(null)
  useEffect(() => setNativeConfigJob(null), [session.csrfToken])
  useEffect(() => { if (section === 'components') setComponentsVisited(true) }, [section])
  const [navigationOpen, setNavigationOpen] = useState(false)
  const navigationTrigger = useRef(null)
  const closeNavigation = useCallback(() => setNavigationOpen(false), [])
  const [nodeView, setNodeView] = useState(createNodeViewState)
  const [restoreState, setRestoreState] = useState({ preview: null })
  const registryNodes = nodes.nodes || []
  const nodesByTag = useMemo(() => new Map(registryNodes.map((node) => [node.outboundTag || node.tag, node])), [registryNodes])
  const routingControllerRef = useRef(null)
  const dnsControllerRef = useRef(null)
  const [unprovenPolicyReads, setUnprovenPolicyReads] = useState({ routing: false, dns: false })
  const invalidateRoutingPreview = useCallback(() => routingControllerRef.current?.invalidateLivePreview(), [])
  const invalidateDNSPreview = useCallback(() => dnsControllerRef.current?.invalidateLivePreview(), [])
  const refreshRoutingPeer = useCallback(() => { void routingControllerRef.current?.refreshPeerProjection() }, [])
  const refreshDNSPeer = useCallback(() => { void dnsControllerRef.current?.refreshPeerProjection() }, [])
  const markRoutingApplyUnproven = useCallback(() => {
    setUnprovenPolicyReads((current) => ({ ...current, routing: true }))
    invalidateDNSPreview()
  }, [invalidateDNSPreview])
  const markDNSApplyUnproven = useCallback(() => {
    setUnprovenPolicyReads((current) => ({ ...current, dns: true }))
    invalidateRoutingPreview()
  }, [invalidateRoutingPreview])
  const clearRoutingApplyUnproven = useCallback(() => setUnprovenPolicyReads((current) => current.routing ? { ...current, routing: false } : current), [])
  const clearDNSApplyUnproven = useCallback(() => setUnprovenPolicyReads((current) => current.dns ? { ...current, dns: false } : current), [])
  const appliancePolicyUncertain = unprovenPolicyReads.routing || unprovenPolicyReads.dns
  const routingController = useRoutingController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized, active: section === 'routing', appliancePolicyUncertain, onBeforeApply: invalidateDNSPreview, onApplied: refreshDNSPeer, onUnprovenApply: markRoutingApplyUnproven, onFreshReadAfterUnprovenApply: clearRoutingApplyUnproven })
  const dnsController = useDNSObservatoryController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized, active: section === 'dns', appliancePolicyUncertain, onBeforeApply: invalidateRoutingPreview, onApplied: refreshRoutingPeer, onUnprovenApply: markDNSApplyUnproven, onFreshReadAfterUnprovenApply: clearDNSApplyUnproven })
  const performanceOwnerBusy = Boolean(status.benchmark?.controlPlane?.running || performance?.manual?.state === 'running' || performance?.adaptive?.state === 'running')
  const performancePolicyController = usePerformancePolicyController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, performanceBusy: performanceOwnerBusy, onUnauthorized, active: section === 'performance' })
  const systemPanelController = useSystemPanelController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized, active: section === 'system' })
  routingControllerRef.current = routingController
  dnsControllerRef.current = dnsController
  const openComponents = useCallback(() => setSection('components'), [])
  const openRouting = useCallback(() => setSection('routing'), [])
  const openDNS = useCallback(() => setSection('dns'), [])
  const openBackup = useCallback(() => setSection('backup'), [])
  const lifecycleBlocked = typeof status.lifecycle?.maintenance !== 'boolean' || typeof status.lifecycle?.applying !== 'boolean' || status.lifecycle.maintenance || status.lifecycle.applying
  const manualLifecycleBlocked = lifecycleBlocked
  const manualRunning = performance?.manual?.state === 'running'
  const adaptiveRunning = performance?.adaptive?.state === 'running'
  const performancePolling = (section === 'overview' && adaptiveRunning)
    || (section === 'nodes' && (manualRunning || adaptiveRunning))
  const performancePollInFlight = useRef(false)

  useEffect(() => {
    setUnprovenPolicyReads({ routing: false, dns: false })
  }, [session.csrfToken])

  useEffect(() => {
    if (!performancePolling || !onPerformanceRefresh) return undefined
    let active = true
    const poll = async () => {
      if (!active || document.hidden || performancePollInFlight.current) return
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

  const sections = [
    ['overview', 'Overview', IconHome, () => setSection('overview')],
    ['nodes', 'Nodes', IconServer, () => setSection('nodes')],
    ['routing', 'Routing', IconSitemap, openRouting],
    ['dns', 'DNS', IconWorld, openDNS],
    ['performance', 'Performance', IconChartBar, () => setSection('performance')],
    ['components', 'Components / Updates', IconCube, openComponents],
    ['backup', 'Backup & Restore', IconHistory, openBackup],
    ['system', 'System / Panel', IconSettings, () => setSection('system')],
  ]
  const pageTitle = { components: 'Components', system: 'System' }[section] || sections.find(([key]) => key === section)?.[1]
  return <Shell>
    <a className="sr-only focus:not-sr-only focus:p-4" href="#workspace">Skip to workspace</a>
    <header className="flex items-center gap-3 border-b p-3 min-[761px]:hidden"><Button ref={navigationTrigger} type="button" variant="ghost" size="icon-lg" className="min-h-11 min-w-11" aria-label="Toggle navigation" aria-expanded={navigationOpen} aria-controls="mobile-dashboard-navigation" aria-haspopup="dialog" onClick={() => setNavigationOpen(!navigationOpen)}><IconMenu2 /></Button><strong>XKeen Control</strong></header>
    <aside className="fixed inset-y-0 left-0 hidden w-60 flex-col gap-4 border-r bg-sidebar p-4 min-[761px]:flex"><NavigationContent sections={sections} section={section} total={nodes.total || 0} version={status.controlPlane?.version || 'dev'} onSelect={closeNavigation} onLogout={onLogout} /></aside>
    <MobileNavigationDrawer returnFocus={navigationTrigger} open={navigationOpen} onClose={closeNavigation}><div className="flex min-h-full flex-col gap-4"><NavigationContent mobile sections={sections} section={section} total={nodes.total || 0} version={status.controlPlane?.version || 'dev'} onSelect={closeNavigation} onLogout={onLogout} /></div></MobileNavigationDrawer>
    <div id="workspace" className="min-w-0 min-[761px]:ml-60" tabIndex="-1"><div className="legacy-workspace"><div className="workspace">
    {section !== 'nodes' && <header className="page-heading"><h1>{pageTitle}</h1>{section === 'overview' && <button className="ghost" type="button" onClick={onRefresh}><Icon name="refresh" />Refresh</button>}</header>}
    <RoutingLifecycleNotice controller={routingController} active={section === 'routing'} onOpenRouting={openRouting} />
    <DNSLifecycleNotice controller={dnsController} active={section === 'dns'} onOpenDNS={openDNS} />
    {error && <Notice message={error} />}
    {section === 'overview' && <Overview status={status} performance={performance} nodeTotal={nodes.total || 0} nodesByTag={nodesByTag} csrfToken={session.csrfToken} onRefresh={onRefresh} onUnauthorized={onUnauthorized} onOpenNodes={() => setSection('nodes')} />}
    {section === 'nodes' && <NodeWorkspace nodes={registryNodes} subscriptions={nodes.subscriptions || []} performance={performance} manualOverride={status.selection?.manualOverride || ''} benchmarkRunning={Boolean(status.benchmark?.controlPlane?.running)} csrf={session.csrfToken} onRefresh={onRefresh} onPerformanceRefresh={onPerformanceRefresh} viewState={nodeView} onViewStateChange={setNodeView} lifecycleBlocked={lifecycleBlocked} manualLifecycleBlocked={manualLifecycleBlocked} selectionAvailable={false} />}
    {section === 'routing' && <RoutingPolicySection controller={routingController} lifecycle={status.lifecycle} />}
    {section === 'dns' && <DNSObservatorySection controller={dnsController} />}
    {section === 'performance' && <PerformancePolicySection controller={performancePolicyController} />}
    {section === 'components' && <NativeXkeenSection facts={status.native} onRefresh={onRefresh} onOpenSystem={() => setSection('system')} csrfToken={session.csrfToken} onUnauthorized={onUnauthorized} jobNotification={nativeConfigJob?.csrfToken === session.csrfToken ? nativeConfigJob.job : null} />}
    {(section === 'components' || componentsVisited) && <div hidden={section !== 'components'}><NativeConfigSection csrfToken={session.csrfToken} onUnauthorized={onUnauthorized} onNativeJob={(job) => setNativeConfigJob({ job, csrfToken: session.csrfToken })} /></div>}
    {section === 'backup' && <BackupRestoreSection csrf={session.csrfToken} restoreState={restoreState} setRestoreState={setRestoreState} onRefresh={onRefresh} onUnauthorized={onUnauthorized} lifecycleBlocked={lifecycleBlocked} />}
    {section === 'system' && <SystemPanelSection controller={systemPanelController} status={status} onOpenComponents={openComponents} onOpenBackup={openBackup} />}
    </div></div></div>
  </Shell>
}

function NavigationContent({ mobile = false, sections, section, total, version, onSelect, onLogout }) {
  return <>
    <div className="flex items-center justify-between gap-2"><strong>XKeen Control</strong>{mobile && <Button type="button" variant="ghost" size="icon-lg" className="min-h-11 min-w-11" aria-label="Close navigation" onClick={onSelect}><IconX /></Button>}</div>
    <Separator />
    <nav id={mobile ? 'mobile-dashboard-navigation' : 'dashboard-navigation'} className="flex flex-col gap-1" aria-label="Dashboard sections">
      {sections.map(([key, label, NavigationIcon, open]) => <Button key={key} type="button" variant={section === key ? 'secondary' : 'ghost'} size="lg" className="min-h-11 w-full justify-start" aria-label={key === 'nodes' ? `Nodes ${total}` : label} aria-current={section === key ? 'page' : undefined} onClick={() => { open(); onSelect() }}><NavigationIcon data-icon="inline-start" /><span className="truncate">{label}</span>{key === 'nodes' && <Badge variant="secondary" className="ml-auto">{total}</Badge>}</Button>)}
    </nav>
    <div className="mt-auto flex flex-col gap-3"><Separator /><small className="text-muted-foreground">{version}</small><Button variant="ghost" size="lg" className="min-h-11 justify-start" type="button" onClick={onLogout}><IconLogout data-icon="inline-start" />Sign out</Button></div>
  </>
}

function Overview({ status, performance, nodeTotal, nodesByTag, csrfToken, onRefresh, onUnauthorized, onOpenNodes }) {
  const healthy = status.observatory?.healthy || 0
  const total = status.observatory?.total || nodeTotal
  const healthText = total ? `${healthy}/${total} healthy` : 'No node data'
  const effective = nodesByTag.get(status.balancer?.effective)
  const ready = status.xray?.running && status.xray?.apiReachable && status.xkeen?.running
  return <div className="section-stack">
    <section className="active-node-strip" aria-label="Active node"><span className={ready ? 'good-text' : 'warning'}><span className={`status-dot ${ready ? 'up' : 'down'}`}></span>{ready ? 'Runtime ready' : 'Runtime unavailable'}</span>{effective ? <NodeName node={effective} /> : <strong>No current target</strong>}<span>{status.selection?.manualOverride ? 'Manual override' : 'Automatic selection'}</span><span>{formatAdaptiveLatency(effective?.latencyMs)}</span><span>{Array.from(nodesByTag.values()).filter((node) => node.enabled).length} enabled</span><button type="button" onClick={onOpenNodes}>Manage nodes</button></section>
    <NativeXkeenStatus facts={status.native} onOpenNodes={onOpenNodes} />
    <section className="hero-grid">
      <HealthCard label="Xray" ok={status.xray?.running && status.xray?.apiReachable} detail={status.xray?.apiReachable ? 'API reachable' : 'Degraded'} />
      <HealthCard label="Probe" ok={status.xray?.probeReachable} detail={status.xray?.probeReachable ? '127.0.0.1:10808' : 'Unavailable'} />
      <HealthCard label="Observatory" ok={status.observatory?.apiReachable} detail={healthText} />
      <HealthCard label="XKeen" ok={status.xkeen?.running} detail={status.xkeen?.running ? 'Running' : 'Not detected'} />
    </section>
    <section className="selection-summary-strip" aria-label="Selection status"><div><small>Mode</small><strong>{status.selection?.manualOverride ? 'Manual override' : 'Automatic'}</strong></div><div><small>State</small><strong>{selectionStateLabel(status.selection?.state)}</strong></div><div><small>Adaptive check</small><strong>{adaptiveStateLabel(performance?.adaptive)}</strong></div></section>
    <Disclosure title="Selection details" attention={performance?.adaptive?.state === 'running' || performance?.adaptive?.state === 'cleanup-pending'}>
    <section className="selection-grid">
      <SelectionCard label="Native leastPing" node={nodesByTag.get(status.balancer?.nativeSelected)} tone="blue" />
      <SelectionCard label="Manual override" node={nodesByTag.get(status.selection?.manualOverride)} tone="amber" emptyText="Automatic selection" />
      <SelectionCard label="Effective" node={nodesByTag.get(status.balancer?.effective)} tone="green" />
    </section>
    <AutomaticQualityOverview status={status} performance={performance} nodesByTag={nodesByTag} />
    <section className="panel setup-banner"><div><span className="panel-label">Panel readiness</span><strong>{status.setup?.runtime || 'setup'} · credential {status.setup?.credential || 'unknown'}</strong><small>XKeen {status.setup?.xkeen || 'missing'} · Xray {status.setup?.xray || 'missing'} · configuration {status.setup?.configuration || 'missing'}</small></div></section>
    </Disclosure>
    <section className="overview-node-list"><div className="workspace-heading"><h2>Available nodes <span className="count">{nodeTotal}</span></h2><button type="button" className="inline-link" onClick={onOpenNodes}>Open all nodes</button></div><div className="table-wrap"><table><thead><tr><th>Node</th><th>Address</th><th>Health</th><th>Latency</th><th>Role</th></tr></thead><tbody>{Array.from(nodesByTag.values()).slice(0, 5).map((node) => <tr key={node.id || node.tag}><td><NodeName node={node} /></td><td><code className="address">{node.address || '—'}</code></td><td>{node.alive ? 'Alive' : node.enabled ? 'No data' : 'Disabled'}</td><td>{formatAdaptiveLatency(node.latencyMs)}</td><td><NodeBadges node={node} /></td></tr>)}</tbody></table></div></section>
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

function NodeWorkspace({ nodes, subscriptions, performance, manualOverride, benchmarkRunning, csrf, onRefresh, onPerformanceRefresh, viewState, onViewStateChange, lifecycleBlocked, manualLifecycleBlocked, selectionAvailable }) {
  const [profiles, setProfiles] = useState('')
  const [subscriptionUrl, setSubscriptionUrl] = useState('')
  const [subscriptionName, setSubscriptionName] = useState('')
  const [subscriptionID, setSubscriptionID] = useState('')
  const [replacement, setReplacement] = useState('')
  const [editingID, setEditingID] = useState('')
  const [selectedIDs, setSelectedIDs] = useState(() => new Set())
  const [composer, setComposer] = useState('')
  const [preview, setPreview] = useState(null)
  const previewTrigger = useRef(null)
  const [notice, setNotice] = useState(null)
  const [busy, setBusy] = useState(false)
  const [manualRequestBusy, setManualRequestBusy] = useState(false)
  const manualStatus = performance?.manual || { state: 'idle', phase: 'done', plannedStages: 11, bytesPlanned: 48 * 1024 * 1024, completedStages: 0, bytesTransferred: 0 }
  const manualRunning = manualStatus.state === 'running'
  const adaptiveStatus = performance?.adaptive || { state: 'waiting' }
  const adaptiveRunning = adaptiveStatus.state === 'running'
  const { query, statusFilter, roleFilter, sourceFilter, subscriptionFilter, countryFilter, sort, page } = viewState

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
      if (subscriptionFilter !== 'all' && JSON.stringify([node.sourceType === 'subscription', node.subscriptionName || '']) !== subscriptionFilter) return false
      if (countryFilter !== 'all' && node.countryCode !== countryFilter) return false
      return true
    })
  }, [nodes, query, statusFilter, roleFilter, sourceFilter, subscriptionFilter, countryFilter])
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
  const subscriptionOptions = useMemo(() => [...new Set(nodes.filter((node) => node.sourceType === 'subscription').map((node) => node.subscriptionName || ''))].sort(), [nodes])
  const statusCounts = useMemo(() => ({
    all: nodes.length,
    alive: nodes.filter((node) => node.alive).length,
    unhealthy: nodes.filter((node) => node.enabled && !node.alive).length,
    disabled: nodes.filter((node) => !node.enabled).length,
    stale: nodes.filter((node) => node.stale || node.missing).length,
  }), [nodes])
  const filtersActive = Boolean(query.trim()) || statusFilter !== 'all' || roleFilter !== 'all' || sourceFilter !== 'all' || subscriptionFilter !== 'all' || countryFilter !== 'all'

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
    previewTrigger.current = document.activeElement
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
      const result = await api('/api/v1/node-changes/apply', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify({ previewToken: preview.previewToken, acceptMissing: preview.operation === 'subscription-refresh' ? false : Boolean(preview.requiresAcceptance) }) })
      const arrayOrNull = (value) => value === null || Array.isArray(value)
      if (result.operation !== preview.operation || !Object.hasOwn(result, 'changes') || !arrayOrNull(result.changes) || !Object.hasOwn(result, 'nodes') || !arrayOrNull(result.nodes)) throw new Error('Invalid Apply response')
      setPreview(null)
      setReplacement('')
      setEditingID('')
      closeComposer()
      await onRefresh()
      setNotice({ tone: 'success', message: 'Change applied; the full Xray candidate and active inventory were validated.' })
    } catch (cause) {
      // An interrupted or malformed reply cannot prove that Apply did not run.
      // Consume the preview in the UI as well; never offer replay of its token.
      setPreview(null)
      setNotice({ tone: 'error', message: !cause.status || cause.status >= 500 ? 'The change outcome could not be confirmed. Refresh the current state before making another change.' : cause.message })
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

  const openEditor = (node = selectedNode) => {
    if (!node || busy || lifecycleBlocked) return
    setSelectedIDs(new Set([node.id]))
    setEditingID(editingID === node.id ? '' : node.id)
    setReplacement('')
  }

  const setManualOverride = async (target) => {
    if (lifecycleBlocked || !selectionAvailable) return
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

  return <section className="nodes-workspace flex min-w-0 flex-col gap-3 bg-background font-sans text-foreground">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h1 className="flex items-center gap-2 text-xl font-semibold">Nodes <Badge variant="secondary">{nodes.length}</Badge></h1>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" aria-label="Add VLESS profiles" disabled={lifecycleBlocked} onClick={() => composer === 'profiles' ? closeComposer() : setComposer('profiles')}><IconPlus data-icon="inline-start" />Add profiles</Button>
        <Button type="button" variant="outline" disabled={lifecycleBlocked} onClick={() => composer === 'subscription' ? closeComposer() : startNewSubscription()}><IconLink data-icon="inline-start" />Add subscription</Button>
        <NodeActionButton icon="refresh" label="Refresh dashboard" onClick={onRefresh} />
      </div>
    </div>

    {notice && <Alert variant={notice.tone === 'error' ? 'destructive' : 'default'} role="status"><AlertDescription>{notice.message}</AlertDescription></Alert>}

    {composer === 'profiles' && <Card className="composer"><CardHeader><CardTitle>Import VLESS + REALITY links</CardTitle></CardHeader><CardContent><form onSubmit={(event) => { event.preventDefault(); requestPreview('/api/v1/nodes/import/preview', { profiles }) }}>
      <FieldGroup><Field><FieldLabel htmlFor="import-profiles">VLESS profiles</FieldLabel>
      <Textarea id="import-profiles" value={profiles} onChange={(event) => setProfiles(event.target.value)} placeholder="Paste one or more vless:// links" autoFocus /></Field>
      <div className="flex flex-wrap justify-end gap-2"><Button variant="outline" type="button" onClick={closeComposer}>Cancel</Button><Button type="submit" disabled={busy || lifecycleBlocked || !profiles.trim()}>Preview add</Button></div>
    </FieldGroup></form></CardContent></Card>}

    {composer === 'subscription' && <Card className="composer"><CardHeader><CardTitle>{subscriptionID ? 'Update subscription' : 'New subscription'}</CardTitle></CardHeader><CardContent><form onSubmit={(event) => { event.preventDefault(); requestPreview('/api/v1/subscriptions/refresh/preview', { ...(subscriptionID ? { subscriptionId: subscriptionID } : {}), name: subscriptionName, url: subscriptionUrl }) }}>
      <FieldGroup>
      <Field><FieldLabel htmlFor="subscription-name">Display name</FieldLabel>
      <Input id="subscription-name" value={subscriptionName} onChange={(event) => setSubscriptionName(event.target.value)} placeholder="Home provider" /></Field>
      <Field><FieldLabel htmlFor="subscription-url">Subscription URL</FieldLabel>
      <Input id="subscription-url" type="url" value={subscriptionUrl} onChange={(event) => setSubscriptionUrl(event.target.value)} placeholder={subscriptionID ? 'Leave blank to keep the saved URL' : 'https://…'} autoComplete="off" spellCheck={false} /></Field>
      <div className="flex flex-wrap justify-end gap-2"><Button variant="outline" type="button" onClick={closeComposer}>Cancel</Button><Button type="submit" disabled={busy || lifecycleBlocked || (!subscriptionID && !subscriptionUrl.trim())}>{subscriptionID ? 'Preview update' : 'Preview subscription'}</Button></div>
    </FieldGroup></form></CardContent></Card>}

    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Field className="max-w-md"><FieldLabel htmlFor="node-search" className="sr-only">Search nodes</FieldLabel><Input id="node-search" value={query} onChange={(event) => onViewStateChange((current) => ({ ...current, query: event.target.value, page: 1 }))} placeholder="Search name, address, or source" aria-label="Search nodes" /></Field>
        <span className="text-sm text-muted-foreground">{filtered.length} / {nodes.length} · <span data-testid="selected-count" aria-live="polite">{selectedIDs.size} selected</span></span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup variant="outline" className="flex-wrap" aria-label="Health filters" value={[statusFilter]} onValueChange={(values) => { if (values.length) chooseFilter('statusFilter', values[0]) }}>
          {[['all', 'All'], ['alive', 'Alive'], ['unhealthy', 'Not alive'], ['disabled', 'Disabled'], ['stale', 'Stale']].map(([value, label]) => <ToggleGroupItem key={value} value={value}>{label} <span>{statusCounts[value]}</span></ToggleGroupItem>)}
        </ToggleGroup>
        <ToggleGroup variant="outline" className="flex-wrap" aria-label="Role filters" value={[roleFilter]} onValueChange={(values) => { if (values.length) chooseFilter('roleFilter', values[0]) }}>
          {[['all', 'Any role'], ['native', 'Native'], ['override', 'Override'], ['effective', 'Effective'], ['none', 'No role']].map(([value, label]) => <ToggleGroupItem key={value} value={value}>{label}</ToggleGroupItem>)}
        </ToggleGroup>
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Source</FieldLabel><NativeSelect aria-label="Filter by source" value={sourceFilter} onChange={(event) => chooseFilter('sourceFilter', event.target.value)}><NativeSelectOption value="all">All</NativeSelectOption>{sourceOptions.map((source) => <NativeSelectOption value={source} key={source}>{source}</NativeSelectOption>)}</NativeSelect></Field>
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Subscription</FieldLabel><NativeSelect aria-label="Filter by subscription" value={subscriptionFilter} onChange={(event) => chooseFilter('subscriptionFilter', event.target.value)}><NativeSelectOption value="all">All</NativeSelectOption>{subscriptionOptions.map((name) => <NativeSelectOption value={JSON.stringify([true, name])} key={name}>{name || 'Unnamed subscription'}</NativeSelectOption>)}</NativeSelect></Field>
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Country</FieldLabel><NativeSelect aria-label="Filter by country" value={countryFilter} onChange={(event) => chooseFilter('countryFilter', event.target.value)}><NativeSelectOption value="all">All</NativeSelectOption>{countryOptions.map((country) => <NativeSelectOption value={country} key={country}>{country}</NativeSelectOption>)}</NativeSelect></Field>
        {filtersActive && <Button variant="ghost" type="button" onClick={clearFilters}>Clear</Button>}
      </div>
    </div>

    <div className="flex flex-wrap items-center gap-3" role="toolbar" aria-label="Selected node actions">
      <div className="flex items-center gap-1"><NodeActionButton icon="select" label={`Select all ${filtered.length} filtered`} onClick={toggleAllFiltered} disabled={busy || lifecycleBlocked || !filtered.length || allFilteredSelected} /><NodeActionButton icon="close" label="Clear selection" onClick={clearSelection} disabled={busy || lifecycleBlocked || !selectedIDs.size} /></div>
      <Separator orientation="vertical" className="h-6" /><div className="flex flex-wrap items-center gap-1">
        <NodeActionButton icon="target" label={selectedManual ? 'Clear manual override' : 'Set manual override'} active={Boolean(selectedManual)} onClick={() => setManualOverride(selectedManual ? '' : (selectedNode.outboundTag || selectedNode.tag))} disabled={busy || lifecycleBlocked || !selectionAvailable || !selectedNode || (!selectedManual && !selectedNode.enabled)} />
        <NodeActionButton icon="gauge" label={manualRequestBusy ? 'Starting speed test…' : 'Full speed test'} onClick={runManualNode} disabled={busy || manualRequestBusy || manualLifecycleBlocked || benchmarkRunning || manualRunning || adaptiveRunning || selectedNodes.length !== 1 || !selectedNode?.enabled} />
        <NodeActionButton icon="edit" label="Edit / replace profile" onClick={() => openEditor()} disabled={busy || lifecycleBlocked || selectedNodes.length !== 1} />
        <NodeActionButton icon="enable" label="Enable" onClick={() => requestPreview('/api/v1/nodes/batch/state/preview', { nodeIds: selectedNodeIDs, enabled: true })} disabled={busy || lifecycleBlocked || !selectedNodes.length || selectedNodes.every((node) => node.enabled)} />
        <NodeActionButton icon="disable" label="Disable" onClick={() => requestPreview('/api/v1/nodes/batch/state/preview', { nodeIds: selectedNodeIDs, enabled: false })} disabled={busy || lifecycleBlocked || !selectedNodes.length || selectedNodes.every((node) => !node.enabled)} />
        <NodeActionButton icon="trash" label="Delete" tone="danger" onClick={() => requestPreview('/api/v1/nodes/batch/remove/preview', { nodeIds: selectedNodeIDs })} disabled={busy || lifecycleBlocked || !selectedNodes.length} />
      </div>
    </div>

    {manualStatus.state !== 'idle' && <div className="legacy-workspace"><ManualPerformanceCard status={manualStatus} node={nodes.find((node) => node.id === manualStatus.targetNodeId)} /></div>}

    {selectedNode && editingID === selectedNode.id && <Card className="selection-editor"><CardHeader><CardTitle>Replace profile</CardTitle><CardDescription><NodeName node={selectedNode} /><span className="block">Stable tag: <code>{selectedNode.outboundTag || selectedNode.tag}</code></span></CardDescription></CardHeader><CardContent><FieldGroup><Field>
      <FieldLabel htmlFor="replacement-profile">Replacement VLESS profile</FieldLabel><Input id="replacement-profile" aria-label="Replacement VLESS profile" value={replacement} onChange={(event) => setReplacement(event.target.value)} placeholder="Replacement vless:// profile" type="password" autoComplete="off" autoFocus /></Field>
      <div className="flex flex-wrap justify-end gap-2"><Button variant="outline" type="button" onClick={() => { setEditingID(''); setReplacement('') }}>Cancel</Button><Button type="button" disabled={busy || lifecycleBlocked || !replacement.trim()} onClick={() => requestPreview('/api/v1/nodes/replace/preview', { id: selectedNode.id, profile: replacement })}>Preview replacement</Button></div>
    </FieldGroup></CardContent></Card>}

    <Table className="nodes-table"><TableHeader><TableRow><TableHead className="selection-column"><SelectionCheckbox label="Select all filtered nodes" checked={allFilteredSelected} indeterminate={selectedFilteredCount > 0 && !allFilteredSelected} onChange={toggleAllFiltered} /></TableHead><SortHeader label="Name" sortKey="name" sort={sort} onSort={changeSort} /><SortHeader label="Address" sortKey="address" sort={sort} onSort={changeSort} /><SortHeader label="Health" sortKey="health" sort={sort} onSort={changeSort} /><SortHeader label="Latency" sortKey="latency" sort={sort} onSort={changeSort} /><SortHeader label="Role" sortKey="role" sort={sort} onSort={changeSort} /><SortHeader label="Source" sortKey="source" sort={sort} onSort={changeSort} /><SortHeader label="Subscription" sortKey="subscription" sort={sort} onSort={changeSort} /></TableRow></TableHeader><TableBody>
      {visibleNodes.map((node) => <NodeRows key={node.id || node.tag} node={node} selected={selectedIDs.has(node.id)} onToggle={() => toggleSelection(node.id)} />)}
      {!visibleNodes.length && <TableRow><TableCell colSpan="8" className="empty">No nodes match this view.</TableCell></TableRow>}
    </TableBody></Table>

    <Pagination page={page} totalPages={totalPages} onPage={(value) => onViewStateChange((current) => ({ ...current, page: value }))} />
    {!!subscriptions.length && <Disclosure title={`Subscriptions · ${subscriptions.length}`} className="subscriptions-disclosure"><div className="grid gap-3 md:grid-cols-2">
      {subscriptions.map((subscription) => { const enabled = subscription.enabled !== false; const name = subscription.name || 'Unnamed subscription'; const autoStatus = subscription.autoRefresh; const autoState = autoRefreshState(autoStatus); return <Card size="sm" key={subscription.id} data-testid={`subscription-card-${subscription.id}`} data-enabled={enabled}><CardContent className="flex flex-wrap items-center justify-between gap-3">
        <div><strong>{name}</strong><small>{enabled ? 'Enabled' : 'Disabled'} · {subscription.nodeCount} nodes{subscription.staleCount ? ` · ${subscription.staleCount} stale` : ''}</small>{autoStatus && <div className={`subscription-auto-refresh ${autoState}`} data-testid={`subscription-auto-refresh-${subscription.id}`}><span>{autoRefreshStateLabels[autoState]}</span><small>{autoRefreshSummary(autoStatus)}</small></div>}</div>
        <div className="flex flex-wrap gap-1">
          <NodeActionButton icon="refresh" label={`Refresh ${name}`} disabled={busy || lifecycleBlocked} onClick={() => requestPreview('/api/v1/subscriptions/refresh/preview', { subscriptionId: subscription.id })} />
          <NodeActionButton icon="edit" label={`Edit ${name}`} disabled={busy} onClick={() => openSubscriptionEditor(subscription)} />
          <NodeActionButton icon="power" label={enabled ? `Disable ${name}` : `Enable ${name}`} active={!enabled} disabled={busy || lifecycleBlocked} onClick={() => requestPreview('/api/v1/subscriptions/state/preview', { subscriptionId: subscription.id, enabled: !enabled })} />
          <NodeActionButton icon="trash" label={`Remove ${name}`} tone="danger" disabled={busy || lifecycleBlocked} onClick={() => requestPreview('/api/v1/subscriptions/remove/preview', { subscriptionId: subscription.id })} />
        </div>
      </CardContent></Card> })}
    </div></Disclosure>}

    <Disclosure title={`Automatic quality · ${adaptiveStateLabel(adaptiveStatus)}`} attention={['running', 'failed', 'cleanup-pending'].includes(adaptiveStatus.state)}><div className="legacy-workspace"><AdaptiveQualityCard status={adaptiveStatus} nodes={nodes} /></div></Disclosure>
    {preview && <PreviewDialog preview={preview} nodes={nodes} manualOverride={manualOverride} busy={busy || lifecycleBlocked} onCancel={cancelPreview} onApply={applyPreview} returnFocus={previewTrigger} />}
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
    <TableRow data-state={selected ? 'selected' : undefined} tabIndex={0} aria-selected={selected} onClick={(event) => { if (!event.target.closest('input, label, button, a, [role=checkbox]')) onToggle() }} onKeyDown={(event) => { if (event.target === event.currentTarget && [' ', 'Enter'].includes(event.key)) { event.preventDefault(); onToggle() } }}>
      <TableCell className="selection-column"><SelectionCheckbox label={`Select ${visibleNodeName(node)}`} checked={selected} onChange={onToggle} /></TableCell>
      <TableCell><NodeName node={node} />{node.stale && <Badge variant="secondary">stale</Badge>}</TableCell>
      <TableCell data-label="Address"><code className="address">{node.address || '—'}</code></TableCell>
      <TableCell data-label="Health"><Badge variant={node.alive ? 'secondary' : 'outline'}>{health}</Badge></TableCell>
      <TableCell data-label="Latency">{formatAdaptiveLatency(node.latencyMs)}</TableCell>
      <TableCell data-label="Role"><NodeBadges node={node} /></TableCell>
      <TableCell data-label="Source"><span>{node.sourceType || 'legacy'}</span></TableCell>
      <TableCell data-label="Subscription">{node.sourceType === 'subscription' ? node.subscriptionName || 'Unnamed subscription' : '—'}</TableCell>
    </TableRow>
  </>
}

function PreviewDialog({ preview, nodes, manualOverride, busy, onCancel, onApply, returnFocus }) {
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
  return <Modal label="Preview node change" busy={busy} onCancel={onCancel} returnFocus={returnFocus}>
      <div className="flex items-start justify-between gap-4"><div><span className="text-sm text-muted-foreground">Preview · {preview.operation}</span><h3>{preview.noop ? 'No persistent change' : `${preview.changes?.length || 0} node changes`}</h3></div><Button variant="ghost" size="icon" aria-label="Close preview" onClick={onCancel} disabled={busy}><IconX /></Button></div>
      <ul aria-label="Node changes" className="flex max-h-72 flex-col overflow-y-auto">
        {changes.map((change) => <li className="flex flex-wrap justify-between gap-2 border-b py-2" key={`${change.action}-${change.id}`}><strong>{change.name}</strong><span>{change.before} → {change.after}</span></li>)}
      </ul>
      {preview.noop && <p className="text-sm text-muted-foreground">The fetched or requested state matches the current registry.</p>}
      {subscriptionRemovalCount > 0 && <Alert><AlertDescription>Provider snapshot removes {subscriptionRemovalCount} {subscriptionRemovalCount === 1 ? 'node that is' : 'nodes that are'} no longer present upstream.</AlertDescription></Alert>}
      {effectiveChanged && <Alert><AlertDescription>The currently effective node changes in this preview. Active proxy traffic will be reselected after Apply.</AlertDescription></Alert>}
      {manualChanged && <Alert><AlertDescription>The current manual-override node changes in this preview. The supervisor owns subsequent reconciliation; this batch mutation does not write override state.</AlertDescription></Alert>}
      {manualSubscriptionRemovals && <Alert><AlertDescription>A removed subscription-owned node may return on a later subscription refresh while it remains upstream.</AlertDescription></Alert>}
      {preview.effectiveImpact && !effectiveChanged && <Alert><AlertDescription>This operation will {preview.effectiveImpact} the currently effective node. Active proxy traffic will be reselected after Apply.</AlertDescription></Alert>}
      <div className="flex flex-wrap justify-end gap-2"><Button variant="outline" type="button" onClick={onCancel} disabled={busy}>Cancel</Button><Button type="button" onClick={onApply} disabled={busy}>{busy ? 'Applying…' : (preview.noop ? 'Confirm no-op' : 'Apply and validate')}</Button></div>
  </Modal>
}

function Pagination({ page, totalPages, onPage }) {
  if (totalPages <= 1) return null
  const pages = [...new Set([1, totalPages, ...Array.from({ length: 5 }, (_, index) => page + index - 2)])].filter((value) => value >= 1 && value <= totalPages).sort((a, b) => a - b)
  return <nav className="flex flex-wrap items-center justify-end gap-1" aria-label="Node pages">
    <span className="mr-auto text-sm text-muted-foreground">Page {page} of {totalPages}</span>
    <NodeActionButton icon="left" label="Previous page" disabled={page <= 1} onClick={() => onPage(page - 1)} />
    <div className="flex flex-wrap gap-1">{pages.map((value, index) => <span key={value}>{index > 0 && value - pages[index - 1] > 1 && <span className="pagination-gap" aria-hidden="true">…</span>}<Button type="button" size="icon" variant={value === page ? 'secondary' : 'ghost'} aria-current={value === page ? 'page' : undefined} onClick={() => onPage(value)}>{value}</Button></span>)}</div>
    <NodeActionButton icon="right" label="Next page" disabled={page >= totalPages} onClick={() => onPage(page + 1)} />
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
      <div className="backup-card-heading"><div><h2>Download current settings</h2><p className="muted">Safe export contains appliance policy and no node secrets.</p></div><button type="button" onClick={exportSafe} disabled={safeBusy}>{safeBusy ? 'Preparing…' : 'Download safe backup'}</button></div>
      <Disclosure title="Encrypted backup · include node secrets"><form className="secret-export" onSubmit={exportSecret}>
        <p className="muted">Contains node secrets. Keep this download private.</p>
        <label>Current panel password<input type="password" autoComplete="current-password" value={secretForm.currentPassword} onChange={(event) => setSecretForm((current) => ({ ...current, currentPassword: event.target.value }))} /></label>
        <label>Encryption passphrase<input type="password" autoComplete="new-password" value={secretForm.passphrase} onChange={(event) => setSecretForm((current) => ({ ...current, passphrase: event.target.value }))} /></label>
        <label>Confirm passphrase<input type="password" autoComplete="new-password" value={secretForm.confirmation} onChange={(event) => setSecretForm((current) => ({ ...current, confirmation: event.target.value }))} /></label>
        <button type="submit" disabled={secretBusy}>{secretBusy ? 'Preparing…' : 'Download encrypted backup'}</button>
      </form></Disclosure>
    </section>

    <section className="panel backup-card">
      <div><h2>Import a local backup</h2><p className="muted">Choose one JSON bundle. The server enforces the 10 MiB request and 9 MiB bundle limits.</p></div>
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

function SortHeader({ label, sortKey, sort, onSort }) {
  const active = sort.key === sortKey
  const direction = active ? sort.direction : 'none'
  const SortIcon = active ? sort.direction === 'asc' ? IconArrowUp : IconArrowDown : IconArrowsSort
  return <TableHead aria-sort={direction === 'none' ? 'none' : direction === 'asc' ? 'ascending' : 'descending'}><Button variant="ghost" size="sm" className="sort-button" type="button" onClick={() => onSort(sortKey)}>{label}<SortIcon data-icon="inline-end" aria-hidden="true" /></Button></TableHead>
}

function SelectionCheckbox({ checked, indeterminate = false, label, onChange }) {
  return <label className="selection-checkbox"><Checkbox aria-label={label} checked={checked} indeterminate={indeterminate} onCheckedChange={onChange} /></label>
}

function NodeActionButton({ icon, label, tone = '', active = false, ...props }) {
  const ActionIcon = { select: IconSquareCheck, enable: IconPlayerPlay, disable: IconPlayerPause, refresh: IconRefresh, edit: IconPencil, power: IconPower, trash: IconTrash, close: IconX, left: IconChevronLeft, right: IconChevronRight, gauge: IconGauge, target: IconFocus2 }[icon]
  return <Button type="button" variant={tone === 'danger' ? 'destructive' : active ? 'secondary' : 'outline'} size="icon" aria-label={label} title={label} {...props}><ActionIcon data-icon="inline-start" /></Button>
}

function NodeName({ node }) {
  const flag = COUNTRY_FLAGS[node?.countryCode]
  return <strong className="display-name">{flag && <img className="country-flag" src={flag} alt="" aria-hidden="true" />}<span>{visibleNodeName(node)}</span></strong>
}

function IconButton({ icon, label, tone = '', active = false, ...props }) {
  return <button type="button" className={`icon-button ${tone} ${active ? 'active' : ''}`} aria-label={label} title={label} data-tooltip={label} {...props}><Icon name={icon} /></button>
}

function Icon({ name }) {
  const icons = { select: IconSquareCheck, enable: IconPlayerPlay, disable: IconPlayerPause, plus: IconPlus, link: IconLink, refresh: IconRefresh, edit: IconPencil, power: IconPower, trash: IconTrash, close: IconX, left: IconChevronLeft, right: IconChevronRight, search: IconSearch, gauge: IconGauge, target: IconFocus2 }
  const Component = icons[name]
  return Component ? <Component size={16} aria-hidden="true" focusable="false" /> : null
}

function Shell({ children }) { return <main className="min-h-svh min-w-80 bg-background text-foreground">{children}</main> }
function Notice({ message, tone = 'error' }) { return <div className={`notice ${tone}`} role="status">{message}</div> }
function HealthCard({ label, ok, detail }) { return <div className="health-card"><div className={`health-icon ${ok ? 'ok' : 'bad'}`}>{ok ? '✓' : '!'}</div><div><span className="panel-label">{label}</span><strong>{ok ? 'Healthy' : 'Degraded'}</strong><small>{detail}</small></div></div> }
function SelectionCard({ label, node, tone, emptyText = 'No current target' }) { return <div className={`panel selection-card ${tone}`}><span className="panel-label">{label}</span>{node ? <NodeName node={node} /> : <strong>{emptyText}</strong>}<small>{node?.address || (node ? 'No address' : '')}</small></div> }
function NodeBadges({ node }) { return <div className="badges">{node.isNativeSelected && <span className="chip blue">native</span>}{node.isOverride && <span className="chip amber">override</span>}{node.isEffective && <span className="chip green">effective</span>}</div> }
function formatUptime(seconds) { if (!seconds) return '—'; const hours = Math.floor(seconds / 3600); const minutes = Math.floor((seconds % 3600) / 60); return `${hours}h ${minutes}m` }

createRoot(document.getElementById('root')).render(<StrictMode><App /></StrictMode>)
