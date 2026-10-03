import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
import { appendDocumentItem, documentNode, editDocumentPath, moveDocumentItem, nodeValue, prependDocumentItem } from './native-config-document'

const GeodataBrowser = lazy(() => import('./native-geodata.jsx'))

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
  const value = (path) => nodeValue(tree, path)
  const node = (path) => documentNode(tree, path)
  const edit = (path, next) => { try { return onChange(editDocumentPath(text, path, next)) } catch (error) { onError(error.message); return false } }
  const add = (path, item) => { try { onChange(appendDocumentItem(text, path, item)) } catch (error) { onError(error.message) } }
  const move = (path, index, next) => { try { onChange(moveDocumentItem(text, path, index, next)) } catch (error) { onError(error.message) } }
  const stringField = (path, label, { multiline = false, choices, boolean = false } = {}) => {
    const current = value(path)
    const id = `native-${path.join('-')}`
    if (multiline && current !== undefined && (!Array.isArray(current) || current.some((item) => typeof item !== 'string'))) return <p key={id}>{label}: custom native value; edit in Text.</p>
    if (!multiline && current !== undefined && typeof current !== (boolean ? 'boolean' : 'string')) return <p key={id}>{label}: custom native value; edit in Text.</p>
    return <Field key={id}><FieldLabel htmlFor={id}>{label}</FieldLabel>{multiline ? <StringListField id={id} value={current} disabled={disabled} onChange={(next) => edit(path, next)} onError={onError} /> : choices || boolean ? <NativeSelect id={id} disabled={disabled} value={String(current ?? '')} onChange={(event) => edit(path, boolean ? event.target.value === 'true' : event.target.value)}><option value="" disabled>Native default / unspecified</option>{(choices || ['false', 'true']).map((option) => <option key={option} value={option}>{boolean ? option === 'true' ? 'Yes' : 'No' : option}</option>)}</NativeSelect> : <Input id={id} maxLength={2048} disabled={disabled} value={current ?? ''} onChange={(event) => edit(path, event.target.value)} />}</Field>
  }
  const array = (path) => node(path)?.type === 'array' ? node(path).children || [] : []
  if (file === '02_dns.json') return <div className="space-y-4">
    <h3 className="font-semibold">DNS resolvers</h3>
    <p className="text-sm text-muted-foreground">Domain/geosite matches select resolvers. Network routing of resolver requests is separate; local transports bypass routing.</p>
    {stringField(['dns', 'tag'], 'DNS traffic tag for routing')}
    {stringField(['dns', 'disableFallbackIfMatch'], 'Disable fallback after a domain match', { boolean: true })}
    {array(['dns', 'servers']).slice(0, 32).map((server, index) => <fieldset key={index} className="space-y-3 rounded-lg border p-3"><legend>Resolver {index + 1}</legend>
      {server.type === 'string' ? stringField(['dns', 'servers', index], `Resolver ${index + 1} address`) : server.type === 'object' ? <>
        {stringField(['dns', 'servers', index, 'address'], `Resolver ${index + 1} address`)}
        {stringField(['dns', 'servers', index, 'domains'], `Resolver ${index + 1} domain / geosite matches (one per line)`, { multiline: true })}
        {stringField(['dns', 'servers', index, 'expectedIPs'], `Resolver ${index + 1} expected IP / geoip matches (one per line)`, { multiline: true })}
        {stringField(['dns', 'servers', index, 'skipFallback'], `Resolver ${index + 1} exclude from fallback`, { boolean: true })}
      </> : <p>Custom resolver; edit in Text.</p>}
      <Button variant="outline" disabled={disabled} onClick={() => edit(['dns', 'servers', index], undefined)}>Remove resolver {index + 1}</Button>
    </fieldset>)}
    {array(['dns', 'servers']).length > 32 && <p>Additional resolvers are available in Text mode.</p>}
    <Button variant="outline" disabled={disabled} onClick={() => add(['dns', 'servers'], { address: '', domains: [], skipFallback: false })}>Add resolver</Button>
  </div>
  if (file === '05_routing.json') return <div className="space-y-4">
    <Button variant="outline" disabled={disabled} onClick={() => setBrowseGeodata(!browseGeodata)}>{browseGeodata ? 'Close geodata browser' : 'Browse installed geodata'}</Button>
    {browseGeodata && <Suspense fallback={<p>Loading geodata browser…</p>}><GeodataBrowser request={request} disabled={disabled} targets={[...targets, ...array(['routing', 'balancers']).map((_, index) => ({ kind: 'balancer', tag: value(['routing', 'balancers', index, 'tag']) })).filter((target) => typeof target.tag === 'string')]} onAdd={(rule, position) => {
      try { return onChange((position === 'first' ? prependDocumentItem : appendDocumentItem)(text, ['routing', 'rules'], rule)) } catch (error) { onError(error.message); return false }
    }} /></Suspense>}
    <h3 className="font-semibold">Traffic rules</h3><p className="text-sm text-muted-foreground">Xray uses the first matching rule. A rule's conditions are combined; different rules provide alternatives.</p>
    {array(['routing', 'rules']).slice(0, shown).map((rule, index) => <fieldset key={index} className="space-y-3 rounded-lg border p-3"><legend>Rule {index + 1}</legend>
      {rule.type === 'object' && value(['routing', 'rules', index, 'type']) === 'field' ? <>
        {stringField(['routing', 'rules', index, 'domain'], `Rule ${index + 1} domains / geosite (one per line)`, { multiline: true })}
        {stringField(['routing', 'rules', index, 'ip'], `Rule ${index + 1} IP / CIDR / geoip (one per line)`, { multiline: true })}
        {stringField(['routing', 'rules', index, 'inboundTag'], `Rule ${index + 1} inbound tags (one per line)`, { multiline: true })}
        {value(['routing', 'rules', index, 'balancerTag']) !== undefined ? stringField(['routing', 'rules', index, 'balancerTag'], `Rule ${index + 1} balancer tag`) : stringField(['routing', 'rules', index, 'outboundTag'], `Rule ${index + 1} outbound tag (VPN / direct / block)`)}
      </> : <p>Custom rule preserved; edit its fields in Text.</p>}
      <div className="flex flex-wrap gap-2"><Button variant="outline" disabled={disabled || index === 0} onClick={() => move(['routing', 'rules'], index, index - 1)}>Move rule {index + 1} up</Button><Button variant="outline" disabled={disabled || index + 1 >= array(['routing', 'rules']).length} onClick={() => move(['routing', 'rules'], index, index + 1)}>Move rule {index + 1} down</Button><Button variant="outline" disabled={disabled} onClick={() => edit(['routing', 'rules', index], undefined)}>Remove rule {index + 1}</Button></div>
    </fieldset>)}
    {shown < array(['routing', 'rules']).length && <Button variant="outline" onClick={() => setShown(shown + 12)}>Show more rules</Button>}
    <Button variant="outline" disabled={disabled} onClick={() => add(['routing', 'rules'], { type: 'field', domain: [], outboundTag: 'direct' })}>Add traffic rule</Button>
    <h3 className="font-semibold">Native balancers</h3>
    {array(['routing', 'balancers']).slice(0, 32).map((balancer, index) => <fieldset key={index} className="space-y-3 rounded-lg border p-3"><legend>Balancer {index + 1}</legend>
      {balancer.type === 'object' ? <>{stringField(['routing', 'balancers', index, 'tag'], `Balancer ${index + 1} tag`)}{stringField(['routing', 'balancers', index, 'selector'], `Balancer ${index + 1} outbound prefixes (one per line)`, { multiline: true })}{stringField(['routing', 'balancers', index, 'strategy', 'type'], `Balancer ${index + 1} strategy`, { choices: ['random', 'roundRobin', 'leastPing', 'leastLoad'] })}{stringField(['routing', 'balancers', index, 'fallbackTag'], `Balancer ${index + 1} fallback outbound tag`)}</> : <p>Custom balancer; edit in Text.</p>}
    </fieldset>)}
  </div>
  if (file === '07_observatory.json') return <div className="space-y-3"><h3 className="font-semibold">Native health probes</h3>{stringField(['observatory', 'subjectSelector'], 'Probed outbound prefixes (one per line)', { multiline: true })}{stringField(['observatory', 'probeURL'], 'Probe URL')}</div>
  return <p className="text-sm text-muted-foreground">This native config is available in Text mode.</p>
}
