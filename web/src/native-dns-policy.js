import { inspectDocument, nodeValue, editDocumentPath, prependDocumentItem } from './native-config-document.js'

export const DNS_VPN_TAG = 'panel-dns-vpn'
export const DNS_DIRECT_TAG = 'panel-dns-direct'

// A native Xray resolver policy, not DHCP/LAN DNS interception. IP-only and
// protocol rules cannot be converted into DNS names; preserve them unchanged.
export function splitDNSDocuments(dnsText, routingText, targets) {
  const dns = inspectDocument(dnsText), routing = inspectDocument(routingText)
  if (dns.error || routing.error) throw new Error('Fix JSON before preparing DNS.')
  const rules = nodeValue(routing.tree, ['routing', 'rules'])
  const balancers = nodeValue(routing.tree, ['routing', 'balancers']) || []
  if (!Array.isArray(rules)) throw new Error('Routing rules are unavailable.')
  const direct = targets.find((item) => item.kind === 'outbound' && item.protocol === 'freedom')
  const vpnRules = rules.filter((rule) => rule.type === 'field' && typeof rule.balancerTag === 'string' && balancers.some((b) => b.tag === rule.balancerTag))
  const vpn = [...new Set(vpnRules.map((rule) => rule.balancerTag))]
  if (!direct || vpn.length !== 1) throw new Error('Choose one existing VPN pool and a DIRECT outbound before preparing split DNS.')
  const domains = [...new Set(vpnRules.flatMap((rule) => Array.isArray(rule.domain) ? rule.domain.filter((d) => typeof d === 'string') : []))]
  const directDomains = [...new Set(rules.filter((rule) => rule.type === 'field' && rule.outboundTag === direct.tag).flatMap((rule) => Array.isArray(rule.domain) ? rule.domain.filter((d) => typeof d === 'string') : []))]
  if (!domains.length) throw new Error('No VPN domain/category matches found. IP rules alone cannot identify DNS names.')
  const servers = [
    ...(directDomains.length ? [{ address: 'localhost', domains: directDomains, skipFallback: true, tag: DNS_DIRECT_TAG }] : []),
    { address: 'https://1.1.1.1/dns-query', domains, skipFallback: true, tag: DNS_VPN_TAG },
    { address: 'https://8.8.8.8/dns-query', domains, skipFallback: true, tag: DNS_VPN_TAG },
    'localhost',
  ]
  let nextDNS = editDocumentPath(dnsText, ['dns', 'servers'], servers)
  nextDNS = editDocumentPath(nextDNS, ['dns', 'disableFallbackIfMatch'], true)
  // Localhost is local-only. Tagged DoH requests use Xray routing and the native
  // balancer. Literal endpoints avoid a DNS bootstrap dependency on VPN DNS.
  let nextRouting = routingText
  for (let i = rules.length-1; i >= 0; i--) {
    if (![DNS_VPN_TAG,DNS_DIRECT_TAG].includes(rules[i].ruleTag)) continue
    if (JSON.stringify(rules[i].inboundTag) !== JSON.stringify([rules[i].ruleTag])) throw new Error('A resolver rule tag is used by a custom rule. Inspect it in Routing first.')
    nextRouting = editDocumentPath(nextRouting, ['routing','rules',i], undefined)
  }
  const resolverRules = [
    { type: 'field', ruleTag: DNS_VPN_TAG, inboundTag: [DNS_VPN_TAG], balancerTag: vpn[0] },
    { type: 'field', ruleTag: DNS_DIRECT_TAG, inboundTag: [DNS_DIRECT_TAG], outboundTag: direct.tag },
  ]
  for (const rule of resolverRules.reverse()) nextRouting = prependDocumentItem(nextRouting, ['routing','rules'], rule)
  return { documents: { '02_dns.json': nextDNS, '05_routing.json': nextRouting }, vpnMatches: domains.length, directMatches: directDomains.length }
}
