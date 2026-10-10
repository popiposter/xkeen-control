import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { StatusBadge, ActionHint, formatRate } from './status-ui'
import { TooltipProvider } from '@/components/ui/tooltip'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuCheckboxItem } from '@/components/ui/dropdown-menu'
import { CSPProvider } from '@base-ui/react/csp-provider'
import { ThemeControl } from './theme-control'
import './theme-init'
import { StrictMode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { createDashboardReader } from './dashboard-reader.js'
import { lifecycleBlocked as isLifecycleBlocked } from './lifecycle.js'
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
import { NativeQualitySection } from './native-quality.jsx'
import { NativeXkeenStatus, NativeXkeenSection } from './native-xkeen.jsx'
import { NativeConfigSection } from './native-config.jsx'
import { SplitDNSSection } from './split-dns.jsx'
import { NativeTransferSection } from './native-transfer.jsx'
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
import flagRO from 'flag-icons/flags/4x3/ro.svg'
import flagRU from 'flag-icons/flags/4x3/ru.svg'
import flagSE from 'flag-icons/flags/4x3/se.svg'
import flagSG from 'flag-icons/flags/4x3/sg.svg'
import flagTH from 'flag-icons/flags/4x3/th.svg'
import flagTR from 'flag-icons/flags/4x3/tr.svg'
import flagUS from 'flag-icons/flags/4x3/us.svg'
import flagUZ from 'flag-icons/flags/4x3/uz.svg'

const PAGE_SIZE = 25
const FLAG_PREFIX = /^[\u{1F1E6}-\u{1F1FF}]{2}\s*/u
const COUNTRY_FLAGS = {
  AE: flagAE, AM: flagAM, AT: flagAT, BG: flagBG, BY: flagBY, CA: flagCA, CZ: flagCZ,
  DE: flagDE, EE: flagEE, ES: flagES, FI: flagFI, FR: flagFR, GB: flagGB,
  IL: flagIL, IN: flagIN, KZ: flagKZ, LV: flagLV, NL: flagNL, PL: flagPL,
  RO: flagRO, RU: flagRU, SE: flagSE, SG: flagSG, TH: flagTH, TR: flagTR, US: flagUS, UZ: flagUZ,
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
	  error.diagnostic = body?.diagnostic
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
  'resource-pressure': 'Stopped because the router is busy or memory is low. Partial measurements are retained.',
  'resource-telemetry-unavailable': 'Router load could not be checked safely. No further test traffic was sent.',
  'native-speed-conflict': 'A native periodic speed test is configured. Quiesce it before running another test.',
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

const formatAdaptiveLatency = (value) => {
  const numeric = Number(value)
  return value == null || !Number.isFinite(numeric) || numeric <= 0 || numeric >= 99999999 ? '—' : `${Math.round(numeric)} ms`
}
const formatAdaptiveRate = (value) => {
  const numeric = Number(value)
  return value == null || !Number.isFinite(numeric) || numeric <= 0 || numeric >= 99999999 ? '—' : `${((numeric * 8) / 1000000).toFixed(1)} Mbps`
}
const sortNodes = (nodes, key, direction, measurements = new Map()) => {
  const multiplier = direction === 'desc' ? -1 : 1
  const stringValue = (value) => String(value || '').toLocaleLowerCase()
  const valueFor = (node) => {
    switch (key) {
      case 'rank': return measurements.get(node.outboundTag || node.tag)?.rank ?? null
      case 'download': return measurements.get(node.outboundTag || node.tag)?.valid ? measurements.get(node.outboundTag || node.tag)?.downloadBps : null
      case 'upload': return measurements.get(node.outboundTag || node.tag)?.valid ? measurements.get(node.outboundTag || node.tag)?.uploadBps : null
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
    if (leftValue == null || rightValue == null) return leftValue == null && rightValue == null ? visibleNodeName(left).localeCompare(visibleNodeName(right)) : leftValue == null ? 1 : -1
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
      <CardHeader><CardTitle><h1>XKeen Control</h1></CardTitle><CardDescription>Sign in to manage your VPN. This browser stays signed in for up to 30 days, including panel restarts. Sign out on shared devices.</CardDescription></CardHeader>
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
  const [quality, setQuality] = useState(null)
  const measurements = useMemo(() => new Map((quality?.progress?.candidates || []).map((node) => [node.tag, { ...node, rank: quality?.ranking?.find((item) => item.tag === node.tag)?.rank }])), [quality])
  const [configVisited, setConfigVisited] = useState(false)
	const [configReadback, setConfigReadback] = useState(0)
  const [nativeConfigJob, setNativeConfigJob] = useState(null)
  const [configWorking, setConfigWorking] = useState(false)
  const qualityReadback = useCallback(() => { setConfigReadback((key) => key + 1); onRefresh() }, [onRefresh])
  const receiveNativeJob = useCallback((job) => setNativeConfigJob({ job, csrfToken: session.csrfToken }), [session.csrfToken])
  useEffect(() => setNativeConfigJob(null), [session.csrfToken])
  useEffect(() => { if (['components', 'routing', 'dns', 'configs'].includes(section)) setConfigVisited(true) }, [section])
  const [navigationOpen, setNavigationOpen] = useState(false)
  const navigationTrigger = useRef(null)
  const closeNavigation = useCallback(() => setNavigationOpen(false), [])
  const [nodeView, setNodeView] = useState(createNodeViewState)
  const registryNodes = nodes.nodes || []
  const nodesByTag = useMemo(() => new Map(registryNodes.map((node) => [node.outboundTag || node.tag, node])), [registryNodes])
  const performanceOwnerBusy = Boolean(performance?.manual?.state === 'running' || performance?.adaptive?.state === 'running')
  const systemPanelController = useSystemPanelController({ csrfToken: session.csrfToken, lifecycle: status.lifecycle, onUnauthorized, active: section === 'system' })
  const openComponents = useCallback(() => setSection('components'), [])
  const openRouting = useCallback(() => setSection('routing'), [])
  const openDNS = useCallback(() => setSection('dns'), [])
  const openBackup = useCallback(() => setSection('backup'), [])
  const lifecycleBlocked = isLifecycleBlocked(status.lifecycle)
  const manualLifecycleBlocked = lifecycleBlocked
  const manualRunning = performance?.manual?.state === 'running'
  const adaptiveRunning = performance?.adaptive?.state === 'running'
  const performancePolling = (section === 'overview' && adaptiveRunning)
    || (section === 'nodes' && (manualRunning || adaptiveRunning))
  const performancePollInFlight = useRef(false)

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
    ['performance', 'Performance', IconChartBar, () => setSection('performance')],
    ['configuration', 'Configuration', IconSitemap, openRouting],
    ['components', 'XKeen', IconCube, openComponents],
    ['system', 'System', IconSettings, () => setSection('system')],
  ]
  // Routing, DNS and all files are one editor; Backup lives in System.
  const navigationKey = { routing: 'configuration', dns: 'configuration', configs: 'configuration', backup: 'system' }[section] || section
  const pageTitle = sections.find(([key]) => key === navigationKey)?.[1]
  return <Shell>
    <a className="sr-only focus:not-sr-only focus:p-4" href="#workspace">Skip to workspace</a>
    <header className="flex items-center gap-3 border-b p-3 min-[761px]:hidden"><Button ref={navigationTrigger} type="button" variant="ghost" size="icon-lg" className="min-h-11 min-w-11" aria-label="Toggle navigation" aria-expanded={navigationOpen} aria-controls="mobile-dashboard-navigation" aria-haspopup="dialog" onClick={() => setNavigationOpen(!navigationOpen)}><IconMenu2 /></Button><strong>XKeen Control</strong></header>
    <aside className="fixed inset-y-0 left-0 hidden w-60 flex-col gap-4 border-r bg-sidebar p-4 min-[761px]:flex"><NavigationContent sections={sections} section={navigationKey} total={nodes.total || 0} version={status.controlPlane?.version || 'dev'} onSelect={closeNavigation} onLogout={onLogout} /></aside>
    <MobileNavigationDrawer returnFocus={navigationTrigger} open={navigationOpen} onClose={closeNavigation}><div className="flex min-h-full flex-col gap-4"><NavigationContent mobile sections={sections} section={navigationKey} total={nodes.total || 0} version={status.controlPlane?.version || 'dev'} onSelect={closeNavigation} onLogout={onLogout} /></div></MobileNavigationDrawer>
    <div id="workspace" className="min-h-svh min-w-0 min-[761px]:ml-60" tabIndex="-1"><div className="app-content"><div className="workspace">
    {section !== 'nodes' && <header className="page-heading"><h1>{pageTitle}</h1>{section === 'overview' && <Button variant="outline" type="button" onClick={onRefresh}><Icon name="refresh" />Refresh</Button>}</header>}
    {error && <Notice message={error} />}
    {navigationKey === 'configuration' && <Tabs value={section} onValueChange={setSection}><TabsList aria-label="Configuration views">{[['routing', 'Routing'], ['dns', 'DNS'], ['configs', 'All files']].map(([value, label]) => <TabsTrigger key={value} value={value} className="min-h-11 sm:min-h-0">{label}</TabsTrigger>)}</TabsList></Tabs>}
    {section === 'overview' && <Overview quality={quality} measurements={measurements} status={status} performance={performance} nodeTotal={nodes.total || 0} nodesByTag={nodesByTag} csrfToken={session.csrfToken} onRefresh={onRefresh} onUnauthorized={onUnauthorized} onOpenNodes={() => setSection('nodes')} />}
    {section === 'nodes' && <NodeWorkspace measurements={measurements} nodes={registryNodes} subscriptions={nodes.subscriptions || []} performance={performance} manualOverride={status.balancer?.override || ''} csrf={session.csrfToken} onRefresh={onRefresh} onPerformanceRefresh={onPerformanceRefresh} viewState={nodeView} onViewStateChange={setNodeView} lifecycleBlocked={lifecycleBlocked} manualLifecycleBlocked={manualLifecycleBlocked} selectionAvailable={Boolean(status.xray?.apiReachable && status.balancer?.effective)} />}
    {<div hidden={section !== 'performance'}><NativeQualitySection onStatusChange={setQuality} csrfToken={session.csrfToken} onUnauthorized={onUnauthorized} busy={performanceOwnerBusy || lifecycleBlocked} nodesByTag={nodesByTag} workingEdits={configWorking} onReadback={qualityReadback} onNativeJob={receiveNativeJob} onOpenConsole={openComponents} onInspectConfigs={openRouting} /></div>}
    {section === 'components' && <NativeXkeenSection facts={status.native} onRefresh={onRefresh} onOpenSystem={() => setSection('system')} csrfToken={session.csrfToken} onUnauthorized={onUnauthorized} jobNotification={nativeConfigJob?.csrfToken === session.csrfToken ? nativeConfigJob.job : null} />}
    {section === 'dns' && <SplitDNSSection csrfToken={session.csrfToken} onUnauthorized={onUnauthorized} readbackKey={configReadback} />}
    {(['components', 'routing', 'dns', 'configs'].includes(section) || configVisited) && <div hidden={!['routing', 'dns', 'configs'].includes(section)}><NativeConfigSection csrfToken={session.csrfToken} onUnauthorized={onUnauthorized} onWorkingChange={setConfigWorking} readbackKey={configReadback} scopeFile={section === 'dns' ? '02_dns.json' : section === 'routing' ? '05_routing.json' : ''} focusFile={section === 'dns' ? '02_dns.json' : section === 'routing' ? '05_routing.json' : ''} onOpenConsole={openComponents} onNativeJob={receiveNativeJob} /></div>}
    {navigationKey === 'system' && <SystemPanelSection controller={systemPanelController} status={status} initialPage={section === 'backup' ? 'backup' : undefined} backup={<NativeTransferSection csrfToken={session.csrfToken} api={api} download={download} onRefresh={onRefresh} onUnauthorized={onUnauthorized} onStaged={() => setConfigReadback((key) => key + 1)} onInspectConfigs={openRouting} />} />}
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
    <div className="mt-auto flex flex-col gap-3"><Separator /><ThemeControl /><small className="text-muted-foreground">{version}</small><Button variant="ghost" size="lg" className="min-h-11 justify-start" type="button" onClick={onLogout}><IconLogout data-icon="inline-start" />Sign out</Button></div>
  </>
}

// Native Observatory watches only the quality pool. A node without any
// Observatory record is outside it: not continuously monitored, which is not
// the same as a failed probe (REQ-011).
const nodeObserved = (node) => Boolean(node?.alive || node?.lastTry || node?.lastSeen || node?.lastError)
const nodeHealthLabel = (node) => !node.enabled ? 'Disabled' : node.alive ? 'Alive' : nodeObserved(node) ? 'Not alive' : 'Not monitored'
const nodeHealthTone = (node) => !node.enabled || (!node.alive && !nodeObserved(node)) ? 'muted' : node.alive ? 'success' : 'warning'
const notMonitoredTitle = 'Outside the active quality pool: not continuously monitored. Reviews probe it in rotation.'

function Overview({ quality, measurements, status, nodeTotal, nodesByTag, onOpenNodes }) {
  const effective = nodesByTag.get(status.balancer?.effective)
  const ready = status.xray?.running && status.xray?.apiReachable && status.xkeen?.running
  const enabled = Array.from(nodesByTag.values()).filter((node) => node.enabled).length
  const applied = Boolean(quality?.appliedRanking?.length)
  const provisional = Boolean(quality?.provisionalAt) && new Date(quality.provisionalAt).getFullYear() >= 2020
  const leaders = applied ? quality.appliedRanking : quality?.ranking || []
  return <div className="section-stack">
    <Card role="region" aria-label="Active node"><CardHeader><div className="flex flex-wrap items-center justify-between gap-3"><CardTitle>{effective ? <NodeName node={effective} /> : 'No current target'}</CardTitle><StatusBadge tone={ready ? 'success' : 'danger'}>{ready ? 'Runtime ready' : 'Runtime unavailable'}</StatusBadge></div><CardDescription>{status.balancer?.override ? 'Manual override' : 'Native automatic selection'} · {formatAdaptiveLatency(effective?.latencyMs)} · {enabled} enabled</CardDescription></CardHeader><CardContent><Button variant="outline" onClick={onOpenNodes}>Manage nodes</Button></CardContent></Card>
    <div role="list" aria-label="Runtime health" className="grid grid-cols-2 gap-2 xl:grid-cols-4">
      <HealthCard label="Xray" ok={status.xray?.running && status.xray?.apiReachable} detail={status.xray?.apiReachable ? 'API reachable' : 'Unavailable'} />
      <HealthCard label="Probe" ok={status.xray?.probeReachable} detail={status.xray?.probeReachable ? 'Probe reachable' : 'Unavailable'} />
      <HealthCard label="Observatory" ok={status.observatory?.apiReachable} detail={Number.isFinite(status.observatory?.observed) ? `${status.observatory?.healthy || 0}/${status.observatory.observed} pool nodes healthy` : `${status.observatory?.healthy || 0}/${status.observatory?.total || nodeTotal} healthy`} />
      <HealthCard label="XKeen" ok={status.xkeen?.running} detail={status.xkeen?.running ? 'Running' : 'Not detected'} />
    </div>
    <Disclosure title="Selection details"><div className="grid gap-4 sm:grid-cols-3">
      <SelectionCard label="Native selection" node={nodesByTag.get(status.balancer?.nativeSelected)} />
      <SelectionCard label="Manual override" node={nodesByTag.get(status.balancer?.override)} emptyText="Automatic selection" />
      <SelectionCard label="Effective" node={effective} />
    </div><p className="mt-4 text-sm text-muted-foreground">Xray selects a healthy node using the configured strategy. Compare throughput in Performance; saved recommendations apply through Routing.</p></Disclosure>
    {(!nodeTotal || status.native?.installation !== 'available') && <NativeXkeenStatus facts={status.native} onOpenNodes={onOpenNodes} />}
    <Card><CardHeader><div className="flex flex-wrap items-center justify-between gap-3"><CardTitle>{provisional ? 'Provisional pool' : applied ? 'Active pool leaders' : 'Measured candidates'}</CardTitle><Button variant="ghost" onClick={onOpenNodes}>Open all nodes</Button></div><CardDescription>{provisional ? 'No pool member was healthy, so availability recovery applied nodes that answered a latency probe. They are not ranked by speed; the next complete automatic review replaces this pool.' : applied ? 'Pool members in saved preference order from the last measured review. Xray chooses among them by live health and latency.' : 'Latest measured candidates; these recommendations have not been confirmed as applied weights.'} Speeds describe the latest comparison in this panel session.</CardDescription></CardHeader><CardContent>
      {!leaders.length ? <p className="py-4 text-muted-foreground">{quality?.state === 'running' ? 'Comparing the current pool…' : 'No completed comparison in this panel session. Run a comparison in Performance.'}</p> : <Table><TableHeader><TableRow><TableHead>{provisional ? 'Member' : 'Rank'}</TableHead><TableHead>Node</TableHead><TableHead>Health</TableHead><TableHead className="hidden sm:table-cell">Download</TableHead><TableHead className="hidden sm:table-cell">Upload</TableHead><TableHead className="hidden sm:table-cell">Role</TableHead></TableRow></TableHeader><TableBody>{leaders.filter((item) => nodesByTag.get(item.tag)?.enabled).map((item) => { const node = nodesByTag.get(item.tag); const sample = measurements.get(item.tag); return <TableRow key={item.tag}><TableCell>{provisional ? '—' : <StatusBadge>#{item.rank}</StatusBadge>}</TableCell><TableCell><NodeName node={node} /></TableCell><TableCell><StatusBadge tone={nodeHealthTone(node)}>{nodeHealthLabel(node)}</StatusBadge></TableCell><TableCell className="hidden sm:table-cell">{formatRate(sample?.downloadBps)}</TableCell><TableCell className="hidden sm:table-cell">{formatRate(sample?.uploadBps)}</TableCell><TableCell className="hidden sm:table-cell"><NodeBadges node={node} /></TableCell></TableRow> })}</TableBody></Table>}
    </CardContent></Card>
  </div>
}

function NodeWorkspace({ measurements, nodes, subscriptions, performance, manualOverride, csrf, onRefresh, onPerformanceRefresh, viewState, onViewStateChange, lifecycleBlocked, manualLifecycleBlocked, selectionAvailable }) {
  const [columns, setColumns] = useState(() => { try { const stored = JSON.parse(localStorage.getItem('xkeen.node-columns.v1')); if (stored && typeof stored === 'object' && !Array.isArray(stored)) return stored } catch {} return { address: false, health: true, latency: true, rank: true, download: true, upload: true, role: true, source: false, subscription: true } })
  const showColumn = (key) => columns[key] !== false
  const toggleColumn = (key, value) => setColumns((previous) => { const next = { ...previous, [key]: value }; try { localStorage.setItem('xkeen.node-columns.v1', JSON.stringify(next)) } catch {} return next })
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
      if (statusFilter === 'unhealthy' && (!node.enabled || node.alive || !nodeObserved(node))) return false
      if (statusFilter === 'unmonitored' && (!node.enabled || nodeObserved(node))) return false
      if (statusFilter === 'disabled' && node.enabled) return false
      if (statusFilter === 'stale' && !node.stale && !node.missing) return false
      if (!matchesNodeRole(node, roleFilter)) return false
      if (sourceFilter !== 'all' && node.sourceType !== sourceFilter) return false
      if (subscriptionFilter !== 'all' && JSON.stringify([node.sourceType === 'subscription', node.subscriptionName || '']) !== subscriptionFilter) return false
      if (countryFilter !== 'all' && node.countryCode !== countryFilter) return false
      return true
    })
  }, [nodes, query, statusFilter, roleFilter, sourceFilter, subscriptionFilter, countryFilter])
  const ordered = useMemo(() => sortNodes(filtered, sort.key, sort.direction, measurements), [filtered, sort, measurements])
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
    unhealthy: nodes.filter((node) => node.enabled && !node.alive && nodeObserved(node)).length,
    unmonitored: nodes.filter((node) => node.enabled && !nodeObserved(node)).length,
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
    if (!selectedNode || selectedNodes.length !== 1 || !selectedNode.enabled || manualLifecycleBlocked || manualRunning || adaptiveRunning || manualRequestBusy) return
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
          {[['all', 'All'], ['alive', 'Alive'], ['unhealthy', 'Not alive'], ['unmonitored', 'Not monitored'], ['disabled', 'Disabled'], ['stale', 'Stale']].filter(([value]) => value !== 'stale' || statusCounts.stale > 0 || statusFilter === 'stale').map(([value, label]) => <ToggleGroupItem key={value} value={value}>{label} <span>{statusCounts[value]}</span></ToggleGroupItem>)}
        </ToggleGroup>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Role</FieldLabel><NativeSelect aria-label="Filter by role" value={roleFilter} onChange={(event) => chooseFilter('roleFilter', event.target.value)}>{[['all', 'Any'], ['native', 'Native'], ['override', 'Override'], ['effective', 'Effective'], ['none', 'No role']].map(([value, label]) => <NativeSelectOption value={value} key={value}>{label}</NativeSelectOption>)}</NativeSelect></Field>
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Source</FieldLabel><NativeSelect aria-label="Filter by source" value={sourceFilter} onChange={(event) => chooseFilter('sourceFilter', event.target.value)}><NativeSelectOption value="all">All</NativeSelectOption>{sourceOptions.map((source) => <NativeSelectOption value={source} key={source}>{source}</NativeSelectOption>)}</NativeSelect></Field>
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Subscription</FieldLabel><NativeSelect aria-label="Filter by subscription" value={subscriptionFilter} onChange={(event) => chooseFilter('subscriptionFilter', event.target.value)}><NativeSelectOption value="all">All</NativeSelectOption>{subscriptionOptions.map((name) => <NativeSelectOption value={JSON.stringify([true, name])} key={name}>{name || 'Unnamed subscription'}</NativeSelectOption>)}</NativeSelect></Field>
        <Field orientation="horizontal" className="w-auto"><FieldLabel>Country</FieldLabel><NativeSelect aria-label="Filter by country" value={countryFilter} onChange={(event) => chooseFilter('countryFilter', event.target.value)}><NativeSelectOption value="all">All</NativeSelectOption>{countryOptions.map((country) => <NativeSelectOption value={country} key={country}>{country}</NativeSelectOption>)}</NativeSelect></Field>
        {filtersActive && <Button variant="ghost" type="button" onClick={clearFilters}>Clear</Button>}
      </div>
    </div>

    <div className="flex flex-wrap items-center gap-3" role="toolbar" aria-label="Selected node actions">
      <CSPProvider disableStyleElements><DropdownMenu><DropdownMenuTrigger render={<Button variant="outline" />} >Columns</DropdownMenuTrigger><DropdownMenuContent>{Object.entries({ address: 'Address', health: 'Health', latency: 'Latency', rank: 'Quality rank', download: 'Download', upload: 'Upload', role: 'Role', source: 'Source', subscription: 'Subscription' }).map(([key,label]) => <DropdownMenuCheckboxItem key={key} checked={showColumn(key)} onCheckedChange={(checked) => toggleColumn(key, checked)}>{label}</DropdownMenuCheckboxItem>)}</DropdownMenuContent></DropdownMenu></CSPProvider>
      <div className="flex items-center gap-1"><NodeActionButton icon="select" label={`Select all ${filtered.length} filtered`} onClick={toggleAllFiltered} disabled={busy || lifecycleBlocked || !filtered.length || allFilteredSelected} /><NodeActionButton icon="close" label="Clear selection" onClick={clearSelection} disabled={busy || lifecycleBlocked || !selectedIDs.size} /></div>
      <Separator orientation="vertical" className="h-6" /><div className="flex flex-wrap items-center gap-1">
        <NodeActionButton icon="target" text={selectedManual ? 'Unpin' : 'Pin'} description={!selectedNode ? 'Select one enabled node first. A manual pin bypasses automatic failover until cleared or Xray restarts.' : 'Native Xray pin. Clear it to resume automatic selection and failover.'} label={selectedManual ? 'Clear manual override' : 'Set manual override'} active={Boolean(selectedManual)} onClick={() => setManualOverride(selectedManual ? '' : (selectedNode.outboundTag || selectedNode.tag))} disabled={busy || lifecycleBlocked || !selectionAvailable || !selectedNode || (!selectedManual && !selectedNode.enabled)} />
        <NodeActionButton icon="gauge" text="Speed test" label={manualRequestBusy ? 'Starting speed test…' : 'Full speed test'} onClick={runManualNode} disabled={busy || manualRequestBusy || manualLifecycleBlocked || manualRunning || adaptiveRunning || selectedNodes.length !== 1 || !selectedNode?.enabled} />
        <NodeActionButton icon="edit" text="Edit" label="Edit / replace profile" onClick={() => openEditor()} disabled={busy || lifecycleBlocked || selectedNodes.length !== 1} />
        <NodeActionButton icon="enable" text="Enable" label="Enable" onClick={() => requestPreview('/api/v1/nodes/batch/state/preview', { nodeIds: selectedNodeIDs, enabled: true })} disabled={busy || lifecycleBlocked || !selectedNodes.length || selectedNodes.every((node) => node.enabled)} />
        <NodeActionButton icon="disable" text="Disable" label="Disable" onClick={() => requestPreview('/api/v1/nodes/batch/state/preview', { nodeIds: selectedNodeIDs, enabled: false })} disabled={busy || lifecycleBlocked || !selectedNodes.length || selectedNodes.every((node) => !node.enabled)} />
        <NodeActionButton icon="trash" text="Delete" label="Delete" tone="danger" onClick={() => requestPreview('/api/v1/nodes/batch/remove/preview', { nodeIds: selectedNodeIDs })} disabled={busy || lifecycleBlocked || !selectedNodes.length} />
      </div>
    </div>

    {manualStatus.state !== 'idle' && <div className="app-content"><ManualPerformanceCard status={manualStatus} node={nodes.find((node) => node.id === manualStatus.targetNodeId)} /></div>}

    {selectedNode && editingID === selectedNode.id && <Card className="selection-editor"><CardHeader><CardTitle>Replace profile</CardTitle><CardDescription><NodeName node={selectedNode} /><span className="block">Stable tag: <code>{selectedNode.outboundTag || selectedNode.tag}</code></span></CardDescription></CardHeader><CardContent><FieldGroup><Field>
      <FieldLabel htmlFor="replacement-profile">Replacement VLESS profile</FieldLabel><Input id="replacement-profile" aria-label="Replacement VLESS profile" value={replacement} onChange={(event) => setReplacement(event.target.value)} placeholder="Replacement vless:// profile" type="password" autoComplete="off" autoFocus /></Field>
      <div className="flex flex-wrap justify-end gap-2"><Button variant="outline" type="button" onClick={() => { setEditingID(''); setReplacement('') }}>Cancel</Button><Button type="button" disabled={busy || lifecycleBlocked || !replacement.trim()} onClick={() => requestPreview('/api/v1/nodes/replace/preview', { id: selectedNode.id, profile: replacement })}>Preview replacement</Button></div>
    </FieldGroup></CardContent></Card>}

    <Table className="nodes-table"><TableHeader><TableRow><TableHead className="selection-column"><SelectionCheckbox label="Select all filtered nodes" checked={allFilteredSelected} indeterminate={selectedFilteredCount > 0 && !allFilteredSelected} onChange={toggleAllFiltered} /></TableHead><SortHeader label="Name" sortKey="name" sort={sort} onSort={changeSort} />{showColumn('address') && <SortHeader label="Address" sortKey="address" sort={sort} onSort={changeSort} />}{showColumn('health') && <SortHeader label="Health" sortKey="health" sort={sort} onSort={changeSort} />}{showColumn('latency') && <SortHeader label="Latency" sortKey="latency" sort={sort} onSort={changeSort} />}{showColumn('rank') && <SortHeader label="Quality rank" sortKey="rank" sort={sort} onSort={changeSort} />}{showColumn('download') && <SortHeader label="Download" sortKey="download" sort={sort} onSort={changeSort} />}{showColumn('upload') && <SortHeader label="Upload" sortKey="upload" sort={sort} onSort={changeSort} />}{showColumn('role') && <SortHeader label="Role" sortKey="role" sort={sort} onSort={changeSort} />}{showColumn('source') && <SortHeader label="Source" sortKey="source" sort={sort} onSort={changeSort} />}{showColumn('subscription') && <SortHeader label="Subscription" sortKey="subscription" sort={sort} onSort={changeSort} />}</TableRow></TableHeader><TableBody>
      {visibleNodes.map((node) => <NodeRows key={node.id || node.tag} showColumn={showColumn} measurement={measurements.get(node.outboundTag || node.tag)} node={node} selected={selectedIDs.has(node.id)} onToggle={() => toggleSelection(node.id)} />)}
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

    {preview && <PreviewDialog preview={preview} nodes={nodes} manualOverride={manualOverride} busy={busy || lifecycleBlocked} onCancel={cancelPreview} onApply={applyPreview} returnFocus={previewTrigger} />}
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
      <div><span>Bytes</span><strong>{formatManualBytes(status.bytesTransferred)} / {formatManualBytes(status.bytesPlanned)}</strong>{status.maxWallSeconds > 0 && <small>Up to {status.maxWallSeconds} seconds including cleanup</small>}</div>
      <div><span>Latency</span><strong>{formatAdaptiveLatency(status.latencyMs)}</strong></div>
      <div><span>Download</span><strong>{formatManualRate(status.downloadBps)}</strong></div>
      <div><span>Upload</span><strong>{formatManualRate(status.uploadBps)}</strong></div>
    </div>
    {status.currentStage && <small className="manual-current-stage">Stage: {status.currentStage}</small>}
    {error && <p className="warning">{error}</p>}
  </section>
}

function NodeRows({ showColumn, measurement, node, selected, onToggle }) {
  return <>
    <TableRow className={!node.enabled ? "node-disabled" : undefined} data-state={selected ? 'selected' : undefined} tabIndex={0} aria-selected={selected} onClick={(event) => { if (!event.target.closest('input, label, button, a, [role=checkbox]')) onToggle() }} onKeyDown={(event) => { if (event.target === event.currentTarget && [' ', 'Enter'].includes(event.key)) { event.preventDefault(); onToggle() } }}>
      <TableCell className="selection-column"><SelectionCheckbox label={`Select ${visibleNodeName(node)}`} checked={selected} onChange={onToggle} /></TableCell>
      <TableCell><NodeName node={node} />{node.stale && <Badge variant="secondary">stale</Badge>}</TableCell>
      {showColumn('address') && <TableCell data-label="Address"><code className="address">{node.address || '—'}</code></TableCell>}
      {showColumn('health') && <TableCell data-label="Health"><span title={node.enabled && !nodeObserved(node) ? notMonitoredTitle : node.enabled && !node.alive && node.lastError ? node.lastError : undefined}><StatusBadge tone={nodeHealthTone(node)}>{nodeHealthLabel(node)}</StatusBadge></span></TableCell>}
      {showColumn('latency') && <TableCell data-label="Latency">{node.alive ? formatAdaptiveLatency(node.latencyMs) : '—'}</TableCell>}
      {showColumn('rank') && <TableCell data-label="Quality rank">{measurement?.rank ? <StatusBadge>#{measurement.rank}</StatusBadge> : '—'}</TableCell>}
      {showColumn('download') && <TableCell data-label="Download">{measurement?.valid ? formatRate(measurement.downloadBps) : '—'}</TableCell>}
      {showColumn('upload') && <TableCell data-label="Upload">{measurement?.valid ? formatRate(measurement.uploadBps) : '—'}</TableCell>}
      {showColumn('role') && <TableCell data-label="Role"><NodeBadges node={node} /></TableCell>}
      {showColumn('source') && <TableCell data-label="Source"><span>{node.sourceType || 'legacy'}</span></TableCell>}
      {showColumn('subscription') && <TableCell data-label="Subscription">{node.sourceType === 'subscription' ? node.subscriptionName || 'Unnamed subscription' : '—'}</TableCell>}
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

function SortHeader({ label, sortKey, sort, onSort }) {
  const active = sort.key === sortKey
  const direction = active ? sort.direction : 'none'
  const SortIcon = active ? sort.direction === 'asc' ? IconArrowUp : IconArrowDown : IconArrowsSort
  return <TableHead aria-sort={direction === 'none' ? 'none' : direction === 'asc' ? 'ascending' : 'descending'}><Button variant="ghost" size="sm" className="sort-button" type="button" onClick={() => onSort(sortKey)}>{label}<SortIcon data-icon="inline-end" aria-hidden="true" /></Button></TableHead>
}

function SelectionCheckbox({ checked, indeterminate = false, label, onChange }) {
  return <label className="selection-checkbox"><Checkbox aria-label={label} checked={checked} indeterminate={indeterminate} onCheckedChange={onChange} /></label>
}

function NodeActionButton({ icon, label, text = '', description, tone = '', active = false, ...props }) {
  const ActionIcon = { select: IconSquareCheck, enable: IconPlayerPlay, disable: IconPlayerPause, refresh: IconRefresh, edit: IconPencil, power: IconPower, trash: IconTrash, close: IconX, left: IconChevronLeft, right: IconChevronRight, gauge: IconGauge, target: IconFocus2 }[icon]
  return <ActionHint label={label} description={description || ({ target: 'Temporarily pin one enabled node in Xray. Clear to resume native automatic selection.', gauge: 'Measure latency, download and upload for one enabled node.', enable: 'Enable selected profiles after preview.', disable: 'Keep these profiles disabled, including on subscription refresh.' })[icon]} disabled={props.disabled}><Button type="button" variant={tone === 'danger' ? 'destructive' : active ? 'secondary' : 'outline'} size={text ? 'sm' : 'icon'} aria-label={label} {...props}><ActionIcon data-icon="inline-start" />{text && <span>{text}</span>}</Button></ActionHint>
}

function NodeName({ node }) {
  const flag = COUNTRY_FLAGS[node?.countryCode]
  return <strong className="display-name">{flag && <img className="country-flag" src={flag} alt="" aria-hidden="true" />}<span>{visibleNodeName(node)}</span></strong>
}

function Icon({ name }) {
  const icons = { select: IconSquareCheck, enable: IconPlayerPlay, disable: IconPlayerPause, plus: IconPlus, link: IconLink, refresh: IconRefresh, edit: IconPencil, power: IconPower, trash: IconTrash, close: IconX, left: IconChevronLeft, right: IconChevronRight, search: IconSearch, gauge: IconGauge, target: IconFocus2 }
  const Component = icons[name]
  return Component ? <Component size={16} aria-hidden="true" focusable="false" /> : null
}

function Shell({ children }) { return <main className="min-h-svh min-w-80 bg-background text-foreground">{children}</main> }
function Notice({ message, tone = 'error' }) { return <Alert variant={tone === 'error' ? 'destructive' : 'default'} role="status"><AlertDescription>{message}</AlertDescription></Alert> }
// One compact health row: a coloured dot carries the state, the detail says why.
function HealthCard({ label, ok, detail }) { return <div role="listitem" className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2"><span aria-hidden="true" className={`mt-1.5 size-2 shrink-0 rounded-full ${ok ? 'bg-emerald-500' : 'bg-amber-500'}`} /><div className="min-w-0"><div className="text-sm font-medium">{label} <span className="sr-only">{ok ? 'healthy' : 'degraded'}</span></div><div className="truncate text-xs text-muted-foreground">{ok ? detail : `Degraded · ${detail}`}</div></div></div> }
function SelectionCard({ label, node, emptyText = 'No current target' }) { return <Card size="sm"><CardHeader><CardDescription>{label}</CardDescription><CardTitle>{node ? <NodeName node={node} /> : emptyText}</CardTitle></CardHeader></Card> }
function NodeBadges({ node }) { return <div className="badges">{node.isNativeSelected && <StatusBadge>native</StatusBadge>}{node.isOverride && <StatusBadge tone="warning">override</StatusBadge>}{node.isEffective && <StatusBadge tone="success">effective</StatusBadge>}</div> }
function formatUptime(seconds) { if (!seconds) return '—'; const hours = Math.floor(seconds / 3600); const minutes = Math.floor((seconds % 3600) / 60); return `${hours}h ${minutes}m` }

createRoot(document.getElementById('root')).render(<StrictMode><TooltipProvider delay={300}><App /></TooltipProvider></StrictMode>)
