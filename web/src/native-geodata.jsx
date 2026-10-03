import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'

export default function NativeGeodata({ request, disabled, targets, onAdd }) {
  const [files, setFiles] = useState([])
  const [file, setFile] = useState('')
  const [view, setView] = useState('categories')
  const [category, setCategory] = useState('')
  const [search, setSearch] = useState('')
  const [result, setResult] = useState(null)
  const [activeQuery, setActiveQuery] = useState(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [target, setTarget] = useState('')
  const [position, setPosition] = useState('first')
  const alive = useRef(true)
  const initialRequest = useRef(request)
  const currentFile = files.find((item) => item.name === file)
  const locked = disabled || busy

  useEffect(() => {
    let active = true
    alive.current = true
    setBusy(true)
    initialRequest.current('geodata').then((items) => {
      if (!active) return
      if (!Array.isArray(items) || items.length > 32 || items.some((item) => typeof item.name !== 'string' || !['geosite', 'geoip'].includes(item.kind))) throw new Error('Geodata inventory was not confirmed.')
      setFiles(items)
      setFile(items.find((item) => item.available)?.name || '')
    }).catch((error) => { if (active) setNotice(error.message) }).finally(() => { if (active) setBusy(false) })
    return () => { active = false; alive.current = false }
  }, [])

  async function query(body) {
    setBusy(true); setNotice('')
    try {
      const value = await request('geodata/query', body)
      if (!alive.current) return
      if (value.file !== body.file || !Array.isArray(value.items) || value.items.length > 100 || !/^[a-f0-9]{64}$/.test(value.snapshot) || value.offset !== body.offset || !Number.isInteger(value.total)) throw new Error('Geodata page was not confirmed.')
      setResult(value); setActiveQuery(body)
    } catch (error) { if (alive.current) { setResult(null); setNotice(error.message) } }
    finally { if (alive.current) setBusy(false) }
  }

  function addCategory(item) {
    const destination = targets.find((item) => `${item.kind}:${item.tag}` === target)
    if (!destination || !result) { setNotice('Choose a destination first.'); return }
    const kind = result.kind === 'geosite' ? 'domain' : 'ip'
    const reference = result.file === `${result.kind}.dat` ? `${result.kind}:${item.category}` : `ext:${result.file}:${item.category}`
    const rule = { type: 'field', [kind]: [reference], [destination.kind === 'balancer' ? 'balancerTag' : 'outboundTag']: destination.tag }
    if (onAdd(rule, position) !== false) setNotice('Category added to the routing draft. Save and restart to apply it.')
  }

  return <Card>
    <CardHeader><CardTitle>Installed geodata</CardTitle><CardDescription>Inspect local categories and their domain/IP matches. Reading or searching does not change routing.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      <FieldGroup>
        <Field><FieldLabel htmlFor="geo-file">Database file</FieldLabel><NativeSelect id="geo-file" value={file} disabled={locked || files.length === 0} onChange={(event) => { setFile(event.target.value); setCategory(''); setResult(null); setSearch('') }}><option value="" disabled>Select installed file</option>{files.map((item) => <option key={item.name} value={item.name} disabled={!item.available}>{item.name} · {Math.round(item.size / 1024)} KiB{item.available ? '' : ' · unavailable'}</option>)}</NativeSelect></Field>
        <Field><FieldLabel htmlFor="geo-view">Browse mode</FieldLabel><NativeSelect id="geo-view" value={view} disabled={locked} onChange={(event) => { setView(event.target.value); setSearch(''); setResult(null) }}><option value="categories">Categories</option><option value="entries">Category contents</option><option value="match">Find matching categories</option></NativeSelect></Field>
        {view !== 'categories' && <Field><FieldLabel htmlFor="geo-category">Category filter (optional)</FieldLabel><Input id="geo-category" maxLength={128} value={category} disabled={locked} onChange={(event) => setCategory(event.target.value)} /></Field>}
        <Field><FieldLabel htmlFor="geo-search">{view === 'match' ? currentFile?.kind === 'geoip' ? 'IP address to match' : 'Domain name to match' : 'Search text (optional)'}</FieldLabel><Input id="geo-search" value={search} maxLength={253} disabled={locked} onChange={(event) => setSearch(event.target.value)} /></Field>
      </FieldGroup>
      <Button disabled={locked || !file || view === 'match' && !search.trim()} onClick={() => query({ file, view, category: view === 'categories' ? '' : category, search, offset: 0, limit: 50, snapshot: '' })}>{busy ? 'Reading database…' : 'Search installed database'}</Button>
      <FieldGroup>
        <Field><FieldLabel htmlFor="geo-target">Rule destination</FieldLabel><NativeSelect id="geo-target" value={target} disabled={locked} onChange={(event) => setTarget(event.target.value)}><option value="" disabled>Choose existing VPN / DIRECT / BLOCK target</option>{targets.map((item) => <option key={`${item.kind}:${item.tag}`} value={`${item.kind}:${item.tag}`}>{item.tag}{item.kind === 'balancer' ? ' · VPN balancer' : item.protocol === 'freedom' ? ' · DIRECT' : item.protocol === 'blackhole' ? ' · BLOCK' : ''}</option>)}</NativeSelect></Field>
        <Field><FieldLabel htmlFor="geo-position">Rule position</FieldLabel><NativeSelect id="geo-position" value={position} disabled={locked} onChange={(event) => setPosition(event.target.value)}><option value="first">Before existing rules</option><option value="last">After existing rules</option></NativeSelect></Field>
      </FieldGroup>
      <p className="text-sm text-muted-foreground">The first matching rule wins. Rules placed after a catch-all will not take effect. DNS selection is configured separately.</p>
      {notice && <p role="status">{notice}</p>}
      {result && <>
        <p className="text-sm">{result.total} results · {activeQuery.file}{activeQuery.category ? ` / ${activeQuery.category}` : ''}{activeQuery.search ? ` / ${activeQuery.search}` : ''}. Snapshot {result.snapshot.slice(0, 12)}.</p>
        {result.items.length === 0 && <p>No matches in this installed snapshot.</p>}
        <ul className="flex flex-col gap-2">{result.items.map((item, index) => <li key={`${result.offset + index}`} className="flex flex-wrap items-center gap-2 rounded-md border p-3">
          <div className="min-w-0 flex-1"><p className="break-all font-medium">{item.value}</p><p className="text-sm text-muted-foreground">{item.category} · {item.type}{item.count !== undefined ? ` · ${item.count} entries` : ''}{item.attributes?.length ? ` · attributes: ${item.attributes.join(', ')}` : ''}{item.inverse ? ' · inverse category' : ''}</p></div>
          {item.type === 'category' && <Button variant="outline" disabled={locked} onClick={() => { setView('entries'); setCategory(item.category); setSearch(''); query({ file: result.file, view: 'entries', category: item.category, search: '', offset: 0, limit: 50, snapshot: result.snapshot }) }}>View contents</Button>}
          <Button variant="outline" disabled={locked || !target} onClick={() => addCategory(item)}>Add category rule</Button>
        </li>)}</ul>
        <div className="flex gap-2"><Button variant="outline" disabled={locked || result.offset === 0} onClick={() => query({ ...activeQuery, offset: Math.max(0, result.offset - 50), snapshot: result.snapshot })}>Previous page</Button><Button variant="outline" disabled={locked || !result.more} onClick={() => query({ ...activeQuery, offset: result.offset + 50, snapshot: result.snapshot })}>Next page</Button></div>
      </>}
    </CardContent>
  </Card>
}
