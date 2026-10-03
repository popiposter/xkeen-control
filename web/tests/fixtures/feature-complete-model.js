const csrf = 'synthetic-feature-complete-csrf-1'

// These lists describe safe DTOs, never copies of internal authority objects.
const allowlist = (value, fields) => Object.fromEntries(fields.filter((field) => Object.hasOwn(value, field)).map((field) => [field, value[field]]))
const performancePolicyFields = ['schemaVersion', 'probeIntervalSeconds', 'failureThreshold', 'adaptiveCadenceMinutes', 'adaptiveChallengerLimit', 'minimumDwellMinutes', 'qualityHysteresisPercent']
const listenerProjection = (value) => allowlist(value, ['host', 'port', 'source', 'editability', 'allowedHosts'])
const updateProjection = (value) => ({
  ...allowlist(value, ['channel', 'rollbackAvailable', 'signingKeyConfigured', 'latestCompatibleVersion', 'latestChannel', 'latestSource', 'latestSourceCommit', 'lastCheckAt']),
  installed: allowlist(value.installed, ['product', 'version', 'sourceCommit', 'channel']),
  policy: allowlist(value.policy, ['channel', 'mode', 'checkCadenceMinutes']),
})

const json = (route, value, status = 200) => route.fulfill({
  status,
  contentType: 'application/json',
  body: JSON.stringify(value),
})

const defaultPerformancePolicy = () => ({
  schemaVersion: 1,
  probeIntervalSeconds: 60,
  failureThreshold: 2,
  adaptiveCadenceMinutes: 180,
  adaptiveChallengerLimit: 5,
  minimumDwellMinutes: 30,
  qualityHysteresisPercent: 10,
})

const performanceProjection = (policy = defaultPerformancePolicy()) => ({
  policy: allowlist(policy, performancePolicyFields),
  source: 'default',
  authorityState: 'editable',
  persistedSource: 'default',
  hardCeilings: {
    maxCandidates: 6,
    candidateDownloadMiB: 16,
    candidateUploadMiB: 8,
    candidateMaxSeconds: 30,
    generationMaxMiB: 144,
    generationMaxSeconds: 180,
    transportIdentity: 'source-owned',
    rttGuard: 'source-owned',
    scoring: 'source-owned',
  },
  adaptive: { state: 'waiting', nextRunAt: new Date(Date.now() + 10_800_000).toISOString(), generation: 3 },
})

const componentState = (overrides = {}) => ({
  controlPlane: { version: 'synthetic', uptimeSeconds: 3600 },
  xray: { running: true, apiReachable: true, probeReachable: true },
  xkeen: { running: true },
  balancer: { effective: 'proxy-node-00000001' },
  observatory: { healthy: 1, total: 1, apiReachable: true },
  benchmark: { controlPlane: { running: false, state: 'idle' } },
  selection: { state: 'stable' },
  native: { installation: 'available', panelIntegration: 'available', version: '2.0.1', channel: 'beta', core: 'xray', xrayRunning: true, geodataFiles: 6, geodataCron: 'available' },
  lifecycle: { maintenance: false, applying: false },
  ...overrides,
})

const nodeState = () => ({
  id: 'node-00000001',
  name: 'Feature test node',
  displayName: 'Feature test node',
  address: '198.51.100.25:443',
  countryCode: 'DE',
  outboundTag: 'proxy-node-00000001',
  enabled: false,
  sourceType: 'manual',
  subscriptionName: '',
  alive: true,
  latencyMs: 36,
  lastError: '',
  lastThroughputKBps: 0,
  lastBenchmarkAt: '',
  isNativeSelected: false,
  isOverride: false,
  isEffective: false,
  stale: false,
  missing: false,
})

const nodeProjection = (node) => allowlist(node, ['id', 'name', 'displayName', 'address', 'countryCode', 'outboundTag', 'enabled', 'sourceType', 'subscriptionName', 'alive', 'latencyMs', 'lastError', 'lastThroughputKBps', 'lastBenchmarkAt', 'isNativeSelected', 'isOverride', 'isEffective', 'stale', 'missing'])
const privateUUID = 'feature-private-uuid-sentinel'
const privateRealityKey = 'feature-private-reality-key-sentinel'
const privateShortID = 'feature-private-short-id-sentinel'
const privateResolverURL = 'https://resolver.example.invalid/feature-private-resolver-url-sentinel'
const privateSubscriptionToken = 'feature-private-subscription-token'
const privateSubscriptionURL = `https://subscriptions.example.invalid/${privateSubscriptionToken}`
const privateGeneratedOutbounds = '{"feature-private-generated-outbounds-sentinel":"synthetic-only"}'
const privatePasswordHash = '$2a$10$feature-private-password-hash-sentinel'

