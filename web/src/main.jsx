import { StrictMode, useCallback, useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './styles.css'

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
    throw error
  }
  return body
}

const formatTime = (value) => value ? new Date(value).toLocaleString() : '—'
const formatNumber = (value) => new Intl.NumberFormat().format(value ?? 0)
const formatBytes = (value) => value ? `${formatNumber(Math.round(value / (1024 * 1024)))} MiB` : '—'
const formatThroughput = (node) => {
  if (!node.lastBenchmarkAt) return '—'
  const value = `${formatNumber(Math.round(node.lastThroughputKBps || 0))} KB/s`
  return node.lastThroughputError ? `${value} (${node.lastThroughputError})` : value
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
      case 'throughput': return node.lastBenchmarkAt ? Number(node.lastThroughputKBps || 0) : -1
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
      const [status, nodes, performance, config] = await Promise.all([
        api('/api/v1/status'),
        api('/api/v1/nodes'),
        api('/api/v1/performance'),
        api('/api/v1/config-summary'),
      ])
      setDashboard({ status, nodes, performance, config })
      setError('')
    } catch (cause) {
      if (cause.status === 401) setSession(null)
      setError(cause.message)
    } finally {
      setLoading(false)
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

  const runBenchmark = async () => {
    try {
      await api('/api/v1/benchmark/run', {
        method: 'POST',
        headers: { 'X-CSRF-Token': session?.csrfToken || '' },
      })
      await loadDashboard()
    } catch (cause) {
      setError(cause.message)
    }
  }

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

  if (!session) return <Login error={error} password={password} setPassword={setPassword} onSubmit={login} />
  if (loading && !dashboard) return <Shell><div className="loading">Reading current router state…</div></Shell>
  if (!dashboard) return <Shell><Notice message={error || 'Runtime state is unavailable.'} /></Shell>

  return <Dashboard dashboard={dashboard} session={session} error={error} onRefresh={loadDashboard} onLogout={logout} onRunBenchmark={runBenchmark} />
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

function Dashboard({ dashboard, session, error, onRefresh, onLogout, onRunBenchmark }) {
  const { status, nodes, config } = dashboard
  const [section, setSection] = useState('overview')
  const [nodeView, setNodeView] = useState(createNodeViewState)
  const registryNodes = nodes.nodes || []
  const nodesByTag = useMemo(() => new Map(registryNodes.map((node) => [node.outboundTag || node.tag, node])), [registryNodes])

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
      <button type="button" className={section === 'system' ? 'active' : ''} onClick={() => setSection('system')}>System</button>
    </nav>
    {error && <Notice message={error} />}
    {section === 'overview' && <Overview status={status} nodeTotal={nodes.total || 0} nodesByTag={nodesByTag} onRunBenchmark={onRunBenchmark} />}
    {section === 'nodes' && <NodeWorkspace nodes={registryNodes} subscriptions={nodes.subscriptions || []} manualOverride={status.selection?.manualOverride || ''} csrf={session.csrfToken} onRefresh={onRefresh} viewState={nodeView} onViewStateChange={setNodeView} />}
    {section === 'system' && <SystemSection status={status} config={config} nodesByTag={nodesByTag} />}
  </Shell>
}

function Overview({ status, nodeTotal, nodesByTag, onRunBenchmark }) {
  const healthy = status.observatory?.healthy || 0
  const total = status.observatory?.total || nodeTotal
  const healthText = total ? `${healthy}/${total} healthy` : 'No node data'
  return <div className="section-stack">
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
      <div className="panel schedule-card"><div className="schedule-heading"><span className="panel-label">Selection & benchmark</span><IconButton icon="gauge" label={status.benchmark?.controlPlane?.running ? 'Full benchmark running' : 'Run full benchmark'} active={status.benchmark?.controlPlane?.running} onClick={onRunBenchmark} disabled={status.benchmark?.controlPlane?.running} /></div><strong>{status.selection?.state || 'starting'} · {status.selection?.effectiveTarget || status.balancer?.effective || 'native fallback'}</strong><p>{status.selection?.lastSwitchReason || 'No selection change recorded'} · evidence: {status.selection?.latencyEvidence ?? 0} RTT samples · dwell: {status.selection?.dwellRemainingSeconds ? `${status.selection.dwellRemainingSeconds}s` : 'ready'}</p><small>Run policy: {formatBytes(status.benchmark?.controlPlane?.totalBudgetBytes)} total · {formatBytes(status.benchmark?.controlPlane?.payloadBytes)} planned/node · {status.benchmark?.controlPlane?.perNodeTimeoutMs ? `${status.benchmark.controlPlane.perNodeTimeoutMs / 1000}s` : '10s'} timeout · next: {formatTime(status.benchmark?.controlPlane?.nextRunAt)}</small></div>
    </section>
  </div>
}

function NodeWorkspace({ nodes, subscriptions, manualOverride, csrf, onRefresh, viewState, onViewStateChange }) {
  const [profiles, setProfiles] = useState('')
  const [subscriptionUrl, setSubscriptionUrl] = useState('')
  const [subscriptionName, setSubscriptionName] = useState('')
  const [subscriptionID, setSubscriptionID] = useState('')
  const [replacement, setReplacement] = useState('')
  const [editingID, setEditingID] = useState('')
  const [composer, setComposer] = useState('')
  const [preview, setPreview] = useState(null)
  const [notice, setNotice] = useState(null)
  const [busy, setBusy] = useState(false)
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

  const requestPreview = async (path, payload, effectiveImpact = '') => {
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
    if (!preview) return
    setBusy(true)
    setNotice(null)
    try {
      await api('/api/v1/node-changes/apply', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify({ previewToken: preview.previewToken, acceptMissing: Boolean(preview.requiresAcceptance) }) })
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

  const openEditor = (id) => {
    setEditingID(editingID === id ? '' : id)
    setReplacement('')
  }

  const setManualOverride = async (target) => {
    setBusy(true)
    setNotice(null)
    try {
      await api('/api/v1/selection/override', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify({ target }) })
      await onRefresh()
      setNotice({ tone: 'success', message: target ? 'Manual override set. Automatic latency and benchmark selection are paused for this node.' : 'Manual override cleared. Automatic selection is active again.' })
    } catch (cause) {
      setNotice({ tone: 'error', message: cause.message })
    } finally {
      setBusy(false)
    }
  }

  return <section className="panel nodes-workspace">
    <div className="workspace-heading">
      <h2>Nodes <span className="count">{nodes.length}</span></h2>
      <div className="workspace-actions">
        <IconButton icon="plus" label="Add VLESS profiles" onClick={() => setComposer(composer === 'profiles' ? '' : 'profiles')} />
        <IconButton icon="link" label="Add subscription" onClick={() => composer === 'subscription' ? closeComposer() : startNewSubscription()} />
        <IconButton icon="refresh" label="Refresh dashboard" onClick={onRefresh} />
      </div>
    </div>

    {notice && <Notice message={notice.message} tone={notice.tone} />}

    {composer === 'profiles' && <form className="composer" onSubmit={(event) => { event.preventDefault(); requestPreview('/api/v1/nodes/import/preview', { profiles }) }}>
      <div><span className="panel-label">Add profiles</span><h3>Import VLESS + REALITY links</h3><p>Names are read from link fragments. Keys stay inside the authenticated request and RAM-only preview.</p></div>
      <textarea value={profiles} onChange={(event) => setProfiles(event.target.value)} placeholder="Paste one or more vless:// links" autoFocus />
      <div className="composer-actions"><button className="ghost" type="button" onClick={closeComposer}>Cancel</button><button type="submit" disabled={busy || !profiles.trim()}>Preview add</button></div>
    </form>}

    {composer === 'subscription' && <form className="composer compact" onSubmit={(event) => { event.preventDefault(); requestPreview('/api/v1/subscriptions/refresh/preview', { ...(subscriptionID ? { subscriptionId: subscriptionID } : {}), name: subscriptionName, url: subscriptionUrl }) }}>
      <div><span className="panel-label">{subscriptionID ? 'Update subscription' : 'New subscription'}</span><h3>Fetch explicitly</h3><p>HTTPS only, with DNS/IP SSRF checks and bounded response parsing.</p></div>
      <label htmlFor="subscription-name">Display name</label>
      <input id="subscription-name" value={subscriptionName} onChange={(event) => setSubscriptionName(event.target.value)} placeholder="Home provider" />
      <label htmlFor="subscription-url">Subscription URL</label>
      <input id="subscription-url" type="password" value={subscriptionUrl} onChange={(event) => setSubscriptionUrl(event.target.value)} placeholder={subscriptionID ? 'Leave blank to keep the saved URL' : 'https://…'} autoComplete="off" />
      <div className="composer-actions"><button className="ghost" type="button" onClick={closeComposer}>Cancel</button><button type="submit" disabled={busy || (!subscriptionID && !subscriptionUrl.trim())}>{subscriptionID ? 'Preview update' : 'Preview subscription'}</button></div>
    </form>}

    {!!subscriptions.length && <div className="subscription-strip">
      {subscriptions.map((subscription) => { const enabled = subscription.enabled !== false; const name = subscription.name || 'Unnamed subscription'; return <div className={`subscription-card ${enabled ? '' : 'disabled'}`} key={subscription.id}>
        <div><strong>{name}</strong><small>{enabled ? 'Enabled' : 'Disabled'} · {subscription.nodeCount} nodes{subscription.staleCount ? ` · ${subscription.staleCount} stale` : ''}</small></div>
        <div className="subscription-actions">
          <IconButton icon="refresh" label={`Refresh ${name}`} disabled={busy} onClick={() => requestPreview('/api/v1/subscriptions/refresh/preview', { subscriptionId: subscription.id })} />
          <IconButton icon="edit" label={`Edit ${name}`} disabled={busy} onClick={() => openSubscriptionEditor(subscription)} />
          <IconButton icon="power" label={enabled ? `Disable ${name}` : `Enable ${name}`} active={!enabled} disabled={busy} onClick={() => requestPreview('/api/v1/subscriptions/state/preview', { subscriptionId: subscription.id, enabled: !enabled })} />
          <IconButton icon="trash" label={`Remove ${name}`} tone="danger" disabled={busy} onClick={() => requestPreview('/api/v1/subscriptions/remove/preview', { subscriptionId: subscription.id })} />
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

    <div className="table-wrap"><table className="nodes-table"><thead><tr><SortHeader label="Name" sortKey="name" sort={sort} onSort={changeSort} /><SortHeader label="Address" sortKey="address" sort={sort} onSort={changeSort} /><SortHeader label="Health" sortKey="health" sort={sort} onSort={changeSort} /><SortHeader label="Latency" sortKey="latency" sort={sort} onSort={changeSort} /><SortHeader label="Role" sortKey="role" sort={sort} onSort={changeSort} /><SortHeader label="Source" sortKey="source" sort={sort} onSort={changeSort} /><SortHeader label="Throughput" sortKey="throughput" sort={sort} onSort={changeSort} /><th aria-label="Actions"></th></tr></thead><tbody>
      {visibleNodes.map((node) => <NodeRows key={node.id || node.tag} node={node} manualOverride={manualOverride} onSetManualOverride={setManualOverride} editing={editingID === node.id} replacement={replacement} setReplacement={setReplacement} busy={busy} openEditor={openEditor} requestPreview={requestPreview} />)}
      {!visibleNodes.length && <tr><td colSpan="8" className="empty">No nodes match this view.</td></tr>}
    </tbody></table></div>

    <Pagination page={page} totalPages={totalPages} onPage={(value) => onViewStateChange((current) => ({ ...current, page: value }))} />
    {preview && <PreviewDialog preview={preview} busy={busy} onCancel={cancelPreview} onApply={applyPreview} />}
  </section>
}

function NodeRows({ node, manualOverride, onSetManualOverride, editing, replacement, setReplacement, busy, openEditor, requestPreview }) {
  const health = node.alive ? 'Alive' : (node.enabled ? (node.lastError || 'No data') : 'Disabled')
  const target = node.outboundTag || node.tag
  const manual = manualOverride === target
  return <>
    <tr className={editing ? 'editing' : ''}>
      <td><NodeName node={node} />{node.stale && <span className="chip amber">stale</span>}</td>
      <td><code className="address">{node.address || '—'}</code></td>
      <td><span className={`status-dot ${node.alive ? 'up' : 'down'}`}></span>{health}</td>
      <td>{node.latencyMs ? `${node.latencyMs} ms` : '—'}</td>
      <td><NodeBadges node={node} /></td>
      <td><span>{node.subscriptionName || node.sourceType || 'legacy'}</span></td>
      <td>{formatThroughput(node)}</td>
      <td><div className="row-actions">
        <IconButton icon="target" label={manual ? `Clear manual override for ${visibleNodeName(node)}` : `Set ${visibleNodeName(node)} as manual override`} active={manual} disabled={busy || (!node.enabled && !manual)} onClick={() => onSetManualOverride(manual ? '' : target)} />
        <IconButton icon="edit" label={`Edit ${visibleNodeName(node)}`} disabled={busy || !node.id} active={editing} onClick={() => openEditor(node.id)} />
        <IconButton icon="power" label={node.enabled ? `Disable ${visibleNodeName(node)}` : `Enable ${visibleNodeName(node)}`} disabled={busy || !node.id} onClick={() => requestPreview(`/api/v1/nodes/${node.id}/state/preview`, { enabled: !node.enabled }, node.isEffective && node.enabled ? 'disable' : '')} />
        <IconButton icon="trash" label={`Remove ${visibleNodeName(node)}`} tone="danger" disabled={busy || !node.id} onClick={() => requestPreview(`/api/v1/nodes/${node.id}/remove/preview`, {}, node.isEffective ? 'remove' : '')} />
      </div></td>
    </tr>
    {editing && <tr className="editor-row"><td colSpan="8"><div className="inline-editor">
      <div><span className="panel-label">Replace profile</span><strong>{visibleNodeName(node)}</strong><small>Stable tag: <code>{node.outboundTag || node.tag}</code></small></div>
      <input value={replacement} onChange={(event) => setReplacement(event.target.value)} placeholder="Replacement vless:// profile" type="password" autoComplete="off" autoFocus />
      <div className="inline-editor-actions"><button className="ghost" type="button" onClick={() => openEditor(node.id)}>Cancel</button><button type="button" disabled={busy || !replacement.trim()} onClick={() => requestPreview('/api/v1/nodes/replace/preview', { id: node.id, profile: replacement })}>Preview replacement</button></div>
    </div></td></tr>}
  </>
}

function PreviewDialog({ preview, busy, onCancel, onApply }) {
  return <div className="modal-backdrop" role="presentation">
    <div className="preview-dialog" role="dialog" aria-modal="true" aria-label="Preview node change">
      <div className="dialog-heading"><div><span className="panel-label">Preview · {preview.operation}</span><h3>{preview.noop ? 'No persistent change' : `${preview.changes?.length || 0} node changes`}</h3></div><IconButton icon="close" label="Close preview" onClick={onCancel} disabled={busy} /></div>
      <div className="diff-list">
        {(preview.changes || []).map((change) => <div className="diff-row" key={`${change.action}-${change.id}`}><strong>{change.name}</strong><span>{change.before} → {change.after}</span></div>)}
        {preview.noop && <p className="muted">The fetched or requested state matches the current registry.</p>}
      </div>
      {preview.requiresAcceptance && <p className="warning">The provider response is missing existing nodes. Applying keeps them stale/missing; remove them separately through another explicit preview.</p>}
      {preview.effectiveImpact && <p className="warning" role="alert">This operation will {preview.effectiveImpact} the currently effective node. Active proxy traffic will be reselected after Apply.</p>}
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

function SystemSection({ status, config, nodesByTag }) {
  const dnsServers = (config.dns?.upstreams || []).map((upstream) => upstream.host || upstream.tag).filter(Boolean)
  const healthSelectors = config.observatory?.subjectSelectors || []
  const activeTag = status.selection?.effectiveTarget || status.balancer?.effective
  const activeNode = nodesByTag.get(activeTag)
  const benchmark = status.benchmark?.controlPlane
  const benchmarkResult = benchmark?.lastResult || benchmark?.state
  const benchmarkDetails = benchmarkResult
    ? `${benchmarkResult === 'completed' ? 'Completed' : benchmarkResult} · ${benchmark.lastValidSamples ?? 0} valid samples · ${formatTime(benchmark.lastCompletedAt || status.benchmark?.lastRunAt)}`
    : 'Not run yet'

  return <section className="lower-grid system-section">
    <div className="panel"><span className="panel-label">Network policy</span><h2>How traffic is handled</h2><div className="metric-row"><span>Routing rules</span><strong>{config.routing?.ruleCount ?? '—'}</strong></div><div className="metric-row"><span>DNS servers</span><div className="metric-value-list">{dnsServers.length ? dnsServers.map((server) => <code key={server}>{server}</code>) : <strong>—</strong>}</div></div><div className="metric-row"><span>Proxy pool</span><strong>Unified proxy pool</strong></div><div className="metric-row"><span>Health-check scope</span><div className="metric-value-list">{healthSelectors.length ? healthSelectors.map((selector) => <code key={selector}>{selector}</code>) : <strong>—</strong>}</div></div></div>
    <div className="panel"><span className="panel-label">Runtime</span><h2>Current state</h2><div className="metric-row"><span>Active proxy</span><div className="metric-value">{activeNode ? <strong>{visibleNodeName(activeNode)}</strong> : <strong>{activeTag || 'Automatic selection'}</strong>}{activeNode?.address && <small>{activeNode.address}</small>}</div></div><div className="metric-row"><span>Healthy proxies</span><strong>{status.observatory?.healthy ?? 0} / {status.observatory?.total ?? 0}</strong></div><div className="metric-row"><span>Full benchmark</span><div className="metric-value"><strong>{benchmarkDetails}</strong>{benchmark?.nextRunAt && <small>Next: {formatTime(benchmark.nextRunAt)}</small>}</div></div><div className="metric-row"><span>Control plane uptime</span><strong>{formatUptime(status.controlPlane?.uptimeSeconds)}</strong></div></div>
  </section>
}

function SortHeader({ label, sortKey, sort, onSort }) {
  const active = sort.key === sortKey
  const direction = active ? sort.direction : 'none'
  return <th aria-sort={direction === 'none' ? 'none' : direction === 'asc' ? 'ascending' : 'descending'}><button className={`sort-button ${active ? 'active' : ''}`} type="button" onClick={() => onSort(sortKey)}>{label}<span aria-hidden="true">{active ? (sort.direction === 'asc' ? '↑' : '↓') : '↕'}</span></button></th>
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
