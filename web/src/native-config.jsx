import { IconBraces, IconArrowBackUp, IconArrowForwardUp, IconDeviceFloppy, IconTrash, IconRefresh, IconPlayerPlay, IconRestore, IconTerminal2 } from '@tabler/icons-react'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
import { documentField, editDocumentField, formatDocument, inspectDocument } from './native-config-document'
import { NativeConfigForm } from './native-config-form'
import { splitDNSDocuments } from './native-dns-policy'
import { configRequestTimeout } from './native-request-budget'
import { Disclosure } from './ui'

const TextEditor = lazy(() => import('./native-config-text.jsx'))
const fields = [
  { file: '02_dns.json', area: 'dns', field: 'queryStrategy', label: 'DNS address family', options: ['UseIP', 'UseIPv4', 'UseIPv6'] },
  { file: '02_dns.json', area: 'dns', field: 'disableCache', label: 'Disable DNS cache', boolean: true },
  { file: '02_dns.json', area: 'dns', field: 'disableFallback', label: 'Disable DNS fallback', boolean: true },
  { file: '05_routing.json', area: 'routing', field: 'domainStrategy', label: 'Routing domain resolution', options: ['AsIs', 'IPIfNonMatch', 'IPOnDemand'] },
  { file: '07_observatory.json', area: 'observatory', field: 'probeInterval', label: 'Node probe interval (for example 30s)' },
  { file: '07_observatory.json', area: 'observatory', field: 'enableConcurrency', label: 'Concurrent node probes', boolean: true },
]