const privateVPNProfile = [
  'vless:',
  '/',
  `/${privateUUID}@198.51.100.50:443?security=reality&pbk=${privateRealityKey}&sid=${privateShortID}`,
].join('')

const PRIVATE_SENTINELS = Object.freeze([
  privateVPNProfile,
  privateUUID, privateRealityKey, privateShortID, privateResolverURL,
  privateSubscriptionURL, privateSubscriptionToken,
  privateGeneratedOutbounds, 'feature-private-generated-outbounds-sentinel', privatePasswordHash,
])

export class FeatureCompleteModel {
  constructor(options = {}) {
    this.lifecycle = Object.hasOwn(options, 'lifecycle') ? options.lifecycle : { maintenance: false, applying: false }
    this.nodes = [{ ...nodeState(), enabled: options.nodeEnabled ?? false, uuid: privateUUID, reality: { publicKey: privateRealityKey, shortID: privateShortID }, profile: privateVPNProfile }]
    this.subscriptions = [{ id: 'subscription-00000001', name: 'Synthetic subscription', url: privateSubscriptionURL, token: privateSubscriptionToken }]
    this.runtime = { ...componentState(), generatedOutbounds: privateGeneratedOutbounds }
    this.performance = { nodes: [], manual: { state: 'idle', phase: 'done' }, adaptive: { state: 'waiting' }, generatedOutbounds: privateGeneratedOutbounds }
    this.routingRules = []
    this.dns = {
      proxyResolverIds: ['synthetic-resolver-a', 'synthetic-resolver-b'],
      fallbackMode: 'system',
      cacheEnabled: true,
      serveStale: true,
      staleTTLSeconds: 3600,
      parallelQueries: true,
      resolverCatalog: [
        { id: 'synthetic-resolver-a', label: 'Proxy resolver 1', url: privateResolverURL },
        { id: 'synthetic-resolver-b', label: 'Proxy resolver 2', url: privateResolverURL },
        { id: 'synthetic-resolver-c', label: 'Proxy resolver 3', url: privateResolverURL },
      ],
    }
    this.observatory = { probeIntervalMinutes: 5 }
    this.performancePolicy = defaultPerformancePolicy()
    this.listener = { host: '127.0.0.1', port: 8787, source: 'default', editability: 'editable', allowedHosts: ['127.0.0.1', '10.0.0.4'] }
    this.update = {
      channel: 'stable',
      installed: { product: 'xkeen-control', version: '0.2.0', sourceCommit: 'a'.repeat(40), channel: 'stable' },
      policy: { channel: 'stable', mode: 'manual', checkCadenceMinutes: 360 },
      rollbackAvailable: true,
      signingKeyConfigured: true,
      latestCompatibleVersion: '1.2.3',
      latestChannel: 'stable',
      latestSource: 'github-release',
      latestSourceCommit: 'b'.repeat(40),
      lastCheckAt: new Date().toISOString(),
    }
    this.requests = []
    this.safeProjectionBodies = []
    this.issues = []
    this.previewTokens = new Map()
    this.invalidatedTokens = []
    this.cancelledTokens = []
    this.writes = []
    this.tokenSequence = 0
    this.sessionSequence = 1
    this.auth = { csrfToken: csrf, passwordHash: privatePasswordHash }
    this.failNextApply = new Set()
    this.failReadCounts = new Map()
    this.reconnect = false
    this.reconnectListenerCandidate = ''
  }

  get csrfToken() { return this.auth.csrfToken }
  set csrfToken(value) { this.auth.csrfToken = value }

  status() {
    const value = {
      controlPlane: allowlist(this.runtime.controlPlane, ['version', 'uptimeSeconds']),
      xray: allowlist(this.runtime.xray, ['running', 'apiReachable', 'probeReachable']),
      xkeen: allowlist(this.runtime.xkeen, ['running']),
      balancer: allowlist(this.runtime.balancer, ['effective']),
      observatory: allowlist(this.runtime.observatory, ['healthy', 'total', 'apiReachable']),
      benchmark: { controlPlane: allowlist(this.runtime.benchmark.controlPlane, ['running', 'state']) },
      selection: allowlist(this.runtime.selection, ['state']),
      native: allowlist(this.runtime.native, ['installation', 'panelIntegration', 'version', 'channel', 'core', 'xrayRunning', 'geodataFiles', 'geodataCron']),
    }
    if (this.lifecycle != null) value.lifecycle = allowlist(this.lifecycle, ['maintenance', 'applying'])
    return value
  }

