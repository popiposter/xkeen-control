import { Modal, RowAction } from './ui'
import { DestinationBadge } from './status-ui'
import { IconPlus, IconSearch, IconPencil, IconShieldLock, IconWorld, IconBan, IconSitemap } from '@tabler/icons-react'
import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Field, FieldLabel, FieldGroup, FieldSet, FieldLegend } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
import { BalancerSummary, DnsOverview, RoutingOverview, RuleArrow, RuleConditions, SectionHeading, ruleConditions } from './native-config-visual.jsx'
import { IconRoute, IconServer } from '@tabler/icons-react'
import { appendDocumentItem, documentNode, editDocumentPath, moveDocumentItem, nodeValue, prependDocumentItem } from './native-config-document'

const GeodataBrowser = lazy(() => import('./native-geodata.jsx'))
const RoutingExample = lazy(() => import('./native-routing-example.jsx'))

function lines(value) { return Array.isArray(value) && value.every((item) => typeof item === 'string') ? value.join('\n') : '' }
function list(value) {
  const result = value.split('\n').map((item) => item.trim()).filter(Boolean)
  if (result.length > 2048) throw new Error('At most 2048 entries per list. Use Text for larger native lists.')
  return result
}

function StringListField({ id, value, disabled, onChange, onError }) {
  const signature = JSON.stringify(value)
  const seen = useRef(signature)
  const [buffer, setBuffer] = useState(() => lines(value))
  useEffect(() => { if (seen.current !== signature) { seen.current = signature; setBuffer(lines(value)) } }, [signature])
  return <Textarea className="max-h-48 overflow-y-auto" id={id} rows={3} maxLength={128 << 10} disabled={disabled} value={buffer} onChange={(event) => {
    const text = event.target.value
    try {
      const next = list(text)
      if (onChange(next) === false) return
      seen.current = JSON.stringify(next); setBuffer(text)
    } catch (error) { onError(error.message) }
  }} />
}

