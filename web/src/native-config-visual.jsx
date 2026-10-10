// Read-only summaries that make the routing and DNS forms readable at a
// glance. They never edit; the editable fields stay in native-config-form.
import { IconArrowRight, IconDevices, IconFilter, IconNetwork, IconPlug, IconRoute, IconRouter, IconServer, IconSitemap, IconWorldWww } from '@tabler/icons-react'
import { Badge } from '@/components/ui/badge'

const inboundNames = { redirect: 'LAN devices', tproxy: 'LAN devices', socks: 'Local SOCKS', 'force-proxy-redirect': 'Forced VPN devices', 'force-proxy-tproxy': 'Forced VPN devices', api: 'Panel API', probe: 'Panel probes' }
const systemInbounds = new Set(['api', 'probe', 'force-proxy-redirect', 'force-proxy-tproxy'])
const privateRanges = new Set(['10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.0/8', '::1/128', 'fc00::/7', 'fe80::/10'])
const protocolNames = { bittorrent: 'BitTorrent', http: 'HTTP', tls: 'TLS', quic: 'QUIC' }

const unique = (items) => [...new Set(items)]
// Keys that are either shown as a chip or are not conditions at all.
const shownKeys = new Set(['type', 'outboundTag', 'balancerTag', 'ruleTag', 'inboundTag', 'domain', 'ip', 'protocol', 'port', 'network', 'source', 'sourcePort', 'user'])
function geodataName(item) {
  const ext = /^ext:(geosite|geoip)_?([^:]*)\.dat:(.+)$/.exec(item)
  if (ext) return ext[3]
  return item.replace(/^(geosite|geoip|domain|full):/, '')
}

// One rule as plain conditions: who, what and where it goes.
export function ruleConditions(rule = {}) {
  const chips = []
  const inbound = Array.isArray(rule.inboundTag) ? rule.inboundTag : []
  if (inbound.length) chips.push({ kind: 'from', icon: IconPlug, text: `From: ${unique(inbound.map((tag) => inboundNames[tag] || tag)).join(', ')}` })
  const list = (key) => Array.isArray(rule[key]) ? rule[key].filter((item) => typeof item === 'string') : []
  const domains = list('domain')
  if (domains.length) chips.push({ kind: 'domain', icon: IconWorldWww, text: `Sites: ${summarize(domains.map(geodataName))}` })
  const ips = list('ip')
  if (ips.length) chips.push({ kind: 'ip', icon: IconNetwork, text: ips.every((ip) => privateRanges.has(ip)) ? 'Local networks' : `IPs: ${summarize(ips.map(geodataName))}` })
  const protocols = list('protocol')
  if (protocols.length) chips.push({ kind: 'protocol', icon: IconFilter, text: `Protocol: ${summarize(protocols.map((name) => protocolNames[name] || name))}` })
  if (rule.port) chips.push({ kind: 'port', icon: IconFilter, text: `port ${rule.port}` })
  // TCP and UDP together is every connection: no condition of its own.
  if (rule.network && !/^tcp,\s*udp$|^udp,\s*tcp$/i.test(String(rule.network))) chips.push({ kind: 'network', icon: IconFilter, text: String(rule.network).toUpperCase() })
  const sources = list('source')
  if (sources.length) chips.push({ kind: 'source', icon: IconDevices, text: `Devices: ${summarize(sources.map(geodataName))}` })
  if (rule.sourcePort) chips.push({ kind: 'sourcePort', icon: IconFilter, text: `source port ${rule.sourcePort}` })
  const users = list('user')
  if (users.length) chips.push({ kind: 'user', icon: IconDevices, text: `Users: ${summarize(users)}` })
  // Any other native condition still narrows the rule: never call it all traffic.
  const other = Object.keys(rule).filter((key) => !shownKeys.has(key) && rule[key] !== undefined && rule[key] !== null && !(Array.isArray(rule[key]) && !rule[key].length))
  if (other.length) chips.push({ kind: 'other', icon: IconFilter, text: `Other conditions: ${other.join(', ')}` })
  const system = inbound.length > 0 && inbound.every((tag) => systemInbounds.has(tag))
  return { chips, system }
}

function summarize(items) {
  return items.length > 3 ? `${items.slice(0, 3).join(', ')} +${items.length - 3}` : items.join(', ')
}

export function RuleConditions({ rule }) {
  const { chips } = ruleConditions(rule)
  if (!chips.some(({ kind }) => kind !== 'from')) return <span className="flex flex-wrap items-center gap-1.5">{chips.map(({ kind, icon: Glyph, text }) => <Badge key={kind} variant="secondary" className="font-normal"><Glyph data-icon="inline-start" aria-hidden="true" />{text}</Badge>)}<span className="text-sm font-medium">{chips.length ? 'all traffic' : 'Everything else'}</span></span>
  return <span className="flex flex-wrap items-center gap-1.5">{chips.map(({ kind, icon: Glyph, text }) => <Badge key={kind} variant="secondary" className="max-w-full font-normal"><Glyph data-icon="inline-start" aria-hidden="true" /><span className="truncate">{text}</span></Badge>)}</span>
}

