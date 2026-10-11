import { IconDeviceFloppy, IconRestore } from '@tabler/icons-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from './components/ui/button.jsx'
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './components/ui/card.jsx'
import { Alert, AlertTitle, AlertDescription } from './components/ui/alert.jsx'
import { Checkbox } from './components/ui/checkbox.jsx'
import { Input } from './components/ui/input.jsx'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from './components/ui/field.jsx'

const fields = ['enabled', 'window', 'refreshSubscriptions', 'review', 'updateGeodata', 'autoApply', 'speedCandidates', 'poolSize', 'recovery', 'report']
const windowPattern = /^([01]\d|2[0-3]):[0-5]\d$/

function validView(value) {
  return value && typeof value === 'object' && value.settings && value.defaults && value.bounds &&
    fields.every((key) => key in value.settings) && Array.isArray(value.bounds.speedCandidates) && Array.isArray(value.bounds.poolSize)
}

// Client-side bounds mirror the server's; the server stays the authority.
export function maintenanceDraftError(draft, bounds) {
  if (!windowPattern.test(draft.window)) return 'Enter the start time as HH:MM, for example 04:00.'
  const [minC, maxC] = bounds.speedCandidates
  const [minP, maxP] = bounds.poolSize
  if (!Number.isInteger(draft.speedCandidates) || draft.speedCandidates < minC || draft.speedCandidates > maxC) return `Speed-test between ${minC} and ${maxC} nodes.`
  if (!Number.isInteger(draft.poolSize) || draft.poolSize < minP || draft.poolSize > maxP) return `The pool holds between ${minP} and ${maxP} nodes.`
  if (draft.speedCandidates < draft.poolSize) return 'Speed-test at least as many nodes as the pool holds.'
  return ''
}

function Toggle({ id, label, description, checked, disabled, onChange, className }) {
  return <Field orientation="horizontal" className={className}>
    <Checkbox id={id} checked={checked} disabled={disabled} onCheckedChange={(value) => onChange(value === true)} />
    <FieldContent><FieldLabel htmlFor={id}>{label}</FieldLabel>{description && <FieldDescription>{description}</FieldDescription>}</FieldContent>
  </Field>
}