export function NativeConfigSection({ csrfToken, onUnauthorized, onNativeJob, onWorkingChange, focusFile = '', scopeFile = '', onOpenConsole, readbackKey = 0 }) {
  const [open, setOpen] = useState(false)
  const [workspace, setWorkspace] = useState(null)
  const [drafts, setDrafts] = useState({})
  const [file, setFile] = useState('')
  const [mode, setMode] = useState('form')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [diagnostic, setDiagnostic] = useState(null)
  const [applying, setApplying] = useState(false)
  const [applyJob, setApplyJob] = useState(null)
  const alive = useRef(true)
  const owner = useRef(csrfToken)
  owner.current = csrfToken
  const requests = useRef(new Set())
  const history = useRef({})
  const lastFocus = useRef('')
  const wantedFile = useRef('')
	const lastReadback = useRef(readbackKey)
  const current = () => alive.current && owner.current === csrfToken
  useEffect(() => {
    alive.current = true
    setOpen(false); setWorkspace(null); setDrafts({}); setFile(''); setNotice(''); setDiagnostic(null); setApplying(false); setApplyJob(null); history.current = {}
    return () => { alive.current = false; requests.current.forEach((controller) => controller.abort()) }
  }, [csrfToken])
  const text = drafts[file] ?? ''
  const parsed = inspectDocument(text)
  const changed = workspace && Object.keys(drafts).some((id) => drafts[id] !== workspace.documents[id]?.text)
  useEffect(() => { onWorkingChange?.(!!changed) }, [changed, onWorkingChange])
  const changedDocuments = Object.fromEntries(Object.entries(drafts).filter(([id, value]) => value !== workspace?.documents[id]?.text))
  const invalidChanged = Object.values(changedDocuments).some((value) => inspectDocument(value).error)
  const pending = workspace?.pending
  const restoreNeedsRestart = !!pending?.applyId || pending?.applyState === 'restored'
  const locked = busy || applying || pending?.applyState === 'running'
  useEffect(() => {
    if (!changed) return
    const warn = (event) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [changed])

  async function request(path, body) {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), configRequestTimeout(path, body))
    requests.current.add(controller)
    try {
      const endpoint = path === 'geodata' || path === 'geodata/query' ? `/api/v1/${path}` : `/api/v1/xkeen/${path.startsWith('jobs/') ? path : `config/${path}`}`
      const response = await fetch(endpoint, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', signal: controller.signal, headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: body === undefined ? undefined : JSON.stringify(body) })
      if (!current()) throw new Error('Session changed.')
      if (response.status === 401) { onUnauthorized?.(); throw new Error('Session ended.') }
      const value = await response.json()
      if (!current()) throw new Error('Session changed.')
      if (!response.ok) {
        if (value.diagnostic) setDiagnostic(value.diagnostic)
        const error = new Error(value.error || 'Save was not confirmed. Reload before another save.')
        error.status = response.status
        throw error
      }
      return value
    } finally { clearTimeout(timer); requests.current.delete(controller) }
  }
  async function run(action) {
    if (busy) return
    setBusy(true); setNotice(''); setDiagnostic(null)
    try { await action() }
    catch (error) { if (current()) setNotice(error.message) }
    finally { if (current()) setBusy(false) }
  }
  async function reload() {
    const value = await request('workspace')
    if (!/^[a-f0-9]{64}$/.test(value.digest) || !value.documents || typeof value.documents !== 'object') throw new Error('Configuration response could not be confirmed.')
    const id = Object.hasOwn(value.documents, focusFile) ? focusFile : Object.keys(value.documents)[0] || ''
    const loaded = id ? await request('document', { file: id }) : null
    if (loaded && (loaded.digest !== value.digest || typeof loaded.document?.text !== 'string')) throw new Error('Configuration changed. Reload before editing.')
    if (loaded) value.documents[id] = loaded.document
    setWorkspace(value)
    setDrafts(loaded ? { [id]: loaded.document.text } : {})
    setFile(id)
    history.current = {}
  }
  useEffect(() => {
    if (!focusFile || open) return
    let cancelled = false
    Promise.resolve().then(() => { if (!cancelled) { setOpen(true); void run(reload) } })
    return () => { cancelled = true }
  }, [focusFile, open, csrfToken])
  async function selectFile(id) {
    if (Object.hasOwn(drafts, id)) { setFile(id); return }
    const loaded = await request('document', { file: id })
    if (loaded.digest !== workspace.digest || typeof loaded.document?.text !== 'string') throw new Error('Configuration changed. Reload before editing.')
    setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, [id]: loaded.document } }))
    setDrafts((previous) => ({ ...previous, [id]: loaded.document.text }))
    setFile(id)
  }
  useEffect(() => {
    if (lastFocus.current !== focusFile) { lastFocus.current = focusFile; wantedFile.current = focusFile }
    if (!open || !workspace || locked || !wantedFile.current) return
    const id = wantedFile.current
    wantedFile.current = ''
    if (id !== file && Object.hasOwn(workspace.documents, id)) void run(() => selectFile(id))
  }, [focusFile, open, locked, workspace?.digest, csrfToken])
  async function syncWorkspace() {
    const next = await request('workspace')
    if (!/^[a-f0-9]{64}$/.test(next.digest) || !next.documents) throw new Error('Configuration response could not be confirmed.')
    const loaded = Object.keys(drafts)
    const newDrafts = { ...drafts }
    for (const id of loaded) {
      if (!Object.hasOwn(next.documents, id)) throw new Error('Native configuration files changed. Inspect before continuing.')
      const value = await request('document', { file: id })
      if (value.digest !== next.digest || typeof value.document?.text !== 'string') throw new Error('Configuration changed during readback.')
      next.documents[id] = value.document
      // Keep unfinished work; update only documents that matched saved files.
      if (drafts[id] === workspace.documents[id]?.text) newDrafts[id] = value.document.text
    }
    setWorkspace(next); setDrafts(newDrafts)
  }
  useEffect(() => {
    if (!open || !workspace || locked || lastReadback.current === readbackKey) return
    lastReadback.current = readbackKey
    void run(syncWorkspace)
  }, [readbackKey, open, locked, workspace?.digest, csrfToken])
  function edit(next) {
    if (locked) return false
    if (next === text) return true
    if (new TextEncoder().encode(next).length > (2 << 20)) { setNotice('Configuration text exceeds 2 MiB.'); return false }
    const item = history.current[file] ||= { undo: [], redo: [] }
    item.undo.push(text); item.redo = []
    while (item.undo.length > 40 || item.undo.reduce((sum, value) => sum + value.length, 0) > (4 << 20)) item.undo.shift()
    setDrafts((previous) => ({ ...previous, [file]: next }))
    return true
  }
  function step(direction) {
    if (locked) return
    const item = history.current[file]
    const source = item?.[direction]
    if (!source?.length) return
    item[direction === 'undo' ? 'redo' : 'undo'].push(text)
    const restored = source.pop()
    setDrafts((previous) => ({ ...previous, [file]: restored }))
  }
  async function save() {
    const result = await request('text', { digest: workspace.digest, file, text })
    if (!/^[a-f0-9]{64}$/.test(result.digest)) throw new Error('Save was not confirmed. Reload before another save.')
    setWorkspace((previous) => ({ ...previous, digest: result.digest, pending: { ...previous.pending, files: [...new Set([...(previous.pending?.files || []), file])], drift: false }, documents: { ...previous.documents, [file]: { ...previous.documents[file], text } } }))
    setNotice('Saved and validated. Use the native Restart command to apply saved configurations together.')
  }
  async function prepareSplitDNS() {
    const ids = ['02_dns.json', '05_routing.json']
    const loaded = {}
    for (const id of ids) {
      const result = await request('document', { file: id })
      if (result.digest !== workspace.digest || typeof result.document?.text !== 'string') throw new Error('Configuration changed. Reload before preparing DNS.')
      loaded[id] = result.document
    }
    const prepared = splitDNSDocuments(drafts['02_dns.json'] ?? loaded['02_dns.json'].text, drafts['05_routing.json'] ?? loaded['05_routing.json'].text, workspace.targets || [])
    for (const id of ids) {
      const old = drafts[id] ?? loaded[id].text
      const item = history.current[id] ||= { undo: [], redo: [] }
      item.undo.push(old); item.redo = []; while (item.undo.length > 40 || item.undo.reduce((sum,value) => sum+value.length,0) > (4 << 20)) item.undo.shift()
    }
    setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, ...loaded } }))
    setDrafts((previous) => ({ ...previous, ...prepared.documents }))
    setNotice(`DNS draft prepared: ${prepared.vpnMatches} VPN matches, ${prepared.directMatches} DIRECT matches. Save all configurations to validate both files together. LAN clients still use the router's own DNS; IP-only rules and ordered overlapping matches need separate inspection.`)
  }
  async function saveAll() {
    const result = await request('save-set', { digest: workspace.digest, documents: changedDocuments })
    if (!/^[a-f0-9]{64}$/.test(result.digest)) throw new Error('Save was not confirmed. Reload before another save.')
    setWorkspace((previous) => ({ ...previous, digest: result.digest, pending: { ...previous.pending, files: [...new Set([...(previous.pending?.files || []), ...Object.keys(changedDocuments)])], restartRequired: true }, documents: { ...previous.documents, ...Object.fromEntries(Object.entries(changedDocuments).map(([id, value]) => [id, { ...previous.documents[id], text: value }])) } }))
    setNotice('All working changes saved and validated. Restart applies the saved set together.')
  }
  async function applySaved() {
    let job
    try { job = await request('apply', { digest: workspace.digest }) }
    catch (error) {
      if (current() && error.status !== 422 && error.status !== 400) setWorkspace((previous) => ({ ...previous, pending: { ...previous.pending, applyState: 'unknown' } }))
      throw error
    }
    if (!/^[a-f0-9]{32}$/.test(job.id) || job.action !== 'restart' || job.state !== 'running') {
      setWorkspace((previous) => ({ ...previous, pending: { ...previous.pending, applyState: 'unknown' } }))
      throw new Error('Apply acceptance could not be confirmed. Inspect; do not repeat Apply.')
    }
    setApplyJob(job); setApplying(true); onNativeJob?.(job)
    setWorkspace((previous) => ({ ...previous, pending: { ...previous.pending, applyId: job.id, applyState: 'running' } }))
    setNotice('Restart accepted. Waiting for independent configuration and process readback.')
  }
  useEffect(() => {
    if (!applying || !applyJob?.id) return
    let stopped = false
    let timer
    const poll = async () => {
      try {
        const job = await request('jobs/read', { id: applyJob.id, cursor: Number.MAX_SAFE_INTEGER })
        if (stopped || !current()) return
        if (job.id !== applyJob.id || job.action !== 'restart' || !['running', 'completed', 'failed', 'unknown', 'inspected'].includes(job.state)) throw new Error('Apply readback could not be confirmed.')
        if (job.state === 'running') { timer = setTimeout(poll, 1000); return }
        await syncWorkspace()
        if (stopped || !current()) return
        setApplying(false); setApplyJob(job)
        setNotice(job.configurationState === 'applied' ? 'Saved configuration and a new running Xray process observed. VPN and DNS quality are checked separately.' : 'Apply was not verified. Inspect native console output or restore the pre-apply configuration; no automatic rollback was performed.')
      } catch (error) { if (!stopped && current()) { setApplying(false); setNotice('Apply outcome unavailable. Inspect current state; do not repeat Apply.'); setWorkspace((previous) => ({ ...previous, pending: { ...previous.pending, applyState: 'unknown' } })) } }
    }
    void poll()
    return () => { stopped = true; clearTimeout(timer) }
  }, [applying, applyJob?.id, csrfToken])
  const storedDraft = workspace?.documents[file]?.draft
  return <Card>
    <CardHeader><CardTitle>{scopeFile === '02_dns.json' ? 'DNS configuration' : scopeFile === '05_routing.json' ? 'Routing configuration' : 'Native configurations'}</CardTitle><CardDescription>Form and Text share one document. Save validates files; native Restart applies them together.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      {!open ? <Button className="self-start" variant="outline" onClick={() => { setOpen(true); void run(reload) }}>Edit native configuration</Button> : <Button className="self-start" variant="outline" disabled={locked || !!changed} onClick={() => void run(reload)}><IconRefresh data-icon="inline-start" />Reload current configuration</Button>}
      {notice && <p role="status">{notice}</p>}
      {open && workspace && (!scopeFile || file === scopeFile) && <>
        {(pending?.files?.length > 0 || pending?.restartRequired) && <p role="status">Saved configurations: {pending.files.join(', ') || 'Restored pre-apply set'} — {pending.applyState === 'running' ? 'Restart in progress' : pending.applyState === 'unknown' ? 'Application needs inspection' : 'Awaiting native restart'}</p>}
        {workspace.pending?.drift && <p role="alert">Saved configuration changed externally. Inspect and reload; pending files will not be overwritten.</p>}
        <div className="flex flex-wrap gap-2">
          {(pending?.applyState === 'running' && !applying || pending?.applyState === 'unknown') && <Button className="self-start" variant="outline" disabled={busy} onClick={() => void run(async () => {
            const job = await request('jobs/read', { id: pending.applyId || '', cursor: Number.MAX_SAFE_INTEGER })
            if (!/^[a-f0-9]{32}$/.test(job.id) || job.action !== 'restart') throw new Error('Apply job could not be identified. Inspect the native command console.')
            setApplyJob(job); onNativeJob?.(job)
            await syncWorkspace()
            if (job.state === 'running') setApplying(true)
            else setWorkspace((previous) => ({ ...previous, pending: { ...previous.pending, applyId: job.id, applyState: 'unknown' } }))
            setNotice('Existing Apply inspected without restarting. Check its configuration or inspect console output.')
          })}><IconRefresh data-icon="inline-start" />Inspect existing Apply</Button>}
          <Button disabled={locked || !pending || !!pending.drift || pending.applyState === 'unknown'} onClick={() => void run(applySaved)}><IconPlayerPlay data-icon="inline-start" />Apply saved configurations</Button>
          {onOpenConsole && (applyJob || pending?.applyId || pending?.applyState === 'unknown') && <Button className="self-start" variant="outline" onClick={onOpenConsole}>View native console</Button>}
          {pending && <Button className="self-start" variant="outline" disabled={locked || !!pending.drift} onClick={() => void run(async () => { await request('restore-saved', { digest: workspace.digest }); await syncWorkspace(); setNotice(restoreNeedsRestart ? 'Pre-apply files restored. Use Apply to restart with this restored set.' : 'Saved changes discarded. The running service was not restarted.') })}><IconRestore data-icon="inline-start" />{restoreNeedsRestart ? 'Restore pre-apply configurations' : 'Discard saved changes'}</Button>}
          {pending?.applyId && pending.applyState !== 'running' && <Button className="self-start" variant="outline" disabled={locked} onClick={() => void run(async () => { await request('inspect', { id: pending.applyId }); await syncWorkspace(); setNotice('Saved configuration and a new running process were independently confirmed. No Restart was repeated.') })}><IconRefresh data-icon="inline-start" />Check applied configuration</Button>}
          {workspace.hasPrevious && <Button className="self-start" variant="outline" disabled={locked || !!pending || workspace.previousDrift} onClick={() => void run(async () => { await request('restore-previous', { digest: workspace.digest }); await syncWorkspace(); setNotice('Previous configuration saved. Apply it when ready; the running service is unchanged.') })}><IconRestore data-icon="inline-start" />Restore previous configuration</Button>}
        </div>
        {!scopeFile && <Field><FieldLabel htmlFor="native-config-file">Configuration file</FieldLabel><NativeSelect id="native-config-file" value={file} disabled={locked} onChange={(event) => void run(() => selectFile(event.target.value))}>{Object.keys(workspace.documents).map((id) => <option key={id} value={id}>{({ '01_log.json': 'Logging', '02_dns.json': 'DNS', '03_inbounds.json': 'Traffic listeners', '05_routing.json': 'Routing & balancing', '06_policy.json': 'Connection policy', '07_observatory.json': 'Node health', '08_api.json': 'Local API & probes' })[id] || id} - {id}</option>)}</NativeSelect></Field>}
        <div className="flex flex-wrap gap-2">
          <ToggleGroup variant="outline" aria-label="Editor mode" value={[mode]} onValueChange={(values) => { if (values.length) setMode(values[0]) }}><ToggleGroupItem value="form">Form</ToggleGroupItem><ToggleGroupItem value="text">Text</ToggleGroupItem></ToggleGroup>
          <Button className="self-start" variant="outline" disabled={locked || !!parsed.error} onClick={() => { try { edit(formatDocument(text)) } catch (error) { setNotice(error.message) } }}><IconBraces data-icon="inline-start" />Format JSON</Button>
          <Button className="self-start" variant="outline" disabled={locked || !history.current[file]?.undo.length} onClick={() => step('undo')}><IconArrowBackUp data-icon="inline-start" />Undo</Button>
          <Button className="self-start" variant="outline" disabled={locked || !history.current[file]?.redo.length} onClick={() => step('redo')}><IconArrowForwardUp data-icon="inline-start" />Redo</Button>
        </div>
        {storedDraft !== undefined && <div className="flex flex-wrap items-center gap-2"><span>A saved draft is available.</span><Button className="self-start" variant="outline" disabled={locked} onClick={() => edit(storedDraft)}><IconRestore data-icon="inline-start" />Resume draft</Button><Button className="self-start" variant="outline" disabled={locked} onClick={() => void run(async () => { await request('draft', { file, discard: true }); setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, [file]: { text: previous.documents[file].text } } })) })}><IconTrash data-icon="inline-start" />Discard saved draft</Button></div>}
        {parsed.error && <p role="alert">{parsed.error}</p>}
        {mode === 'text' ? <Suspense fallback={<p>Loading text editor…</p>}><TextEditor text={text} disabled={locked} onChange={edit} onUndo={() => step('undo')} onRedo={() => step('redo')} /></Suspense> : !parsed.error && <div className="flex flex-col gap-3">
          {file === '02_dns.json' && <div className="rounded-lg border bg-muted/30 p-4"><h3 className="font-semibold">DNS follows VPN domain categories</h3><p className="my-2 text-sm text-muted-foreground">Replace the resolver list in the draft with Cloudflare and Google DoH through the existing VPN pool for its domain categories. Other Xray lookups use system DNS. This does not change router DHCP or intercept LAN DNS, so the router resolver remains independent of XKeen. Review overlapping categories and IP-only rules separately.</p><Button className="self-start" variant="outline" disabled={locked || !workspace.targetsComplete} onClick={() => void run(prepareSplitDNS)}><IconBraces data-icon="inline-start" />Prepare DNS from routing</Button></div>}
          <Disclosure defaultOpen title="General settings">{fields.filter((item) => item.file === file).map((item) => <NativeField key={item.field} item={item} current={documentField(parsed.tree, item.area, item.field)} disabled={locked} onChange={(value) => { try { edit(editDocumentField(text, item.area, item.field, value)) } catch (error) { setNotice(error.message) } }} />)}</Disclosure>
          <NativeConfigForm key={file} file={file} text={text} tree={parsed.tree} disabled={locked} onChange={edit} onError={setNotice} request={request} targets={workspace.targets || []} />
          <p className="text-sm text-muted-foreground">Additional native properties are available in Text mode. Unknown fields and comments are preserved.</p>
        </div>}
        <div className="flex flex-wrap gap-2">
          {changed && <p className="w-full text-sm text-muted-foreground">Unfinished working edits are excluded from Apply until you save them.</p>}
          <Button disabled={locked || !!parsed.error || !!pending?.drift || pending?.applyState === 'unknown' || text === workspace.documents[file]?.text} onClick={() => void run(save)}><IconDeviceFloppy data-icon="inline-start" />Save configuration</Button>
          <Button className="self-start" variant="outline" disabled={locked || !changed || invalidChanged || !!pending?.drift || pending?.applyState === 'unknown'} onClick={() => void run(saveAll)}>Save all configurations</Button>
          <Button className="self-start" variant="outline" disabled={locked} onClick={() => void run(async () => { await request('draft', { file, text }); setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, [file]: { ...previous.documents[file], draft: text } } })); setNotice('Draft saved. Native files and the running service are unchanged.') })}><IconDeviceFloppy data-icon="inline-start" />Save draft</Button>
          <Button className="self-start" variant="outline" disabled={locked || text === workspace.documents[file]?.text} onClick={() => edit(workspace.documents[file].text)}><IconTrash data-icon="inline-start" />Discard working edits</Button>
        </div>
        {diagnostic && <details open><summary>Xray validation — {diagnostic.file}</summary><pre className="max-h-96 overflow-auto whitespace-pre-wrap">{diagnostic.output}</pre>{diagnostic.truncated && <p role="alert">Output exceeded 256 KiB; the captured output is incomplete.</p>}</details>}
      </>}
    </CardContent>
  </Card>
}

function NativeField({ item, current, disabled, onChange }) {
  const id = `native-config-${item.area}-${item.field}`
  return <Field><FieldLabel htmlFor={id}>{item.label}</FieldLabel>
    {item.options || item.boolean ? <NativeSelect id={id} value={String(current ?? '')} disabled={disabled} onChange={(event) => onChange(event.target.value === '' ? undefined : item.boolean ? event.target.value === 'true' : event.target.value)}><option value="">Native default / unspecified</option>{(item.options || ['false', 'true']).map((option) => <option key={option} value={option}>{item.boolean ? option === 'true' ? 'Yes' : 'No' : option}</option>)}</NativeSelect> : <Input id={id} value={current ?? ''} maxLength={32} disabled={disabled} onChange={(event) => onChange(event.target.value)} />}
  </Field>
}
