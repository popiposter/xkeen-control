import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
import { Checkbox } from '@/components/ui/checkbox'
import { IconSearch, IconDatabase, IconChevronDown, IconChevronRight, IconX } from '@tabler/icons-react'
import { DestinationBadge } from './status-ui'
import { catchAllRule, membershipKind, trafficInbounds } from './routing-rules.js'

const pageSize = 50
const inboundLabel = { redirect: 'LAN devices', tproxy: 'LAN devices', socks: 'Local SOCKS' }

// A native reference to one category: the short form for the default
// geosite.dat/geoip.dat, ext:<file>:<category> for any other file.
function reference(file, kind, category) {
  return file === `${kind}.dat` ? `${kind}:${category}` : `ext:${file}:${category}`
}

function validPage(value, body) {
  return value && value.file === body.file && Array.isArray(value.items) && value.items.length <= pageSize && /^[a-f0-9]{64}$/.test(value.snapshot) && value.offset === body.offset && Number.isInteger(value.total)
}

// rows lists a group's categories: those found by name or text first, then
// those found only by native membership, each with the entry that matched.
function rows(group) {
  const out = group.items.filter((item) => item.type === 'category').map((item) => ({ category: item.category, count: item.count, inverse: item.inverse }))
  const index = new Map(out.map((row) => [row.category, row]))
  for (const item of group.matches || []) {
    if (!item.category) continue
    let row = index.get(item.category)
    if (!row) { row = { category: item.category, inverse: item.inverse }; index.set(item.category, row); out.push(row) }
    if (!row.match) row.match = item.type === 'inverse' ? 'it lies outside the listed networks (inverse category)' : item.value
  }
  return out
}

function targetLabel(item) {
  return `${item.tag}${item.kind === 'balancer' ? ' · VPN pool' : item.protocol === 'freedom' ? ' · DIRECT' : item.protocol === 'blackhole' ? ' · BLOCK' : ''}`
}

