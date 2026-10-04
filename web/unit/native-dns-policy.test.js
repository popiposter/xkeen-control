import assert from 'node:assert/strict'
import test from 'node:test'
import { splitDNSDocuments } from '../src/native-dns-policy.js'
import { inspectDocument, nodeValue } from '../src/native-config-document.js'

const targets=[{tag:'direct',kind:'outbound',protocol:'freedom'}]
const routing='{/* native */"routing":{"rules":[{/* preserve */"type":"field","domain":["domain:example.test"],"balancerTag":"pool","opaque":9007199254740993},{"type":"field","ip":["geoip:private"],"outboundTag":"direct"}],"balancers":[{"tag":"pool","selector":["proxy-"]}]}}'

test('split DNS keeps native opaque routing bytes and uses tagged routed DoH with local default',()=>{
 const result=splitDNSDocuments('{/* dns */"future":9007199254740993}',routing,targets)
 const dns=nodeValue(inspectDocument(result.documents['02_dns.json']).tree,['dns'])
 assert.deepEqual(dns.servers.map((server)=>typeof server==='string'?server:server.address),['https://1.1.1.1/dns-query','https://8.8.8.8/dns-query','localhost'])
 assert.equal(dns.disableFallbackIfMatch,true)
 assert.equal(dns.servers[0].tag,'panel-dns-vpn')
 assert.ok(result.documents['05_routing.json'].includes('/* preserve */'))
 assert.ok(result.documents['05_routing.json'].includes('9007199254740993'))
 assert.ok(result.documents['02_dns.json'].includes('9007199254740993'))
 const repeated=splitDNSDocuments(result.documents['02_dns.json'],result.documents['05_routing.json'],targets)
 assert.equal(nodeValue(inspectDocument(repeated.documents['05_routing.json']).tree,['routing','rules']).length,4)
})

test('IP-only routing cannot be silently treated as a DNS domain policy',()=>{
 assert.throws(()=>splitDNSDocuments('{}','{"routing":{"rules":[{"type":"field","ip":["geoip:ru"],"balancerTag":"pool"}],"balancers":[{"tag":"pool"}]}}',targets),/IP rules/)
})

test('a custom collision with the DNS rule marker is not overwritten',()=>{
 assert.throws(()=>splitDNSDocuments('{}',routing.replace('"rules":[','"rules":[{"ruleTag":"panel-dns-vpn","inboundTag":["other"],"outboundTag":"direct"},'),targets),/custom rule/)
})