  recordProjection(route, value) {
    this.safeProjectionBodies.push(JSON.stringify(value))
    return json(route, value)
  }

  createPreview(owner, candidate) {
    const token = `feature-${owner}-preview-${++this.tokenSequence}`
    this.previewTokens.set(token, { owner, candidate, sessionSequence: this.sessionSequence })
    return token
  }

  async applyPreview(route, entry, owner) {
    const body = entry.body || {}
    if (Object.keys(body).length !== 1 || typeof body.previewToken !== 'string') {
      this.issues.push(`${owner} Apply did not use a token-only body`)
      return json(route, { error: 'invalid synthetic Apply body', code: 'invalid-request' }, 400)
    }
    const token = body.previewToken
    const pending = this.previewTokens.get(token)
    this.previewTokens.delete(token)
    if (!pending || pending.owner !== owner || pending.sessionSequence !== this.sessionSequence) {
      return json(route, { error: 'preview is unavailable in this session', code: 'preview-stale' }, 409)
    }
    if (this.failNextApply.has(owner)) {
      this.failNextApply.delete(owner)
      return route.abort('connectionfailed')
    }
    this.writes.push({ owner, candidate: pending.candidate })
    return pending
  }

  async handle(route) {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    let body = null
    if (request.postData()) {
      try { body = request.postDataJSON() } catch { body = request.postData() }
    }
    const entry = {
      path,
      method: request.method(),
      body,
      csrf: request.headers()['x-csrf-token'] || '',
    }
    this.requests.push(entry)

    if (entry.method === 'GET' && (this.failReadCounts.get(path) || 0) > 0) {
      this.failReadCounts.set(path, this.failReadCounts.get(path) - 1)
      return route.abort('connectionfailed')
    }

    switch (path) {
      case '/api/v1/session':
        if (this.reconnect && this.reconnectListenerCandidate) {
          this.listener = { ...this.listener, host: this.reconnectListenerCandidate, source: 'file' }
          this.reconnectListenerCandidate = ''
          this.reconnect = false
          this.sessionSequence++
          this.csrfToken = `synthetic-feature-complete-csrf-${this.sessionSequence}`
        }
        return this.recordProjection(route, allowlist(this.auth, ['csrfToken']))
      case '/api/v1/session/login':
        this.sessionSequence++
        this.csrfToken = `synthetic-feature-complete-csrf-${this.sessionSequence}`
        return json(route, allowlist(this.auth, ['csrfToken']))
      case '/api/v1/session/logout':
        this.invalidatedTokens.push(...this.previewTokens.keys())
        this.previewTokens.clear()
        this.sessionSequence++
        return json(route, {})
      case '/api/v1/status':
        return this.recordProjection(route, this.status())
      case '/api/v1/nodes':
        return this.recordProjection(route, { total: this.nodes.length, nodes: this.nodes.map(nodeProjection), subscriptions: this.subscriptions.map((subscription) => allowlist(subscription, ['id', 'name'])) })
      case '/api/v1/performance':
        return this.recordProjection(route, { nodes: this.performance.nodes.map(nodeProjection), manual: allowlist(this.performance.manual, ['state', 'phase']), adaptive: allowlist(this.performance.adaptive, ['state']) })
      case '/api/v1/config-summary':
        return this.recordProjection(route, { routing: {}, dns: {}, observatory: {} })
      case '/api/v1/xkeen/commands':
      case '/api/v1/xkeen/jobs/read':
        // This broader workspace model represents commands not wired in main.
        // Mutating native routes deliberately remain unexpected.
        if (entry.method !== (path.endsWith('/commands') ? 'GET' : 'POST')) {
          this.issues.push(`unexpected native inspection method: ${entry.method}`)
        }
        return json(route, { error: 'native commands unavailable' }, 503)
      case '/api/v1/panel/listener':
        return this.recordProjection(route, listenerProjection(this.listener))
      case '/api/v1/panel/listener/preview': {
        const host = body?.host
        const previewToken = this.createPreview('listener', host)
        const noop = host === this.listener.host
        const before = { host: this.listener.host, port: this.listener.port }
        const after = { host: noop ? this.listener.host : host, port: this.listener.port }
        return json(route, { previewToken, expiresAt: new Date(Date.now() + 300_000).toISOString(), before, after, noop, reconnectClassification: noop ? 'no-op' : 'loopback-to-lan', restartRequired: !noop, sessionInvalidated: !noop, loginRequired: !noop })
      }
      case '/api/v1/panel/listener/apply': {
        const pending = await this.applyPreview(route, entry, 'listener')
        if (pending?.owner !== 'listener') return pending
        const before = { host: this.listener.host, port: this.listener.port }
        const after = { host: pending.candidate, port: this.listener.port }
        this.reconnectListenerCandidate = pending.candidate
        return json(route, { accepted: true, state: 'rebind-started', before, after, noop: false, reconnectClassification: 'loopback-to-lan', restartRequired: true, sessionInvalidated: true, loginRequired: true }, 202)
      }
      case '/api/v1/update':
        return this.recordProjection(route, updateProjection(this.update))
      case '/api/v1/notifications':
        return this.recordProjection(route, { provider: 'telegram', configured: false, enabled: false, authorityState: 'unconfigured', deliveryState: 'idle' })
      case '/api/v1/update/check': {
        const channel = body?.channel || this.update.policy.channel
        const version = channel === 'beta' ? body?.version : '1.2.3'
        this.update = {
          ...this.update,
          latestCompatibleVersion: version,
          latestChannel: channel,
          latestSource: 'github-release',
          latestSourceCommit: 'b'.repeat(40),
          lastCheckAt: new Date().toISOString(),
        }
        return json(route, updateProjection(this.update))
      }
      case '/api/v1/update/apply':
        if (Object.keys(body || {}).sort().join(',') !== 'channel,version') this.issues.push('panel update Apply payload was not the checked channel and version')
        return json(route, { accepted: true, state: 'update-attempt-started' }, 202)
      case '/api/v1/update/rollback':
        return json(route, { accepted: true, state: 'rollback-attempt-started' }, 202)
      case '/api/v1/update/policy':
        this.update = { ...this.update, policy: { ...body }, channel: body.channel, latestCompatibleVersion: null, latestChannel: null, latestSource: '' }
        return json(route, updateProjection(this.update))
      case '/api/v1/performance/policy':
        return this.recordProjection(route, performanceProjection(this.performancePolicy))
      case '/api/v1/performance/policy/preview': {
        const candidate = body
        const before = this.performancePolicy
        const changes = Object.keys(before).filter((field) => field !== 'schemaVersion' && before[field] !== candidate[field])
          .map((field) => ({ field, before: before[field], after: candidate[field] }))
        const noop = changes.length === 0
        const previewToken = this.createPreview('performance', candidate)
        return json(route, { previewToken, expiresAt: new Date(Date.now() + 300_000).toISOString(), before, after: candidate, changes, noop, restartRequired: false, nextRunTimeChanges: !noop })
      }
      case '/api/v1/performance/policy/cancel': {
        const token = body?.previewToken
        this.previewTokens.delete(token)
        this.cancelledTokens.push({ owner: 'performance', token, csrf: entry.csrf })
        return json(route, { canceled: true })
      }
      case '/api/v1/performance/policy/apply': {
        const pending = await this.applyPreview(route, entry, 'performance')
        if (pending?.owner !== 'performance') return pending
        this.performancePolicy = { ...pending.candidate }
        return json(route, { policy: this.performancePolicy, source: 'persisted', changes: [], noop: false, restartRequired: false, nextRunTimeChanged: true })
      }
      case '/api/v1/panel/listener/cancel': {
        const token = body?.previewToken
        this.previewTokens.delete(token)
        this.cancelledTokens.push({ owner: 'listener', token, csrf: entry.csrf })
        return json(route, { canceled: true })
      }
      case '/api/v1/session/password':
        this.invalidatedTokens.push(...this.previewTokens.keys())
        this.sessionSequence++
        this.previewTokens.clear()
        return json(route, { authenticated: false, state: 'reauthentication-required' })
      default:
        this.issues.push(`unexpected synthetic request: ${entry.method} ${path}`)
        return json(route, { error: 'unexpected synthetic route' }, 404)
    }
  }
}

export async function mountFeatureCompleteDashboard(page, options = {}) {
  const model = new FeatureCompleteModel(options)
  page.on('pageerror', (error) => model.issues.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    if (['error', 'warning'].includes(message.type()) && !message.text().startsWith('Failed to load resource:')) {
      model.issues.push(`${message.type()}: ${message.text()}`)
    }
  })
  await page.route('**/api/v1/**', (route) => model.handle(route))
  return model
}

export const featureCompleteRequests = (model, path, method) => model.requests.filter((request) => request.path === path && (!method || request.method === method))
export { PRIVATE_SENTINELS }