export function NativeConfigForm({ file, text, tree, disabled, onChange, onError, request, targets = [] }) {
  const [editingRule, setEditingRule] = useState(null)
  const [editingResolver, setEditingResolver] = useState(null)
  const [shown, setShown] = useState(12)
  const [browseGeodata, setBrowseGeodata] = useState(false)
  const [showExample, setShowExample] = useState(false)
  const value = (path) => nodeValue(tree, path)
  const node = (path) => documentNode(tree, path)
  const edit = (path, next) => { try { return onChange(editDocumentPath(text, path, next)) } catch (error) { onError(error.message); return false } }
  const add = (path, item) => { try { onChange(appendDocumentItem(text, path, item)) } catch (error) { onError(error.message) } }
  const move = (path, index, next) => { try { onChange(moveDocumentItem(text, path, index, next)) } catch (error) { onError(error.message) } }
  const stringField = (path, label, { multiline = false, choices, boolean = false, number = false, min = 0, max = 65535 } = {}) => {
    const current = value(path)
    const id = `native-${path.join('-')}`
    if (multiline && current !== undefined && (!Array.isArray(current) || current.some((item) => typeof item !== 'string'))) return <p key={id}>{label}: custom native value; edit in Text.</p>
    if (!multiline && current !== undefined && (typeof current !== (number ? 'number' : boolean ? 'boolean' : 'string') || number && !Number.isSafeInteger(current))) return <p key={id}>{label}: custom native value; edit in Text.</p>
    return <Field key={id}><FieldLabel htmlFor={id}>{label}</FieldLabel>{multiline ? <StringListField id={id} value={current} disabled={disabled} onChange={(next) => edit(path, next)} onError={onError} /> : choices || boolean ? <NativeSelect id={id} disabled={disabled} value={String(current ?? '')} onChange={(event) => edit(path, event.target.value === '' ? undefined : boolean ? event.target.value === 'true' : event.target.value)}><option value="">Native default / unspecified</option>{choices && current && !choices.includes(current) && <option value={current}>{current} (native)</option>}{(choices || ['false', 'true']).map((option) => <option key={option} value={option}>{boolean ? option === 'true' ? 'Yes' : 'No' : option}</option>)}</NativeSelect> : <Input id={id} type={number ? 'number' : 'text'} min={number ? min : undefined} max={number ? max : undefined} maxLength={2048} disabled={disabled} value={current ?? ''} onChange={(event) => { const next = number ? event.target.value === '' ? undefined : Number(event.target.value) : event.target.value; if (number && next !== undefined && (!Number.isSafeInteger(next) || next < min || next > max)) { onError(`Enter a whole number between ${min} and ${max}.`); return }; edit(path, next) }} />}</Field>
  }
  const array = (path) => node(path)?.type === 'array' ? node(path).children || [] : []
  if (file === '02_dns.json') return <div className="flex flex-col gap-4">
    <DnsOverview servers={array(['dns', 'servers']).length} />
    <SectionHeading icon={IconServer} title="Xray resolvers" count={array(['dns', 'servers']).length} />
    {!array(['dns', 'servers']).length && <p className="text-sm text-muted-foreground">No resolver configured: Xray uses the router’s DNS. This is the recommended setting.</p>}
    <details className="disclosure"><summary>Advanced DNS behaviour</summary><div className="disclosure-body flex flex-col gap-3"><p className="text-sm text-muted-foreground">With several resolvers, a resolver’s domain / geosite matches decide which one answers a name; the first resolver is the default. How resolver traffic itself is routed is separate, and a resolver given by hostname needs a path that does not depend on the VPN it serves.</p><div className="grid gap-4 sm:grid-cols-2">{stringField(['dns', 'tag'], 'DNS traffic tag for routing')}
    {stringField(['dns', 'disableFallbackIfMatch'], 'Disable fallback after a domain match', { boolean: true })}</div></div></details>
    {array(['dns', 'servers']).slice(0, 32).map((server, index) => <div key={index} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4"><div className="flex min-w-0 items-start gap-3"><IconShieldLock className="shrink-0 text-info" /><div><strong className="break-all">{server.type === 'string' ? value(['dns', 'servers', index]) : value(['dns', 'servers', index, 'address']) || 'New resolver'}</strong><p className="text-sm text-muted-foreground">{(value(['dns', 'servers', index, 'domains']) || []).length ? `${value(['dns', 'servers', index, 'domains']).length} domain/category matches` : 'Default resolver'} · {value(['dns', 'servers', index, 'tag']) || 'Native transport'}</p></div></div><Button variant="outline" disabled={disabled} onClick={() => setEditingResolver(index)}><IconPencil />Edit resolver {index + 1}</Button>{editingResolver === index && <Modal label={`Resolver ${index + 1}`} busy={disabled} onCancel={() => setEditingResolver(null)}><h3 className="text-lg font-semibold">Resolver {index + 1}</h3><div className="flex flex-col gap-4">
      {server.type === 'string' ? stringField(['dns', 'servers', index], `Resolver ${index + 1} address`) : server.type === 'object' ? <>
        {stringField(['dns', 'servers', index, 'address'], `Resolver ${index + 1} address`)}
        {stringField(['dns', 'servers', index, 'domains'], `Resolver ${index + 1} domain / geosite matches (one per line)`, { multiline: true })}
        {stringField(['dns', 'servers', index, 'expectedIPs'], `Resolver ${index + 1} expected IP / geoip matches (one per line)`, { multiline: true })}
        {stringField(['dns', 'servers', index, 'skipFallback'], `Resolver ${index + 1} exclude from fallback`, { boolean: true })}
        {stringField(['dns', 'servers', index, 'queryStrategy'], `Resolver ${index + 1} address family`, { choices: ['UseIP', 'UseIPv4', 'UseIPv6'] })}
        {stringField(['dns', 'servers', index, 'disableCache'], `Resolver ${index + 1} disable cache`, { boolean: true })}
        {stringField(['dns', 'servers', index, 'tag'], `Resolver ${index + 1} traffic tag`)}
      </> : <p>Custom resolver; edit in Text.</p>}
      <Button className="self-start" variant="outline" disabled={disabled} onClick={() => { edit(['dns', 'servers', index], undefined); setEditingResolver(null) }}>Remove resolver {index + 1}</Button>
    <Button className="self-start" onClick={() => setEditingResolver(null)}>Done</Button></div></Modal>}</div>)}
    {array(['dns', 'servers']).length > 32 && <p>Additional resolvers are available in Text mode.</p>}
    <div className="flex flex-wrap gap-2"><Button variant="outline" disabled={disabled} onClick={() => { const index = array(['dns', 'servers']).length; add(['dns', 'servers'], { address: '', domains: [], skipFallback: false }); setEditingResolver(index) }}><IconPlus />Add resolver</Button></div>
  </div>
  if (file === '05_routing.json') return <div className="flex flex-col gap-5">
    <RoutingOverview />
    <section className="flex flex-col gap-3 rounded-xl border p-4">
    <SectionHeading icon={IconRoute} title="Traffic rules" count={array(['routing', 'rules']).length}>
    <Button variant="outline" size="sm" disabled={disabled} onClick={() => setShowExample(!showExample)}>{showExample ? 'Close routing example' : 'Check a routing example'}</Button>
    <Button variant="outline" size="sm" disabled={disabled} onClick={() => setBrowseGeodata(!browseGeodata)}>{browseGeodata ? 'Close geodata browser' : 'Browse installed geodata'}</Button>
    </SectionHeading>
    <p className="text-sm text-muted-foreground">Checked top to bottom; the first rule whose conditions all match decides. System rules keep the panel and forced-VPN devices working; change them only on purpose.</p>
    {showExample && <Modal label="Check routing example" busy={disabled} onCancel={() => setShowExample(false)}><Suspense fallback={<p>Loading routing example…</p>}><RoutingExample text={text} request={request} disabled={disabled} /></Suspense><Button variant="outline" onClick={() => setShowExample(false)}>Close example</Button></Modal>}
    {browseGeodata && <Modal label="Search installed geodata" busy={disabled} onCancel={() => setBrowseGeodata(false)}><Suspense fallback={<p>Loading geodata browser…</p>}><GeodataBrowser request={request} disabled={disabled} targets={[...targets, ...array(['routing', 'balancers']).map((_, index) => ({ kind: 'balancer', tag: value(['routing', 'balancers', index, 'tag']) })).filter((target) => typeof target.tag === 'string')]} onAdd={(rule, position) => {
      try { const rules = Array.isArray(rule) ? rule : [rule]; return onChange((position === 'first' ? [...rules].reverse() : rules).reduce((next, item) => (position === 'first' ? prependDocumentItem : appendDocumentItem)(next, ['routing', 'rules'], item), text)) } catch (error) { onError(error.message); return false }
    }} /></Suspense><Button className="self-start" variant="outline" onClick={() => setBrowseGeodata(false)}>Done browsing</Button></Modal>}
    {array(['routing', 'rules']).slice(0, shown).map((rule, index) => { const ruleValue = value(['routing', 'rules', index]) || {}; const tag = ruleValue.balancerTag || ruleValue.outboundTag || 'No destination'; const target = ruleValue.balancerTag ? { kind: 'balancer' } : targets.find((item) => item.tag === tag); const { system } = ruleConditions(ruleValue); return <div key={index} className={`rounded-lg border p-3 ${system ? 'bg-muted/40' : ''}`}><div className="flex flex-wrap items-start gap-3"><span className="flex size-7 shrink-0 items-center justify-center rounded-full border text-xs text-muted-foreground">{index + 1}</span><div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">{system && <Badge variant="outline">System</Badge>}<RuleConditions rule={ruleValue} /><RuleArrow /><DestinationBadge tag={tag} target={target} /></div><div className="flex flex-wrap gap-1 max-sm:w-full max-sm:justify-end"><Button size="icon" variant="outline" aria-label={`Edit rule ${index+1}`} disabled={disabled} onClick={() => setEditingRule(index)}><IconPencil /></Button><RowAction action="up" label={`Move rule ${index+1} up`} disabled={disabled || index === 0} onClick={() => move(['routing', 'rules'], index, index-1)} /><RowAction action="down" label={`Move rule ${index+1} down`} disabled={disabled || index+1 >= array(['routing', 'rules']).length} onClick={() => move(['routing', 'rules'], index, index+1)} /></div></div>{editingRule === index && <Modal label={`Edit rule ${index+1}`} busy={disabled} onCancel={() => setEditingRule(null)}><h3 className="text-lg font-semibold">Rule {index+1}</h3><div className="flex flex-col gap-4">
      {rule.type === 'object' && value(['routing', 'rules', index, 'type']) === 'field' ? <>
        {stringField(['routing', 'rules', index, 'domain'], `Rule ${index + 1} domains / geosite (one per line)`, { multiline: true })}
        {stringField(['routing', 'rules', index, 'ip'], `Rule ${index + 1} IP / CIDR / geoip (one per line)`, { multiline: true })}
        <div className="grid gap-4 sm:grid-cols-2">{stringField(['routing', 'rules', index, 'network'], `Rule ${index + 1} network`, { choices: ['tcp', 'udp', 'tcp,udp'] })}{stringField(['routing', 'rules', index, 'port'], `Rule ${index + 1} destination ports`)}{stringField(['routing', 'rules', index, 'protocol'], `Rule ${index + 1} detected protocols (one per line)`, { multiline: true })}{stringField(['routing', 'rules', index, 'inboundTag'], `Rule ${index + 1} inbound tags (one per line)`, { multiline: true })}</div>
        <Field><FieldLabel htmlFor={`rule-destination-${index}`}>Traffic destination</FieldLabel><NativeSelect id={`rule-destination-${index}`} disabled={disabled} value={`${ruleValue.balancerTag !== undefined ? 'balancer' : 'outbound'}:${tag}`} onChange={(event) => { const destination = event.target.value; const split = destination.indexOf(':'); const kind = destination.slice(0,split); const nextTag = destination.slice(split+1); try { let next = editDocumentPath(text, ['routing', 'rules', index, kind === 'balancer' ? 'outboundTag' : 'balancerTag'], undefined); next = editDocumentPath(next, ['routing', 'rules', index, kind === 'balancer' ? 'balancerTag' : 'outboundTag'], nextTag); onChange(next) } catch (error) { onError(error.message) } }}><option value={`${ruleValue.balancerTag !== undefined ? 'balancer' : 'outbound'}:${tag}`}>{tag}</option>{[...targets.filter((item) => item.kind === 'outbound'), ...array(['routing','balancers']).map((_,n) => ({ kind: 'balancer', tag: value(['routing','balancers',n,'tag']) }))].map((item) => <option key={`${item.kind}:${item.tag}`} value={`${item.kind}:${item.tag}`}>{item.kind === 'balancer' ? 'VPN pool' : item.protocol === 'freedom' ? 'DIRECT' : item.protocol === 'blackhole' ? 'BLOCK' : 'Outbound'} · {item.tag}</option>)}</NativeSelect></Field>
      </> : <p>Custom rule preserved; edit its fields in Text.</p>}
      <div className="flex gap-2"><Button onClick={() => setEditingRule(null)}>Done</Button><Button variant="destructive" disabled={disabled} onClick={() => { edit(['routing', 'rules', index], undefined); setEditingRule(null) }}>Remove rule {index+1}</Button></div>
    </div></Modal>}</div> })}
    {shown < array(['routing', 'rules']).length && <Button className="self-start" variant="outline" onClick={() => setShown(shown + 12)}>Show more rules</Button>}
    <Button className="self-start" variant="outline" disabled={disabled} onClick={() => { const index = array(['routing', 'rules']).length; add(['routing', 'rules'], { type: 'field', domain: [], outboundTag: targets.find((item) => item.protocol === 'freedom')?.tag || 'direct' }); setEditingRule(index); setShown(Math.max(shown,index+1)) }}><IconPlus />Add traffic rule</Button>
    </section>
    <section className="flex flex-col gap-3 rounded-xl border p-4">
    <SectionHeading icon={IconSitemap} title="VPN pool & balancing" count={array(['routing', 'balancers']).length} />
    <p className="text-sm text-muted-foreground">A rule with a VPN destination sends traffic to a balancer. Its members are VPN nodes; Xray picks a healthy one by the strategy. Automatic reviews in Performance keep the members and their weights up to date.</p>
    {array(['routing', 'balancers']).slice(0, 32).map((balancer, index) => <div key={index} className="flex flex-col gap-3 rounded-lg border p-3">
      <BalancerSummary balancer={value(['routing', 'balancers', index]) || {}} exact={(tag) => targets.some((item) => item.tag === tag)} />
      <details className="disclosure"><summary>Edit balancer {index + 1}</summary><div className="disclosure-body flex flex-col gap-3">
      {balancer.type === 'object' ? <>{stringField(['routing', 'balancers', index, 'tag'], `Balancer ${index + 1} tag`)}{stringField(['routing', 'balancers', index, 'selector'], `Balancer ${index + 1} outbound prefixes (one per line)`, { multiline: true })}{stringField(['routing', 'balancers', index, 'strategy', 'type'], `Balancer ${index + 1} strategy`, { choices: ['random', 'roundRobin', 'leastPing', 'leastLoad'] })}{stringField(['routing', 'balancers', index, 'fallbackTag'], `Balancer ${index + 1} fallback outbound tag`)}</> : <p>Custom balancer; edit in Text.</p>}
    </div></details></div>)}
    </section>
  </div>
  if (file === '07_observatory.json') return <div className="flex flex-col gap-3"><h3 className="font-semibold">Native health probes</h3>{stringField(['observatory', 'subjectSelector'], 'Probed outbound prefixes (one per line)', { multiline: true })}{stringField(['observatory', value(['observatory', 'probeURL']) !== undefined && value(['observatory', 'probeUrl']) === undefined ? 'probeURL' : 'probeUrl'], 'Probe URL')}</div>
  if (file === '01_log.json') return <FieldGroup>
    <h3 className="font-semibold">Logging</h3><p className="text-sm text-muted-foreground">Choose diagnostic detail and the native log destinations. Extra logging uses router storage.</p>
    {stringField(['log', 'loglevel'], 'Log detail', { choices: ['none', 'error', 'warning', 'info', 'debug'] })}
    {stringField(['log', 'access'], 'Access log destination')}{stringField(['log', 'error'], 'Error log destination')}
    {stringField(['log', 'dnsLog'], 'Log DNS requests', { boolean: true })}
  </FieldGroup>
  if (file === '03_inbounds.json' || file === '08_api.json') return <FieldGroup>
    {file === '08_api.json' && <><h3 className="font-semibold">Local Xray API</h3><p className="text-sm text-muted-foreground">The panel uses local routing, health and statistics services. Keep API listeners on loopback.</p>{stringField(['api', 'tag'], 'API routing tag')}{stringField(['api', 'services'], 'Enabled API services (one per line)', { multiline: true })}</>}
    <h3 className="font-semibold">Traffic listeners</h3>
    {array(['inbounds']).slice(0, 32).map((inbound, index) => <FieldSet key={index} className="rounded-lg border p-4"><FieldLegend>Listener {index + 1} - {value(['inbounds', index, 'tag']) || 'unnamed'}</FieldLegend><FieldGroup>
      {inbound.type === 'object' ? <>
        {stringField(['inbounds', index, 'tag'], `Listener ${index + 1} tag`)}
        {stringField(['inbounds', index, 'protocol'], `Listener ${index + 1} protocol`, { choices: ['dokodemo-door', 'socks', 'http', 'tunnel'] })}
        {stringField(['inbounds', index, 'listen'], `Listener ${index + 1} bind address`)}
        {stringField(['inbounds', index, 'port'], `Listener ${index + 1} port`, { number: true, min: 1 })}
        {stringField(['inbounds', index, 'settings', 'network'], `Listener ${index + 1} network`, { choices: ['tcp', 'udp', 'tcp,udp'] })}
        {stringField(['inbounds', index, 'settings', 'followRedirect'], `Listener ${index + 1} follow transparent redirection`, { boolean: true })}
        {stringField(['inbounds', index, 'sniffing', 'enabled'], `Listener ${index + 1} detect destination names`, { boolean: true })}
        {stringField(['inbounds', index, 'sniffing', 'routeOnly'], `Listener ${index + 1} use detected names only for routing`, { boolean: true })}
      </> : <p>Custom listener preserved. Edit it in Text.</p>}
    </FieldGroup></FieldSet>)}
    {array(['inbounds']).length > 32 && <p>Additional listeners remain available in Text.</p>}
  </FieldGroup>
  if (file === '06_policy.json') return <FieldGroup>
    <h3 className="font-semibold">Connection policy</h3><p className="text-sm text-muted-foreground">Native timeout and statistics settings. Values are seconds; zero keeps the native meaning.</p>
    {Object.keys(value(['policy', 'levels']) || {}).slice(0, 32).map((level) => <FieldSet key={level} className="rounded-lg border p-4"><FieldLegend>Level {level}</FieldLegend><FieldGroup>
      {['handshake', 'connIdle', 'uplinkOnly', 'downlinkOnly'].map((name) => stringField(['policy', 'levels', level, name], `${name} (seconds) - level ${level}`, { number: true, max: 86400 }))}
      {stringField(['policy', 'levels', level, 'statsUserUplink'], `Upload statistics - level ${level}`, { boolean: true })}{stringField(['policy', 'levels', level, 'statsUserDownlink'], `Download statistics - level ${level}`, { boolean: true })}
    </FieldGroup></FieldSet>)}
    {stringField(['policy', 'system', 'statsInboundUplink'], 'Inbound upload statistics', { boolean: true })}{stringField(['policy', 'system', 'statsInboundDownlink'], 'Inbound download statistics', { boolean: true })}
  </FieldGroup>
  return <p className="text-sm text-muted-foreground">This native config is available in Text mode.</p>
}
