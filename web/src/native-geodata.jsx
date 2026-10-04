import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
import { Checkbox } from '@/components/ui/checkbox'
import { IconSearch, IconDatabase } from '@tabler/icons-react'
import { DestinationBadge } from './status-ui'

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
  const [globalPages, setGlobalPages] = useState([])
  const [selected, setSelected] = useState(new Set())
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

  async function searchAll() {
    if (!search.trim()) { setNotice('Enter a service, category, domain or IP fragment.'); return }
    setBusy(true); setNotice(''); setSelected(new Set()); setGlobalPages([])
    const pages = [], failed = []
    try {
      // The reader serializes bounded scans. Search each installed file once;
      // later pages remain pinned to each file's content hash.
      for (const item of files.filter((item) => item.available)) {
        try {
          const page = await request('geodata/query', { file: item.name, view: 'search', category: '', search: search.trim(), offset: 0, limit: 50, snapshot: '' })
          if (!alive.current) return
          if (page.file !== item.name || !Array.isArray(page.items) || page.items.length > 50 || !/^[a-f0-9]{64}$/.test(page.snapshot)) throw new Error('Invalid page')
          pages.push({ ...page, search: search.trim() })
        } catch { failed.push(item.name) }
      }
      if (alive.current) { setGlobalPages(pages); setNotice(failed.length ? `Some databases could not be read: ${failed.join(', ')}. Results are partial.` : pages.length ? 'All installed databases searched. Select categories to add routing rules.' : 'No available databases.') }
    } finally { if (alive.current) setBusy(false) }
  }

  async function more(page) {
    setBusy(true)
    try {
      const next = await request('geodata/query', { file: page.file, view: 'search', category: '', search: page.search, offset: page.offset + 50, limit: 50, snapshot: page.snapshot })
      if (!alive.current) return
      if (next.snapshot !== page.snapshot || next.file !== page.file || !Array.isArray(next.items) || next.items.length > 50 || next.offset !== page.offset+50) throw new Error('Database changed; search again.')
      setGlobalPages((previous) => previous.map((item) => item.file === page.file ? { ...next, items: [...item.items, ...next.items], search: page.search } : item))
    } catch (error) { if (alive.current) { setNotice(error.message); setSelected(new Set()) } }
    finally { if (alive.current) setBusy(false) }
  }

  function addSelected() {
    const destination = targets.find((item) => `${item.kind}:${item.tag}` === target)
    if (!destination) return
    const domain = [], ip = []
    for (const page of globalPages) for (const item of page.items) {
      if (!selected.has(`${page.file}:${item.category}`)) continue
      const reference = page.file === `${page.kind}.dat` ? `${page.kind}:${item.category}` : `ext:${page.file}:${item.category}`
      ;(page.kind === 'geosite' ? domain : ip).push(reference)
    }
    const key = destination.kind === 'balancer' ? 'balancerTag' : 'outboundTag'
    // Separate alternatives: combining domain AND IP in one rule would miss
    // traffic. One atomic draft edit creates the two category groups together.
    const rules = [domain.length && { type: 'field', domain, [key]: destination.tag }, ip.length && { type: 'field', ip, [key]: destination.tag }].filter(Boolean)
    if (rules.length && onAdd(rules, position) !== false) { setSelected(new Set()); setNotice('Selected categories added to the draft. Save and apply when ready.') }
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
      <Field><FieldLabel htmlFor="geo-global-search">Find a service across all databases</FieldLabel><Input id="geo-global-search" value={search} maxLength={253} placeholder="microsoft, youtube, domain or IP fragment…" disabled={locked} onChange={(event) => setSearch(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !locked) { event.preventDefault(); void searchAll() } }} /></Field>
      <Button className="self-start" disabled={locked || !search.trim()} onClick={searchAll}><IconSearch />{busy ? 'Searching…' : 'Search all databases'}</Button>
      <div className="grid gap-2">{globalPages.map((page) => <section key={page.file} className="rounded-lg border p-3"><h4 className="mb-2 flex items-center gap-2 font-medium"><IconDatabase className="text-info" />{page.file} · {page.total} categories</h4>{page.items.map((item) => { const key = `${page.file}:${item.category}`; return <div key={key} className="flex flex-wrap items-center gap-3 border-b py-2 last:border-0"><Checkbox aria-label={`Select ${page.file} ${item.category}`} checked={selected.has(key)} disabled={locked} onCheckedChange={(checked) => setSelected((old) => { const next = new Set(old); if (checked) next.add(key); else next.delete(key); return next })} /><div className="min-w-0 flex-1"><strong className="break-all">{item.category}</strong><p className="text-xs text-muted-foreground">{page.kind === 'geoip' ? 'IP / CIDR' : 'Host / domain'} · {item.count || 0} entries{item.inverse ? ' · inverse match' : ''}</p></div><Button variant="ghost" disabled={locked} onClick={() => { setFile(page.file); setView('entries'); setCategory(item.category); query({ file: page.file, view: 'entries', category: item.category, search: '', offset: 0, limit: 50, snapshot: page.snapshot }) }}>View contents</Button></div> })}{page.more && <Button className="mt-2" variant="outline" disabled={locked} onClick={() => more(page)}>More categories in {page.file}</Button>}</section>)}</div>
      <details className="disclosure"><summary>Browse one database / inspect category contents</summary><div className="disclosure-body flex flex-col gap-4">
      <FieldGroup>
        <Field><FieldLabel htmlFor="geo-file">Database file</FieldLabel><NativeSelect id="geo-file" value={file} disabled={locked || files.length === 0} onChange={(event) => { setFile(event.target.value); setCategory(''); setResult(null); setSearch('') }}><option value="" disabled>Select installed file</option>{files.map((item) => <option key={item.name} value={item.name} disabled={!item.available}>{item.name} · {Math.round(item.size / 1024)} KiB{item.available ? '' : ' · unavailable'}</option>)}</NativeSelect></Field>
        <Field><FieldLabel htmlFor="geo-view">Browse mode</FieldLabel><NativeSelect id="geo-view" value={view} disabled={locked} onChange={(event) => { setView(event.target.value); setSearch(''); setResult(null) }}><option value="categories">Categories</option><option value="entries">Category contents</option><option value="match">Find matching categories</option></NativeSelect></Field>
        {view !== 'categories' && <Field><FieldLabel htmlFor="geo-category">Category filter (optional)</FieldLabel><Input id="geo-category" maxLength={128} value={category} disabled={locked} onChange={(event) => setCategory(event.target.value)} /></Field>}
        <Field><FieldLabel htmlFor="geo-search">{view === 'match' ? currentFile?.kind === 'geoip' ? 'IP address to match' : 'Domain name to match' : 'Search text (optional)'}</FieldLabel><Input id="geo-search" value={search} maxLength={253} disabled={locked} onChange={(event) => setSearch(event.target.value)} /></Field>
      </FieldGroup>
      <Button disabled={locked || !file || view === 'match' && !search.trim()} onClick={() => query({ file, view, category: view === 'categories' ? '' : category, search, offset: 0, limit: 50, snapshot: '' })}>{busy ? 'Reading database…' : 'Search installed database'}</Button>
      </div></details>
      <FieldGroup>
        <Field><FieldLabel htmlFor="geo-target">Rule destination</FieldLabel><NativeSelect id="geo-target" value={target} disabled={locked} onChange={(event) => setTarget(event.target.value)}><option value="" disabled>Choose existing VPN / DIRECT / BLOCK target</option>{targets.map((item) => <option key={`${item.kind}:${item.tag}`} value={`${item.kind}:${item.tag}`}>{item.tag}{item.kind === 'balancer' ? ' · VPN balancer' : item.protocol === 'freedom' ? ' · DIRECT' : item.protocol === 'blackhole' ? ' · BLOCK' : ''}</option>)}</NativeSelect></Field>
        <Field><FieldLabel htmlFor="geo-position">Rule position</FieldLabel><NativeSelect id="geo-position" value={position} disabled={locked} onChange={(event) => setPosition(event.target.value)}><option value="first">Before existing rules</option><option value="last">After existing rules</option></NativeSelect></Field>
      </FieldGroup>
      <div className="flex flex-wrap items-center gap-2"><Button disabled={locked || !target || !selected.size} onClick={addSelected}>Add selected categories ({selected.size})</Button>{target && <DestinationBadge tag={targets.find((item) => `${item.kind}:${item.tag}` === target)?.tag} target={targets.find((item) => `${item.kind}:${item.tag}` === target)} />}</div>
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