export function RuleArrow() { return <IconArrowRight className="size-4 shrink-0 text-muted-foreground" aria-label="goes to" /> }

const strategyNames = { leastLoad: 'Least load: speed and latency', leastPing: 'Lowest latency', random: 'Random', roundRobin: 'Round robin' }
export function BalancerSummary({ balancer = {}, exact }) {
  const selector = Array.isArray(balancer.selector) ? balancer.selector : []
  const maxRTT = balancer.strategy?.settings?.maxRTT
  return <div className="flex flex-col gap-2">
    <div className="flex flex-wrap items-center gap-2"><strong>{balancer.tag || 'Unnamed balancer'}</strong><Badge variant="outline">{strategyNames[balancer.strategy?.type] || balancer.strategy?.type || 'Native default'}</Badge>{maxRTT && <Badge variant="outline">max latency {maxRTT}</Badge>}{balancer.fallbackTag && <Badge variant="outline">fallback {balancer.fallbackTag}</Badge>}</div>
    <span className="text-sm text-muted-foreground">{selector.length > 0 && !selector.every((tag) => exact?.(tag)) ? `Nodes whose tag starts with ${selector.map((tag) => `“${tag}”`).join(', ')}` : `${selector.length} ${selector.length === 1 ? 'member' : 'members'}`}</span>
    {selector.length > 0 && <span className="flex flex-wrap gap-1.5">{selector.slice(0, 12).map((tag) => <Badge key={tag} variant="secondary" className="font-normal"><IconServer data-icon="inline-start" aria-hidden="true" />{tag}</Badge>)}{selector.length > 12 && <Badge variant="secondary">+{selector.length - 12}</Badge>}</span>}
  </div>
}

export function SectionHeading({ icon: Glyph, title, count, children }) {
  return <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="flex items-center gap-2 font-semibold"><Glyph className="size-5 text-info" aria-hidden="true" />{title}{count !== undefined && <Badge variant="secondary">{count}</Badge>}</h3>{children && <div className="flex flex-wrap gap-2">{children}</div>}</div>
}


// How name resolution actually works on this router (REQ CON-001: ordinary
// Keenetic DNS, no interception, no DNS over VLESS).
export function DnsOverview({ servers = 0 }) {
  const steps = [
    { icon: IconDevices, title: 'Devices ask the router', text: 'Phones and computers use the router’s DNS; the panel does not intercept or change these queries. Turn on encrypted DNS (DNS-over-HTTPS or DNS-over-TLS) in Keenetic so the provider cannot substitute answers for blocked sites.' },
    { icon: IconRoute, title: 'Xray reads the site name', text: 'Xray takes the site name from the connection itself (TLS, HTTP or QUIC) for name rules and checks IP rules against the address the device already resolved. It looks a name up itself only when no rule matched at all; the shipped last rule matches all remaining LAN traffic.' },
    { icon: IconServer, title: 'Xray DNS (this file)', text: servers ? `Xray uses the ${servers} resolver${servers === 1 ? '' : 's'} below for its own lookups, such as VPN node addresses.` : 'Empty, so Xray uses the router’s DNS for its own lookups, such as VPN node addresses. Add a resolver only if Xray needs a specific one.' },
  ]
  return <section aria-label="How DNS works on this router" className="grid gap-3 md:grid-cols-3">{steps.map(({ icon: Glyph, title, text }, index) => <div key={title} className="flex gap-3 rounded-xl border p-4"><span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted"><Glyph className="size-4 text-info" aria-hidden="true" /></span><div className="flex flex-col gap-1"><strong className="text-sm">{index + 1}. {title}</strong><span className="text-sm text-muted-foreground">{text}</span></div></div>)}</section>
}

export function RoutingOverview() {
  const steps = [
    { icon: IconRouter, title: 'XKeen hands traffic to Xray', text: 'Connections from LAN devices arrive at Xray, and the rules below decide where each one goes.' },
    { icon: IconRoute, title: 'Rules decide, top to bottom', text: 'The first rule whose conditions all match picks the destination. Order matters.' },
    { icon: IconSitemap, title: 'Destination', text: 'DIRECT goes straight out, VPN goes through the node pool, BLOCK drops the connection.' },
  ]
  return <section aria-label="How routing works" className="grid gap-3 md:grid-cols-3">{steps.map(({ icon: Glyph, title, text }, index) => <div key={title} className="flex gap-3 rounded-xl border p-4"><span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-muted"><Glyph className="size-4 text-info" aria-hidden="true" /></span><div className="flex flex-col gap-1"><strong className="text-sm">{index + 1}. {title}</strong><span className="text-sm text-muted-foreground">{text}</span></div></div>)}</section>
}
