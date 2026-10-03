import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect } from '@/components/ui/native-select'
import { documentField, editDocumentField, formatDocument, inspectDocument } from './native-config-document'

const TextEditor = lazy(() => import('./native-config-text.jsx'))
const fields = [
  { file: '02_dns.json', area: 'dns', field: 'queryStrategy', label: 'DNS address family', options: ['UseIP', 'UseIPv4', 'UseIPv6'] },
  { file: '02_dns.json', area: 'dns', field: 'disableCache', label: 'Disable DNS cache', boolean: true },
  { file: '02_dns.json', area: 'dns', field: 'disableFallback', label: 'Disable DNS fallback', boolean: true },
  { file: '05_routing.json', area: 'routing', field: 'domainStrategy', label: 'Routing domain resolution', options: ['AsIs', 'IPIfNonMatch', 'IPOnDemand'] },
  { file: '07_observatory.json', area: 'observatory', field: 'probeInterval', label: 'Node probe interval (for example 30s)' },
  { file: '07_observatory.json', area: 'observatory', field: 'enableConcurrency', label: 'Concurrent node probes', boolean: true },
]

export function NativeConfigSection({ csrfToken, onUnauthorized }) {
  const [open, setOpen] = useState(false)
  const [workspace, setWorkspace] = useState(null)
  const [drafts, setDrafts] = useState({})
  const [file, setFile] = useState('')
  const [mode, setMode] = useState('form')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [diagnostic, setDiagnostic] = useState(null)
  const alive = useRef(true)
  const owner = useRef(csrfToken)
  owner.current = csrfToken
  const requests = useRef(new Set())
  const history = useRef({})
  const current = () => alive.current && owner.current === csrfToken
  useEffect(() => {
    alive.current = true
    setOpen(false); setWorkspace(null); setDrafts({}); setFile(''); setNotice(''); setDiagnostic(null); history.current = {}
    return () => { alive.current = false; requests.current.forEach((controller) => controller.abort()) }
  }, [csrfToken])
  const text = drafts[file] ?? ''
  const parsed = inspectDocument(text)
  const changed = workspace && Object.keys(drafts).some((id) => drafts[id] !== workspace.documents[id]?.text)
  useEffect(() => {
    if (!changed) return
    const warn = (event) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [changed])

  async function request(path, body) {
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), body ? 60_000 : 10_000)
    requests.current.add(controller)
    try {
      const response = await fetch(`/api/v1/xkeen/config/${path}`, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', signal: controller.signal, headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: body === undefined ? undefined : JSON.stringify(body) })
      if (!current()) throw new Error('Session changed.')
      if (response.status === 401) { onUnauthorized?.(); throw new Error('Session ended.') }
      const value = await response.json()
      if (!current()) throw new Error('Session changed.')
      if (!response.ok) {
        if (value.diagnostic) setDiagnostic(value.diagnostic)
        throw new Error(value.error || 'Save was not confirmed. Reload before another save.')
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
    const id = Object.keys(value.documents)[0] || ''
    const loaded = id ? await request('document', { file: id }) : null
    if (loaded && (loaded.digest !== value.digest || typeof loaded.document?.text !== 'string')) throw new Error('Configuration changed. Reload before editing.')
    if (loaded) value.documents[id] = loaded.document
    setWorkspace(value)
    setDrafts(loaded ? { [id]: loaded.document.text } : {})
    setFile(id)
    history.current = {}
  }
  async function selectFile(id) {
    if (Object.hasOwn(drafts, id)) { setFile(id); return }
    const loaded = await request('document', { file: id })
    if (loaded.digest !== workspace.digest || typeof loaded.document?.text !== 'string') throw new Error('Configuration changed. Reload before editing.')
    setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, [id]: loaded.document } }))
    setDrafts((previous) => ({ ...previous, [id]: loaded.document.text }))
    setFile(id)
  }
  function edit(next) {
    if (busy || next === text || next.length > (2 << 20)) return
    const item = history.current[file] ||= { undo: [], redo: [] }
    item.undo.push(text); item.redo = []
    while (item.undo.length > 40 || item.undo.reduce((sum, value) => sum + value.length, 0) > (4 << 20)) item.undo.shift()
    setDrafts((previous) => ({ ...previous, [file]: next }))
  }
  function step(direction) {
    if (busy) return
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
    setWorkspace((previous) => ({ ...previous, digest: result.digest, pending: { files: [...new Set([...(previous.pending?.files || []), file])], drift: false }, documents: { ...previous.documents, [file]: { ...previous.documents[file], text } } }))
    setNotice('Saved and validated. Use the native Restart command to apply saved configurations together.')
  }
  const storedDraft = workspace?.documents[file]?.draft
  return <Card>
    <CardHeader><CardTitle>Native configuration</CardTitle><CardDescription>Form and Text share one document. Save validates files; native Restart applies them together.</CardDescription></CardHeader>
    <CardContent className="space-y-4">
      {!open ? <Button variant="outline" onClick={() => { setOpen(true); void run(reload) }}>Edit native configuration</Button> : <Button variant="outline" disabled={busy || !!changed} onClick={() => void run(reload)}>Reload current configuration</Button>}
      {notice && <p role="status">{notice}</p>}
      {open && workspace && <>
        {workspace.pending?.files?.length > 0 && <p role="status">Awaiting native restart: {workspace.pending.files.join(', ')}</p>}
        {workspace.pending?.drift && <p role="alert">Saved configuration changed externally. Inspect and reload; pending files will not be overwritten.</p>}
        <Field><FieldLabel htmlFor="native-config-file">Configuration file</FieldLabel><NativeSelect id="native-config-file" value={file} disabled={busy} onChange={(event) => void run(() => selectFile(event.target.value))}>{Object.keys(workspace.documents).map((id) => <option key={id}>{id}</option>)}</NativeSelect></Field>
        <div className="flex flex-wrap gap-2">
          <Button variant={mode === 'form' ? 'default' : 'outline'} onClick={() => setMode('form')}>Form</Button>
          <Button variant={mode === 'text' ? 'default' : 'outline'} onClick={() => setMode('text')}>Text</Button>
          <Button variant="outline" disabled={busy || !!parsed.error} onClick={() => { try { edit(formatDocument(text)) } catch (error) { setNotice(error.message) } }}>Format JSON</Button>
          <Button variant="outline" disabled={busy || !history.current[file]?.undo.length} onClick={() => step('undo')}>Undo</Button>
          <Button variant="outline" disabled={busy || !history.current[file]?.redo.length} onClick={() => step('redo')}>Redo</Button>
        </div>
        {storedDraft !== undefined && <div className="flex flex-wrap items-center gap-2"><span>A saved draft is available.</span><Button variant="outline" disabled={busy} onClick={() => edit(storedDraft)}>Resume draft</Button><Button variant="outline" disabled={busy} onClick={() => void run(async () => { await request('draft', { file, discard: true }); setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, [file]: { text: previous.documents[file].text } } })) })}>Discard saved draft</Button></div>}
        {parsed.error && <p role="alert">{parsed.error}</p>}
        {mode === 'text' ? <Suspense fallback={<p>Loading text editor…</p>}><TextEditor text={text} disabled={busy} onChange={edit} onUndo={() => step('undo')} onRedo={() => step('redo')} /></Suspense> : !parsed.error && <div className="space-y-3">
          {fields.filter((item) => item.file === file).map((item) => <NativeField key={item.field} item={item} current={documentField(parsed.tree, item.area, item.field)} disabled={busy} onChange={(value) => { try { edit(editDocumentField(text, item.area, item.field, value)) } catch (error) { setNotice(error.message) } }} />)}
          <p className="text-sm text-muted-foreground">Additional native properties are available in Text mode. Unknown fields and comments are preserved.</p>
        </div>}
        <div className="flex flex-wrap gap-2">
          <Button disabled={busy || !!parsed.error || !!workspace.pending?.drift || text === workspace.documents[file]?.text} onClick={() => void run(save)}>Save configuration</Button>
          <Button variant="outline" disabled={busy} onClick={() => void run(async () => { await request('draft', { file, text }); setWorkspace((previous) => ({ ...previous, documents: { ...previous.documents, [file]: { ...previous.documents[file], draft: text } } })); setNotice('Draft saved. Native files and the running service are unchanged.') })}>Save draft</Button>
          <Button variant="outline" disabled={busy || text === workspace.documents[file]?.text} onClick={() => edit(workspace.documents[file].text)}>Discard working edits</Button>
        </div>
        {diagnostic && <details open><summary>Xray validation — {diagnostic.file}</summary><pre className="max-h-96 overflow-auto whitespace-pre-wrap">{diagnostic.output}</pre>{diagnostic.truncated && <p role="alert">Output exceeded 256 KiB; the captured output is incomplete.</p>}</details>}
      </>}
    </CardContent>
  </Card>
}

function NativeField({ item, current, disabled, onChange }) {
  const id = `native-config-${item.area}-${item.field}`
  return <Field><FieldLabel htmlFor={id}>{item.label}</FieldLabel>
    {item.options || item.boolean ? <NativeSelect id={id} value={String(current ?? '')} disabled={disabled} onChange={(event) => onChange(item.boolean ? event.target.value === 'true' : event.target.value)}><option value="" disabled>Native default / unspecified</option>{(item.options || ['false', 'true']).map((option) => <option key={option} value={option}>{item.boolean ? option === 'true' ? 'Yes' : 'No' : option}</option>)}</NativeSelect> : <Input id={id} value={current ?? ''} maxLength={32} disabled={disabled} onChange={(event) => onChange(event.target.value)} />}
  </Field>
}
