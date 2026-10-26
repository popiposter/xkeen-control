// Pure helpers for routing rules added from the panel. They never touch the
// document text; native-config-form.jsx applies their result as one edit.

// Inbounds the panel and the forced-VPN path own: a user rule never targets
// them, and a rule that names none of its inbounds would also capture them.
export const systemInbounds = new Set(['api', 'probe', 'force-proxy-redirect', 'force-proxy-tproxy'])

// The XKeen interception inbounds: LAN TCP, LAN UDP and the local SOCKS port.
export const defaultTrafficInbounds = ['redirect', 'tproxy', 'socks']

// trafficInbounds returns every non-system inbound the draft's rules name, in
// first-seen order, so a new rule covers all user traffic those rules cover
// (LAN and SOCKS alike). A system tag in a mixed list is dropped on its own.
// With none named, the XKeen interception defaults apply.
export function trafficInbounds(rules = []) {
  const seen = new Set()
  const tags = []
  for (const rule of rules) {
    if (!Array.isArray(rule?.inboundTag)) continue
    for (const tag of rule.inboundTag) {
      if (typeof tag !== 'string' || !tag || systemInbounds.has(tag) || seen.has(tag)) continue
      seen.add(tag)
      tags.push(tag)
    }
  }
  return tags.length ? tags : [...defaultTrafficInbounds]
}

// everyNetwork reports a network condition that admits both TCP and UDP.
function everyNetwork(value) {
  if (value === undefined || value === null || value === '') return true
  const parts = new Set(String(value).toLowerCase().split(',').map((part) => part.trim()).filter(Boolean))
  return parts.has('tcp') && parts.has('udp')
}

// catchAllRule reports a rule that matches every connection reaching it
// through its inbounds: no destination, source, protocol or other condition,
// and a network condition (if any) that admits both TCP and UDP. Rules after
// it never run.
export function catchAllRule(rule) {
  if (!rule || typeof rule !== 'object' || Array.isArray(rule)) return false
  if (!everyNetwork(rule.network)) return false
  return Object.keys(rule).every((key) => ['type', 'outboundTag', 'balancerTag', 'ruleTag', 'inboundTag', 'network'].includes(key) || rule[key] === undefined || rule[key] === null || Array.isArray(rule[key]) && !rule[key].length)
}

const ipv4 = /^(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)$/
const ipv6 = /^[0-9a-f]{0,4}(?::[0-9a-f]{0,4}){2,7}$/i
const hostname = /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$/i

// membershipKind tells which databases can answer "which categories contain
// this?" by native matching (CIDR containment, domain/regexp rules) rather
// than by text: "ip" for an IP address (geoip files), "domain" for a host
// name (geosite files), "" otherwise.
export function membershipKind(text) {
  const value = String(text || '').trim().replace(/\.$/, '')
  if (ipv4.test(value) || value.includes(':') && ipv6.test(value)) return 'ip'
  if (hostname.test(value)) return 'domain'
  return ''
}