export function MaintenanceSettingsCard({ csrfToken, onUnauthorized, onSaved, automatic }) {
  const [view, setView] = useState(null)
  const [draft, setDraft] = useState(null)
  const [error, setError] = useState('')
  const [result, setResult] = useState('')
  const [pending, setPending] = useState(false)
  const mounted = useRef(false)
  const load = useCallback(async () => {
    try {
      const response = await fetch('/api/v1/performance/maintenance', { credentials: 'same-origin', cache: 'no-store' })
      if (response.status === 401) { onUnauthorized?.(); return }
      const value = response.ok ? await response.json() : null
      if (!validView(value)) throw new Error('invalid')
      if (!mounted.current) return
      setView(value); setDraft(value.settings); setError('')
    } catch { if (mounted.current) setError('Maintenance settings are unavailable.') }
  }, [onUnauthorized])
  useEffect(() => { mounted.current = true; load(); return () => { mounted.current = false } }, [load])
  if (!view || !draft) return <Card><CardHeader><CardTitle>Nightly maintenance</CardTitle></CardHeader><CardContent>{error ? <p role="alert">{error}</p> : <p className="text-muted-foreground">Loading settings…</p>}</CardContent></Card>
  const set = (key) => (value) => { setDraft((current) => ({ ...current, [key]: value })); setResult('') }
  const number = (key) => (event) => set(key)(event.target.value === '' ? NaN : Number(event.target.value))
  const draftError = maintenanceDraftError(draft, view.bounds)
  const changed = fields.some((key) => draft[key] !== view.settings[key])
  const off = !draft.enabled
  const save = async () => {
    if (pending || draftError || !changed) return
    setPending(true); setResult(''); setError('')
    try {
      const response = await fetch('/api/v1/performance/maintenance', {
        method: 'POST', credentials: 'same-origin', cache: 'no-store',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
        body: JSON.stringify(Object.fromEntries(fields.map((key) => [key, draft[key]]))),
      })
      if (response.status === 401) { onUnauthorized?.(); return }
      const value = response.ok ? await response.json() : null
      if (!validView(value)) throw new Error('invalid')
      if (!mounted.current) return
      setView(value); setDraft(value.settings); setResult('Settings saved. The next window is planned from them.'); onSaved?.()
    } catch {
      if (mounted.current) { setError('Settings were not saved. Reload them and try again.'); load() }
    } finally { if (mounted.current) setPending(false) }
  }
  return <Card>
    <CardHeader><CardTitle>Nightly maintenance</CardTitle><CardDescription>One window a night refreshes subscriptions, reviews nodes and updates geodata, so Xray restarts only then. Changes apply from the next window; a window already running finishes with the settings it started with.</CardDescription></CardHeader>
    <CardContent className="flex flex-col gap-4">
      {error && <Alert variant="destructive"><AlertTitle>Settings</AlertTitle><AlertDescription>{error}</AlertDescription></Alert>}
      {automatic === false && <p role="status">Automatic maintenance is unavailable on this panel process, so these settings have no effect until it is.</p>}
      <FieldGroup>
        <Toggle id="maintenance-enabled" label="Run the nightly window" checked={draft.enabled} disabled={pending} onChange={set('enabled')} description="Off: nothing below runs automatically. Subscriptions, speed tests and geodata stay available by hand." />
        <Field className="max-w-xs"><FieldLabel htmlFor="maintenance-window">Start time (router time)</FieldLabel><Input id="maintenance-window" type="time" step="60" value={draft.window} disabled={pending || off} onChange={(event) => set('window')(event.target.value)} /></Field>
        <FieldSet className="rounded-lg border p-4"><FieldLegend>Steps, in order</FieldLegend>
          <Toggle id="maintenance-subscriptions" label="Refresh subscriptions" checked={draft.refreshSubscriptions} disabled={pending || off} onChange={set('refreshSubscriptions')} description="Off: subscriptions refresh only when you or the Telegram command ask." />
          <Toggle id="maintenance-review" label="Review nodes" checked={draft.review} disabled={pending || off} onChange={set('review')} description="Probe every enabled node, then speed-test the fastest that answered." />
          <Toggle id="maintenance-apply" className="pl-6" label="Apply the best pool automatically" checked={draft.autoApply} disabled={pending || off || !draft.review} onChange={set('autoApply')} description="Off: the review leaves a recommendation for Apply on this page." />
          <Toggle id="maintenance-geodata" label="Update geodata" checked={draft.updateGeodata} disabled={pending || off} onChange={set('updateGeodata')} description="Runs XKeen's own geodata update last. Off: nothing updates geodata automatically unless you restore XKeen's own schedule in the XKeen console." />
        </FieldSet>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field><FieldLabel htmlFor="maintenance-candidates">Nodes to speed-test</FieldLabel><Input id="maintenance-candidates" type="number" min={view.bounds.speedCandidates[0]} max={view.bounds.speedCandidates[1]} value={Number.isFinite(draft.speedCandidates) ? draft.speedCandidates : ''} disabled={pending || off || !draft.review} onChange={number('speedCandidates')} /><FieldDescription>The fastest answering nodes. Fewer means less traffic and a shorter window.</FieldDescription></Field>
          <Field><FieldLabel htmlFor="maintenance-pool">Pool size</FieldLabel><Input id="maintenance-pool" type="number" min={view.bounds.poolSize[0]} max={view.bounds.poolSize[1]} value={Number.isFinite(draft.poolSize) ? draft.poolSize : ''} disabled={pending} onChange={number('poolSize')} /><FieldDescription>Nodes Xray balances between. Also used by availability recovery.</FieldDescription></Field>
        </div>
        <Toggle id="maintenance-recovery" label="Availability recovery" checked={draft.recovery} disabled={pending} onChange={set('recovery')} description="Every 10 minutes: if no pool member is healthy, apply a provisional pool of nodes that answer." />
        <Toggle id="maintenance-report" label="Telegram report after the window" checked={draft.report} disabled={pending || off} onChange={set('report')} description="Needs outbound notifications enabled in System." />
      </FieldGroup>
      {draftError && <p role="alert">{draftError}</p>}
      {result && <p role="status">{result}</p>}
    </CardContent>
    <CardFooter className="flex flex-wrap gap-2">
      <Button disabled={pending || !changed || !!draftError} onClick={save}><IconDeviceFloppy data-icon="inline-start" />{pending ? 'Saving…' : 'Save settings'}</Button>
      <Button variant="ghost" disabled={pending || fields.every((key) => draft[key] === view.defaults[key])} onClick={() => { setDraft(view.defaults); setResult('') }}><IconRestore data-icon="inline-start" />Reset to defaults</Button>
    </CardFooter>
  </Card>
}