// NativeGeodata finds categories in the installed geodata files, shows what
// they contain and adds them to the routing draft as one rule per kind
// (sites, IPs). rules is the current draft's rule list, used for the preview:
// a new rule covers the same inbounds as the draft's user rules and lands
// before a final catch-all rule unless placed at the top.
export default function NativeGeodata({ request, disabled, targets, rules = [], onAdd }) {
  const [files, setFiles] = useState([])
  const [search, setSearch] = useState('')
  const [browseFile, setBrowseFile] = useState('')
  const [groups, setGroups] = useState([])
  const [open, setOpen] = useState(null)
  const [selected, setSelected] = useState([])
  const [target, setTarget] = useState('')
  const [position, setPosition] = useState('last')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const alive = useRef(true)
  const initialRequest = useRef(request)
  const locked = disabled || busy
  const available = files.filter((item) => item.available)

  useEffect(() => {
    let active = true
    alive.current = true
    setBusy(true)
    initialRequest.current('geodata').then((items) => {
      if (!active) return
      if (!Array.isArray(items) || items.length > 32 || items.some((item) => typeof item.name !== 'string' || !['geosite', 'geoip'].includes(item.kind))) throw new Error('Geodata inventory was not confirmed.')
      setFiles(items)
      setBrowseFile(items.find((item) => item.available)?.name || '')
    }).catch((error) => { if (active) setNotice(error.message) }).finally(() => { if (active) setBusy(false) })
    return () => { active = false; alive.current = false }
  }, [])

  // The default destination is the VPN pool when the draft has one.
  useEffect(() => {
    if (!target && targets.length) {
      const preferred = targets.find((item) => item.kind === 'balancer') || targets.find((item) => item.protocol !== 'freedom' && item.protocol !== 'blackhole')
      if (preferred) setTarget(`${preferred.kind}:${preferred.tag}`)
    }
  }, [targets, target])

  async function read(body) {
    const value = await request('geodata/query', body)
    if (!validPage(value, body)) throw new Error('Geodata page was not confirmed.')
    return value
  }

  // A read that finds the database replaced (or unreadable) drops everything
  // built from that file: its listed categories, open contents and selection.
  function invalidate(file, message) {
    setGroups((previous) => previous.filter((group) => group.file !== file))
    setOpen((previous) => previous?.file === file ? null : previous)
    setSelected((previous) => previous.filter((item) => item.file !== file))
    setNotice(message)
  }

  // New results replace the list; a selection whose file now has a different
  // snapshot no longer names what the user inspected.
  function showGroups(found) {
    setOpen(null)
    setGroups(found)
    const snapshots = new Map(found.map((group) => [group.file, group.snapshot]))
    setSelected((previous) => previous.filter((item) => !snapshots.has(item.file) || snapshots.get(item.file) === item.snapshot))
  }

  async function searchAll() {
    const text = search.trim()
    if (!text) { setNotice('Enter a service, category, domain or IP fragment.'); return }
    // An address or host name is also looked up by native matching, so
    // 192.0.2.5 finds a category holding 192.0.2.0/24 and login.example.com
    // one holding domain:example.com, not only literal text.
    const membership = membershipKind(text)
    setBusy(true); setNotice('')
    const found = [], failed = []
    try {
      // The reader serializes bounded scans. Search each installed file once;
      // later pages stay pinned to each file's content hash.
      for (const item of available) {
        try {
          const body = { file: item.name, view: 'search', category: '', search: text, offset: 0, limit: pageSize, snapshot: '' }
          const page = await read(body)
          if (!alive.current) return
          let matches = []
          if (membership === (item.kind === 'geoip' ? 'ip' : 'domain')) {
            const matchBody = { file: item.name, view: 'match', category: '', search: text, offset: 0, limit: pageSize, snapshot: page.snapshot }
            const matched = await read(matchBody)
            if (!alive.current) return
            if (matched.snapshot !== page.snapshot) throw new Error('changed')
            matches = matched.items
          }
          const group = { ...page, query: body, matches }
          if (rows(group).length) found.push(group)
        } catch { failed.push(item.name) }
      }
      if (!alive.current) return
      showGroups(found)
      setNotice(failed.length ? `Some databases could not be read: ${failed.join(', ')}. Results are partial.` : found.length ? '' : `No category in the installed databases matches “${text}”.`)
    } finally { if (alive.current) setBusy(false) }
  }

  async function browse() {
    if (!browseFile) return
    setBusy(true); setNotice('')
    try {
      const body = { file: browseFile, view: 'categories', category: '', search: '', offset: 0, limit: pageSize, snapshot: '' }
      const page = await read(body)
      if (alive.current) showGroups([{ ...page, query: body, matches: [] }])
    } catch (error) { if (alive.current) setNotice(error.message) }
    finally { if (alive.current) setBusy(false) }
  }

  async function more(group) {
    setBusy(true)
    try {
      const body = { ...group.query, offset: group.items.length, snapshot: group.snapshot }
      const next = await request('geodata/query', body)
      if (!alive.current) return
      if (!validPage(next, body) || next.snapshot !== group.snapshot) throw new Error('changed')
      setGroups((previous) => previous.map((item) => item.file === group.file ? { ...item, more: next.more, total: next.total, items: [...item.items, ...next.items] } : item))
    } catch (error) { if (alive.current) invalidate(group.file, `${group.file} changed or could not be read (${error.message === 'changed' ? 'database replaced' : error.message}); search again.`) }
    finally { if (alive.current) setBusy(false) }
  }

  async function contents(group, category, more = false) {
    const key = `${group.file}:${category}`
    if (!more && open?.key === key) { setOpen(null); return }
    // Each page continues after the entries already shown.
    const offset = more && open?.key === key ? open.items.length : 0
    setBusy(true); setNotice('')
    try {
      const body = { file: group.file, view: 'entries', category, search: '', offset, limit: pageSize, snapshot: group.snapshot }
      const page = await read(body)
      if (!alive.current) return
      if (page.snapshot !== group.snapshot) throw new Error('changed')
      setOpen((previous) => ({ key, file: group.file, category, total: page.total, more: page.more, items: offset && previous?.key === key ? [...previous.items, ...page.items] : page.items }))
    } catch (error) { if (alive.current) invalidate(group.file, `${group.file} changed or could not be read (${error.message === 'changed' ? 'database replaced' : error.message}); search again.`) }
    finally { if (alive.current) setBusy(false) }
  }

  function toggle(group, row, checked) {
    const key = `${group.file}:${row.category}`
    setSelected((old) => checked ? [...old.filter((entry) => entry.key !== key), { key, file: group.file, kind: group.kind, snapshot: group.snapshot, category: row.category, reference: reference(group.file, group.kind, row.category) }] : old.filter((entry) => entry.key !== key))
  }

  const destination = targets.find((item) => `${item.kind}:${item.tag}` === target)
  const inboundTag = trafficInbounds(rules)
  const domain = selected.filter((item) => item.kind === 'geosite').map((item) => item.reference)
  const ip = selected.filter((item) => item.kind === 'geoip').map((item) => item.reference)
  const destinationKey = destination?.kind === 'balancer' ? 'balancerTag' : 'outboundTag'
  // Separate alternatives: one rule with both sites AND IPs would match only
  // connections that satisfy both, and miss traffic.
  const planned = destination ? [domain.length && { type: 'field', inboundTag, domain, [destinationKey]: destination.tag }, ip.length && { type: 'field', inboundTag, ip, [destinationKey]: destination.tag }].filter(Boolean) : []
  const beforeCatchAll = position !== 'first' && rules.length > 0 && catchAllRule(rules[rules.length - 1])
  const firstNumber = position === 'first' ? 1 : beforeCatchAll ? rules.length : rules.length + 1
  const inboundText = [...new Set(inboundTag.map((tag) => inboundLabel[tag] || tag))].join(', ')

  function add() {
    if (!planned.length) return
    const count = planned.length
    if (onAdd(planned, position) !== false) {
      setSelected([])
      setNotice(`Added as rule${count > 1 ? `s ${firstNumber}–${firstNumber + count - 1}` : ` ${firstNumber}`}. Save and apply the configuration to activate ${count > 1 ? 'them' : 'it'}.`)
    }
  }

  return <Card>
    <CardHeader><CardTitle>Add rules from geodata</CardTitle><CardDescription>Find categories in the installed geosite (sites) and geoip (IP) files, check what they contain, and add them to the routing draft. Nothing changes on the router until you save and apply.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-5">
      <section className="flex flex-col gap-3" aria-label="Find categories">
        <h4 className="font-medium">1. Find categories</h4>
        <div className="flex flex-wrap items-end gap-2">
          <Field className="min-w-56 flex-1"><FieldLabel htmlFor="geo-global-search">Find a service across all databases</FieldLabel><Input id="geo-global-search" value={search} maxLength={253} placeholder="microsoft, youtube, a domain or IP fragment…" disabled={locked} onChange={(event) => setSearch(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !locked) { event.preventDefault(); void searchAll() } }} /></Field>
          <Button disabled={locked || !search.trim()} onClick={searchAll}><IconSearch />{busy ? 'Searching…' : 'Search all databases'}</Button>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <Field className="min-w-56 flex-1"><FieldLabel htmlFor="geo-file">Or list every category of one database</FieldLabel><NativeSelect id="geo-file" value={browseFile} disabled={locked || available.length === 0} onChange={(event) => setBrowseFile(event.target.value)}><option value="" disabled>Select installed file</option>{files.map((item) => <option key={item.name} value={item.name} disabled={!item.available}>{item.name} · {item.kind === 'geoip' ? 'IPs' : 'sites'} · {Math.round(item.size / 1024)} KiB{item.available ? '' : ' · unavailable'}</option>)}</NativeSelect></Field>
          <Button variant="outline" disabled={locked || !browseFile} onClick={browse}><IconDatabase />Show categories</Button>
        </div>
        {groups.map((group) => <section key={group.file} className="rounded-lg border" aria-label={`Categories in ${group.file}`}>
          <h5 className="flex flex-wrap items-center gap-2 border-b px-3 py-2 text-sm font-medium"><IconDatabase className="text-info" />{group.file}<Badge variant="outline">{group.kind === 'geoip' ? 'IP ranges' : 'Sites'}</Badge><span className="text-muted-foreground">{group.total} {group.total === 1 ? 'category' : 'categories'}</span></h5>
          <ul>{rows(group).map((item) => {
            const key = `${group.file}:${item.category}`
            const expanded = open?.key === key
            return <li key={key} className="border-b px-3 py-2 last:border-0">
              <div className="flex flex-wrap items-center gap-3">
                <Checkbox aria-label={`Select ${group.file} ${item.category}`} checked={selected.some((entry) => entry.key === key)} disabled={locked} onCheckedChange={(checked) => toggle(group, item, checked)} />
                <div className="min-w-0 flex-1"><strong className="break-all">{item.category}</strong><p className="text-xs text-muted-foreground">{item.count !== undefined ? `${item.count} ${group.kind === 'geoip' ? 'IP ranges' : 'domain entries'}` : group.kind === 'geoip' ? 'IP ranges' : 'Domain entries'}{item.inverse ? ' · inverse match' : ''}{item.match ? ` · matches via ${item.match}` : ''}</p></div>
                <Button variant="ghost" size="sm" aria-expanded={expanded} aria-label={`${expanded ? 'Hide' : 'Show'} contents of ${item.category}`} disabled={locked} onClick={() => contents(group, item.category)}>{expanded ? <IconChevronDown /> : <IconChevronRight />}{expanded ? 'Hide contents' : 'Show contents'}</Button>
              </div>
              {expanded && <div className="mt-2 rounded-md bg-muted/40 p-2">
                <p className="mb-1 text-xs text-muted-foreground">Showing {open.items.length} of {open.total}</p>
                <ul className="grid gap-x-4 sm:grid-cols-2">{open.items.map((entry, index) => <li key={index} className="break-all font-mono text-xs">{entry.value}{entry.type && entry.type !== 'cidr' && entry.type !== 'domain' ? <span className="text-muted-foreground"> · {entry.type}</span> : ''}</li>)}</ul>
                {open.more && <Button className="mt-2" variant="outline" size="sm" disabled={locked} onClick={() => contents(group, item.category, true)}>Show more entries</Button>}
              </div>}
            </li>
          })}</ul>
          {group.more && <Button className="m-3" variant="outline" size="sm" disabled={locked} onClick={() => more(group)}>More categories in {group.file}</Button>}
        </section>)}
      </section>

      <section className="flex flex-col gap-2" aria-label="Selected categories">
        <h4 className="font-medium">2. Selected categories ({selected.length})</h4>
        {selected.length === 0 ? <p className="text-sm text-muted-foreground">Tick categories above. Sites and IP ranges become separate rules.</p>
          : <div className="flex flex-wrap gap-2">{selected.map((item) => <Badge key={item.key} variant="secondary" className="gap-1">{item.category}<span className="text-muted-foreground">· {item.file}</span><button type="button" aria-label={`Remove ${item.category} from selection`} disabled={locked} onClick={() => setSelected((old) => old.filter((entry) => entry.key !== item.key))}><IconX className="size-3" /></button></Badge>)}</div>}
      </section>

      <section className="flex flex-col gap-3" aria-label="Rule">
        <h4 className="font-medium">3. Rule</h4>
        <FieldGroup className="grid gap-4 sm:grid-cols-2">
          <Field><FieldLabel htmlFor="geo-target">Rule destination</FieldLabel><NativeSelect id="geo-target" value={target} disabled={locked} onChange={(event) => setTarget(event.target.value)}><option value="" disabled>Choose VPN pool, DIRECT or BLOCK</option>{targets.map((item) => <option key={`${item.kind}:${item.tag}`} value={`${item.kind}:${item.tag}`}>{targetLabel(item)}</option>)}</NativeSelect></Field>
          <Field><FieldLabel htmlFor="geo-position">Rule position</FieldLabel><NativeSelect id="geo-position" value={position} disabled={locked} onChange={(event) => setPosition(event.target.value)}><option value="last">Before the final catch-all rule</option><option value="first">At the top, before all rules</option></NativeSelect><FieldDescription>Rules are checked top to bottom; the first match decides.</FieldDescription></Field>
        </FieldGroup>
        {planned.length > 0 && <div className="flex flex-col gap-2 rounded-lg border p-3" aria-label="Rule preview">
          {planned.map((rule, index) => <p key={index} className="flex flex-wrap items-center gap-2 text-sm"><Badge variant="outline">Rule {firstNumber + index}</Badge><span>From {inboundText}</span><span>·</span><span>{rule.domain ? `Sites: ${selected.filter((item) => item.kind === 'geosite').map((item) => item.category).join(', ')}` : `IPs: ${selected.filter((item) => item.kind === 'geoip').map((item) => item.category).join(', ')}`}</span><span aria-hidden="true">→</span><DestinationBadge tag={destination.tag} target={destination} /></p>)}
          {beforeCatchAll && <p className="text-xs text-muted-foreground">Placed before the final rule that sends all other traffic to {rules[rules.length - 1].balancerTag || rules[rules.length - 1].outboundTag || 'its destination'}.</p>}
        </div>}
        <Button className="self-start" disabled={locked || !planned.length} onClick={add}>{planned.length > 1 ? `Add ${planned.length} rules` : 'Add rule'}</Button>
      </section>
      {notice && <p role="status">{notice}</p>}
    </CardContent>
  </Card>
}
