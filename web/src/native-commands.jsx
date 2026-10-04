import { IconPlayerPlay, IconRefresh, IconCalendar, IconTools, IconShieldLock, IconNetwork } from '@tabler/icons-react'
import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Field, FieldLabel } from '@/components/ui/field'

const NativeConsole = lazy(() => import('./native-console.jsx'))


export function NativeCommands({ csrfToken, onUnauthorized, onRefresh, jobNotification }) {
  const [catalog, setCatalog] = useState([])
  const [selected, setSelected] = useState(null)
  const [parameter, setParameter] = useState('')
  const [job, setJob] = useState(null)
  const [chunk, setChunk] = useState(null)
  const [consoleOpen, setConsoleOpen] = useState(false)
  const [consoleReady, setConsoleReady] = useState(false)
  const [initializing, setInitializing] = useState(true)
  const [pending, setPending] = useState(false)
  const [unknown, setUnknown] = useState(false)
  const [inputFault, setInputFault] = useState(false)
  const [notice, setNotice] = useState('')
  const alive = useRef(true)
  const cursor = useRef(0)
  const owner = useRef(csrfToken)
  owner.current = csrfToken
  const inputQueue = useRef(Promise.resolve())
  const queuedInput = useRef({ bytes: 0, count: 0, epoch: 0 })
  const currentJob = useRef(job?.id)
  currentJob.current = job?.id
  const consumed = useRef(null)
  const generation = useRef(0)
  const requests = useRef(new Set())
  const current = () => alive.current && owner.current === csrfToken
  const refresh = useRef(onRefresh)
  refresh.current = onRefresh

  const request = useCallback(async (path, body) => {
    const epoch = generation.current
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), 10_000)
    requests.current.add(controller)
    try {
    const response = await fetch(`/api/v1/xkeen/${path}`, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store', signal: controller.signal, headers: body === undefined ? {} : { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: body === undefined ? undefined : JSON.stringify(body) })
    if (!alive.current || owner.current !== csrfToken || epoch !== generation.current) throw new Error('Session changed.')
    if (response.status === 401) { onUnauthorized?.(); throw new Error('Session ended.') }
    if (!response.ok) { const error = new Error(response.status === 503 ? 'Native commands are not enabled yet.' : 'Native action unavailable. Inspect its current state before retrying.'); error.status = response.status; throw error }
    const value = await response.json()
    if (!alive.current || owner.current !== csrfToken || epoch !== generation.current) throw new Error('Session changed.')
    if (path === 'jobs/start' || path === 'jobs/read' || path === 'jobs/resolve') {
      if (!value || !/^[a-f0-9]{32}$/.test(value.id) || !['running', 'completed', 'failed', 'unknown', 'inspected'].includes(value.state) || typeof value.interactive !== 'boolean' || typeof value.output !== 'string' || value.output.length > 43692 || !/^[A-Za-z0-9+/]*={0,2}$/.test(value.output) || !Number.isSafeInteger(value.cursor) || value.cursor < 0) throw new Error('Native job response could not be confirmed. Inspect before retrying.')
    }
    return value
    } finally { clearTimeout(timeout); requests.current.delete(controller) }
  }, [csrfToken, onUnauthorized])

  useEffect(() => {
    alive.current = true
    generation.current += 1
    setInitializing(true); setCatalog([]); setJob(null); setConsoleOpen(false); setConsoleReady(false); setUnknown(false); cursor.current = 0
    let active = true
    request('commands').then((value) => { if (active && current() && Array.isArray(value)) setCatalog(value) }).catch((error) => { if (active && current()) setNotice(error.message) })
    // Reattach to this session's existing job; never start one while navigating.
    request('jobs/read', { id: '', cursor: 0 }).then((value) => { if (active && current()) { setJob(value); setUnknown(value.state === 'unknown') } }).catch(() => {}).finally(() => { if (active && current()) setInitializing(false) })
    return () => { active = false; alive.current = false; generation.current += 1; queuedInput.current = { bytes: 0, count: 0, epoch: queuedInput.current.epoch + 1 }; requests.current.forEach((controller) => controller.abort()); consumed.current?.(); consumed.current = null }
  }, [request])

  useEffect(() => {
    if (!jobNotification?.id || !current()) return
    cursor.current = 0
    queuedInput.current = { bytes: 0, count: 0, epoch: queuedInput.current.epoch + 1 }
    consumed.current?.(); consumed.current = null
    setJob(jobNotification); setSelected(null); setUnknown(jobNotification.state === 'unknown')
    setConsoleReady(false); setConsoleOpen(true); setChunk(null); setInputFault(false)
  }, [jobNotification?.id])

  useEffect(() => {
    if (!job?.id || job.state !== 'running' && !(consoleOpen && consoleReady)) return
    let stopped = false
    let timer
    async function read() {
      try {
        const value = await request('jobs/read', { id: job.id, cursor: consoleOpen && consoleReady ? cursor.current : Number.MAX_SAFE_INTEGER })
        if (stopped) return
        setJob(value)
        if (consoleOpen && consoleReady) {
          await new Promise((resolve) => { consumed.current = resolve; setChunk(value) })
          consumed.current = null
          if (stopped) return
          cursor.current = value.cursor
        }
        if (value.state === 'unknown') { setUnknown(true); setNotice('Outcome unknown. Inspect XKeen before another change.') }
        if (value.state === 'running' || consoleOpen && consoleReady && value.output) timer = setTimeout(read, value.state === 'running' ? 750 : 0)
        else { setNotice(value.state === 'completed' ? 'Command finished. Refresh status to inspect the result; this does not prove VPN health.' : 'Native command failed. Inspect the console output.'); refresh.current?.() }
      } catch (error) { if (!stopped) { setUnknown(true); setNotice(error.message) } }
    }
    read()
    return () => { stopped = true; clearTimeout(timer); consumed.current?.(); consumed.current = null }
  }, [job?.id, consoleOpen, consoleReady, request])

  const busy = initializing || pending || job?.state === 'running' || unknown
  async function start(event) {
    event.preventDefault()
    if (!selected || busy) return
    setPending(true); setNotice(''); cursor.current = 0; setChunk(null); setConsoleReady(false); setInputFault(false)
    queuedInput.current = { bytes: 0, count: 0, epoch: queuedInput.current.epoch + 1 }
    try {
      const value = await request('jobs/start', { action: selected.action, ...(selected.parameter ? { parameter } : {}) })
      if (current()) { setJob(value); setSelected(null); setConsoleOpen(value.interactive) }
    } catch (error) {
      // A lost response may hide an accepted mutation. Never replay Start.
      if (current()) { setUnknown(!error.status || error.status >= 500 && error.status !== 503); setNotice(error.message) }
    } finally { if (current()) setPending(false) }
  }
  async function input(data) {
    if (!job?.interactive || job.state !== 'running' || inputFault || !current()) return
    const bytes = new TextEncoder().encode(data).length
    if (bytes > 4096) { setNotice('Input is too long. Send a shorter answer.'); return }
    const queue = queuedInput.current
    if (queue.count >= 64 || queue.bytes + bytes > 16384) { setNotice('Input queue is full. Wait for XKeen to accept the pending answer.'); return }
    queue.count += 1; queue.bytes += bytes
    const epoch = queue.epoch
    inputQueue.current = inputQueue.current.then(async () => { if (current() && currentJob.current === job.id && queuedInput.current.epoch === epoch) await request('jobs/input', { id: job.id, data }) }).catch(() => {
      if (current() && currentJob.current === job.id && queuedInput.current.epoch === epoch) { setInputFault(true); setNotice('Input delivery could not be confirmed. Inspect XKeen; the answer will not be sent again.'); queuedInput.current = { bytes: 0, count: 0, epoch: epoch + 1 } }
    }).finally(() => { if (queuedInput.current.epoch === epoch) { queue.count -= 1; queue.bytes -= bytes } })
  }
  async function resize(cols, rows) {
    if (!job?.interactive || job.state !== 'running') return
    try { await request('jobs/resize', { id: job.id, cols, rows }) } catch { /* A viewer resize never restarts the job. */ }
  }
  async function cancel() {
    if (job?.state !== 'running') return
    try { await request('jobs/cancel', { id: job.id }); if (alive.current) setNotice('Cancellation requested. Inspect the final state.') } catch (error) { if (alive.current) setNotice(error.message) }
  }
  async function resolveInspection() {
    if (!unknown || !job || pending) return
    setPending(true)
    try {
      const value = await request('jobs/resolve', { id: job.id, inspected: true })
      if (current()) { setJob(value); setUnknown(false); setNotice('Inspection recorded. The command was not repeated and its outcome remains unconfirmed.'); refresh.current?.() }
    } catch (error) { if (current()) setNotice(error.message) }
    finally { if (current()) setPending(false) }
  }
  async function openConsole() {
    if (consoleOpen) return
    setConsoleOpen(true); cursor.current = 0
  }
  function choose(command) { setSelected(command); setParameter(command.parameter === 'state' ? 'off' : command.parameter === 'version' ? 'auto' : '') }

  return <Card>
    <CardHeader><CardTitle>Native XKeen commands</CardTitle><CardDescription>XKeen performs these actions itself. Console output stays private to your session.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      <div className="grid gap-4 sm:grid-cols-2">{[
        { title: 'Service', icon: IconPlayerPlay, description: 'Run and inspect the native VPN service.', match: (id) => ['start','stop','restart','status','test-xray'].includes(id) },
        { title: 'Updates', icon: IconRefresh, description: 'Use XKeenâ€™s own component installers and updaters.', match: (id) => id.startsWith('update-') },
        { title: 'Schedules', icon: IconCalendar, description: 'Configure the cron jobs maintained by XKeen.', match: (id) => id.includes('schedule') },
        { title: 'Network & tools', icon: IconTools, description: 'Native network modes, diagnostics and maintenance.', match: (id) => !['start','stop','restart','status','test-xray'].includes(id) && !id.startsWith('update-') && !id.includes('schedule') },
      ].map((group) => <section key={group.title} className="rounded-lg border p-4"><h3 className="flex items-center gap-2 font-semibold"><group.icon className="text-info" />{group.title}</h3><p className="my-2 text-sm text-muted-foreground">{group.description}</p><div className="flex flex-wrap gap-2">{catalog.filter((command) => group.match(command.action)).map((command) => <Button key={command.action} aria-label={command.label} variant="outline" disabled={busy} onClick={() => choose(command)}>{command.label}{command.interactive && <span className="text-xs text-muted-foreground"> · console</span>}</Button>)}</div></section>)}</div>
      {selected && <form onSubmit={start} className="flex flex-col gap-3">
        <p>{selected.label}{selected.interactive ? ' — answer the native prompts in the console.' : ''}</p>
        {selected.parameter && <Field><FieldLabel htmlFor="native-parameter">{selected.parameter === 'state' ? 'State: on or off' : selected.parameter === 'version' ? 'Version or auto' : 'Ports/ranges, for example 80 443 1000:2000'}</FieldLabel><Input id="native-parameter" value={parameter} maxLength={512} onChange={(event) => setParameter(event.target.value)} disabled={busy} /></Field>}
        <div className="flex gap-2"><Button type="submit" disabled={busy}>Run native command</Button><Button type="button" variant="outline" disabled={pending} onClick={() => setSelected(null)}>Cancel</Button></div>
      </form>}
      {notice && <p role="status">{notice}</p>}
      {unknown && job && <div className="flex flex-col gap-2"><p>Review the console and current XKeen status before enabling another change. This does not repeat the interrupted command.</p><Button variant="outline" onClick={() => refresh.current?.()}>Refresh current status</Button><Button variant="outline" disabled={pending} onClick={resolveInspection}>I inspected XKeen; allow new actions</Button></div>}
      {job && <div className="flex flex-wrap items-center gap-2"><span>{job.action}: {job.state}</span><Button variant="outline" onClick={openConsole}>Console output</Button>{job.state === 'running' && <Button variant="outline" onClick={cancel}>Interrupt command</Button>}</div>}
      {consoleOpen && job && <Suspense fallback={<p>Loading console…</p>}><NativeConsole key={job.id} chunk={chunk} interactive={job.interactive && job.state === 'running' && !inputFault} onInput={input} onResize={resize} onCancel={cancel} onReady={() => setConsoleReady(true)} onConsumed={() => consumed.current?.()} /></Suspense>}
    </CardContent>
  </Card>
}
