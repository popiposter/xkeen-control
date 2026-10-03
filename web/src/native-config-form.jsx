import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Field, FieldLabel, FieldGroup, FieldSet, FieldLegend } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
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
  return <Textarea id={id} rows={3} maxLength={128 << 10} disabled={disabled} value={buffer} onChange={(event) => {
    const text = event.target.value
    try {
      const next = list(text)
      if (onChange(next) === false) return
      seen.current = JSON.stringify(next); setBuffer(text)
    } catch (error) { onError(error.message) }
  }} />
}

export function NativeConfigForm({ file, text, tree, disabled, onChange, onError, request, targets = [] }) {
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
    <h3 className="font-semibold">DNS resolvers</h3>
    <p className="text-sm text-muted-foreground">Domain/geosite matches select resolvers. Network routing of resolver requests is separate; local transports bypass routing.</p>
    <p className="text-sm text-muted-foreground">For split DNS, use a direct default resolver and domain/geosite matches on the VPN resolver. Route that resolver's traffic separately. IP-only traffic rules do not identify names before resolution. A resolver's hostname needs a reachable bootstrap path; avoid depending on the same unresolved VPN connection.</p>
    {stringField(['dns', 'tag'], 'DNS traffic tag for routing')}
    {stringField(['dns', 'disableFallbackIfMatch'], 'Disable fallback after a domain match', { boolean: true })}
    {array(['dns', 'servers']).slice(0, 32).map((server, index) => <FieldSet key={index} className="flex flex-col gap-3 rounded-lg border p-3"><FieldLegend>Resolver {index + 1}</FieldLegend>
      {server.type === 'string' ? stringField(['dns', 'servers', index], `Resolver ${index + 1} address`) : server.type === 'object' ? <>
        {stringField(['dns', 'servers', index, 'address'], `Resolver ${index + 1} address`)}
        {stringField(['dns', 'servers', index, 'domains'], `Resolver ${index + 1} domain / geosite matches (one per line)`, { multiline: true })}
        {stringField(['dns', 'servers', index, 'expectedIPs'], `Resolver ${index + 1} expected IP / geoip matches (one per line)`, { multiline: true })}
        {stringField(['dns', 'servers', index, 'skipFallback'], `Resolver ${index + 1} exclude from fallback`, { boolean: true })}
        {stringField(['dns', 'servers', index, 'queryStrategy'], `Resolver ${index + 1} address family`, { choices: ['UseIP', 'UseIPv4', 'UseIPv6'] })}
        {stringField(['dns', 'servers', index, 'disableCache'], `Resolver ${index + 1} disable cache`, { boolean: true })}
        {stringField(['dns', 'servers', index, 'tag'], `Resolver ${index + 1} traffic tag`)}
      </> : <p>Custom resolver; edit in Text.</p>}
      <Button className="self-start" variant="outline" disabled={disabled} onClick={() => edit(['dns', 'servers', index], undefined)}>Remove resolver {index + 1}</Button>
    </FieldSet>)}
    {array(['dns', 'servers']).length > 32 && <p>Additional resolvers are available in Text mode.</p>}
    <Button className="self-start" variant="outline" disabled={disabled} onClick={() => add(['dns', 'servers'], { address: '', domains: [], skipFallback: false })}>Add resolver</Button>
  </div>
  if (file === '05_routing.json') return <div className="flex flex-col gap-4">
    <Button className="self-start" variant="outline" disabled={disabled} onClick={() => setShowExample(!showExample)}>{showExample ? 'Close routing example' : 'Check a routing example'}</Button>
    {showExample && <Suspense fallback={<p>Loading routing example…</p>}><RoutingExample text={text} request={request} disabled={disabled} /></Suspense>}
    <Button className="self-start" variant="outline" disabled={disabled} onClick={() => setBrowseGeodata(!browseGeodata)}>{browseGeodata ? 'Close geodata browser' : 'Browse installed geodata'}</Button>
    {browseGeodata && <Suspense fallback={<p>Loading geodata browser…</p>}><GeodataBrowser request={request} disabled={disabled} targets={[...targets, ...array(['routing', 'balancers']).map((_, index) => ({ kind: 'balancer', tag: value(['routing', 'balancers', index, 'tag']) })).filter((target) => typeof target.tag === 'string')]} onAdd={(rule, position) => {
      try { return onChange((position === 'first' ? prependDocumentItem : appendDocumentItem)(text, ['routing', 'rules'], rule)) } catch (error) { onError(error.message); return false }
    }} /></Suspense>}
    <h3 className="font-semibold">Traffic rules</h3><p className="text-sm text-muted-foreground">Xray uses the first matching rule. A rule's conditions are combined; different rules provide alternatives.</p>
    {array(['routing', 'rules']).slice(0, shown).map((rule, index) => <details key={index} className="disclosure" open={index === 0 || undefined}><summary>Rule {index + 1} - {value(['routing', 'rules', index, 'balancerTag']) || value(['routing', 'rules', index, 'outboundTag']) || 'No destination'} - {(value(['routing', 'rules', index, 'domain']) || []).length} domains / {(value(['routing', 'rules', index, 'ip']) || []).length} IP entries</summary><div className="disclosure-body flex flex-col gap-4">
      {rule.type === 'object' && value(['routing', 'rules', index, 'type']) === 'field' ? <>
        {stringField(['routing', 'rules', index, 'domain'], `Rule ${index + 1} domains / geosite (one per line)`, { multiline: true })}
        {stringField(['routing', 'rules', index, 'ip'], `Rule ${index + 1} IP / CIDR / geoip (one per line)`, { multiline: true })}
        <details className="disclosure"><summary>Advanced match conditions</summary><div className="disclosure-body flex flex-col gap-4">{stringField(['routing', 'rules', index, 'network'], `Rule ${index + 1} network`, { choices: ['tcp', 'udp', 'tcp,udp'] })}{stringField(['routing', 'rules', index, 'port'], `Rule ${index + 1} destination ports`)}{stringField(['routing', 'rules', index, 'protocol'], `Rule ${index + 1} detected protocols (one per line)`, { multiline: true })}{stringField(['routing', 'rules', index, 'inboundTag'], `Rule ${index + 1} inbound tags (one per line)`, { multiline: true })}</div></details>
        {value(['routing', 'rules', index, 'balancerTag']) !== undefined ? stringField(['routing', 'rules', index, 'balancerTag'], `Rule ${index + 1} balancer tag`, { choices: array(['routing', 'balancers']).map((_, n) => value(['routing', 'balancers', n, 'tag'])).filter((tag) => typeof tag === 'string') }) : stringField(['routing', 'rules', index, 'outboundTag'], `Rule ${index + 1} outbound tag (VPN / direct / block)`, { choices: targets.filter((target) => target.kind === 'outbound').map((target) => target.tag) })}
      </> : <p>Custom rule preserved; edit its fields in Text.</p>}
      <div className="flex flex-wrap gap-2"><Button className="self-start" variant="outline" disabled={disabled || index === 0} onClick={() => move(['routing', 'rules'], index, index - 1)}>Move rule {index + 1} up</Button><Button className="self-start" variant="outline" disabled={disabled || index + 1 >= array(['routing', 'rules']).length} onClick={() => move(['routing', 'rules'], index, index + 1)}>Move rule {index + 1} down</Button><Button className="self-start" variant="outline" disabled={disabled} onClick={() => edit(['routing', 'rules', index], undefined)}>Remove rule {index + 1}</Button></div>
    </div></details>)}
    {shown < array(['routing', 'rules']).length && <Button className="self-start" variant="outline" onClick={() => setShown(shown + 12)}>Show more rules</Button>}
    <Button className="self-start" variant="outline" disabled={disabled} onClick={() => add(['routing', 'rules'], { type: 'field', domain: [], outboundTag: 'direct' })}>Add traffic rule</Button>
    <h3 className="font-semibold">Native balancers</h3>
    {array(['routing', 'balancers']).slice(0, 32).map((balancer, index) => <FieldSet key={index} className="flex flex-col gap-3 rounded-lg border p-3"><FieldLegend>Balancer {index + 1}</FieldLegend>
      {balancer.type === 'object' ? <>{stringField(['routing', 'balancers', index, 'tag'], `Balancer ${index + 1} tag`)}{stringField(['routing', 'balancers', index, 'selector'], `Balancer ${index + 1} outbound prefixes (one per line)`, { multiline: true })}{stringField(['routing', 'balancers', index, 'strategy', 'type'], `Balancer ${index + 1} strategy`, { choices: ['random', 'roundRobin', 'leastPing', 'leastLoad'] })}{stringField(['routing', 'balancers', index, 'fallbackTag'], `Balancer ${index + 1} fallback outbound tag`)}</> : <p>Custom balancer; edit in Text.</p>}
    </FieldSet>)}
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
