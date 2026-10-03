import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Checkbox } from '@/components/ui/checkbox'
import { Alert, AlertDescription } from '@/components/ui/alert'

const limit = 9 * 1024 * 1024
export function NativeTransferSection({ csrfToken, api, download, onUnauthorized, onRefresh, onStaged, onInspectConfigs }) {
  const [secret, setSecret] = useState({ currentPassword: '', passphrase: '', confirmation: '' })
  const [file, setFile] = useState(null)
  const [passphrase, setPassphrase] = useState('')
  const [mapping, setMapping] = useState({})
  const [preview, setPreview] = useState(null)
  const [checked, setChecked] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const [diagnostic, setDiagnostic] = useState(null)
  const [staged, setStaged] = useState(false)
  const upload = useRef(null)
  const token = useRef('')
  const generation = useRef(0)
  const owner = useRef(csrfToken)
  owner.current = csrfToken
  const post = (action, body, csrf = csrfToken) => api(`/api/v1/xkeen/transfer/${action}`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
  const cancel = (value, csrf = csrfToken) => value ? post('cancel', { token: value }, csrf).catch(() => {}) : Promise.resolve()
  useEffect(() => {
    generation.current++
    setSecret({ currentPassword: '', passphrase: '', confirmation: '' }); setFile(null); setPassphrase(''); setMapping({}); setPreview(null); setBusy(false); setNotice(''); setChecked(false); setStaged(false); setDiagnostic(null)
    if (upload.current) upload.current.value = ''
    return () => { generation.current++; const previous = token.current; token.current = ''; void cancel(previous, csrfToken) }
  }, [csrfToken])
  const valid = (id) => id === generation.current && owner.current === csrfToken
  const fail = (error) => {
    if (error.status === 401 && error.code !== 'reauthentication failed') { onUnauthorized(); return }
    setNotice(error.status === 401 ? 'Current panel password was not accepted.' : error.message || 'Transfer request failed. Inspect the saved configuration before retrying.')
    setDiagnostic(error.diagnostic || null)
  }
  const invalidate = () => {
    generation.current++
    const previous = token.current; token.current = ''
    setPreview(null); setChecked(false); setDiagnostic(null)
    void cancel(previous)
  }
  async function exportBackup(event) {
    event.preventDefault()
    const id = generation.current
    const form = secret
    setSecret({ currentPassword: '', passphrase: '', confirmation: '' })
    const bytes = new TextEncoder().encode(form.passphrase).length
    if (!form.currentPassword || bytes < 12 || bytes > 256 || form.passphrase !== form.confirmation) { setNotice('Enter a matching passphrase between 12 and 256 bytes.'); return }
    setBusy(true); setNotice(''); setDiagnostic(null)
    try {
      await download('/api/v1/backup/export-secret', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken }, body: JSON.stringify({ currentPassword: form.currentPassword, passphrase: form.passphrase }) }, 'xkeen-native-backup-encrypted.json')
      if (valid(id)) setNotice('Encrypted configuration backup downloaded. Keep its passphrase separately.')
    } catch (error) { if (valid(id)) fail(error) }
    finally { if (valid(id)) setBusy(false) }
  }
  async function previewUpload() {
    if (!file || busy) return
    if (file.size > limit) { setNotice('The backup exceeds the 9 MiB limit.'); return }
    const id = generation.current
    setBusy(true); setNotice(''); setDiagnostic(null)
    const form = new FormData()
    form.append('bundle', file); form.append('passphrase', passphrase); form.append('mapping', JSON.stringify(mapping))
    try {
      const value = await api('/api/v1/xkeen/transfer/preview', { method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: form })
      if (!valid(id)) { void cancel(value.token); return }
      if (!value.mappingRequired && !/^[a-f0-9]{32}$/.test(value.token)) throw new Error('Preview was not confirmed. Select the backup again.')
      token.current = value.token || ''; setPreview(value)
      if (value.mappingRequired) {
        setMapping(Object.fromEntries(value.references.map(({ source, destination }) => [source, destination])))
        setNotice('Choose destination interfaces, then check the backup again.')
      } else {
        setFile(null); setPassphrase(''); if (upload.current) upload.current.value = ''
        setNotice('Validated preview ready. The upload and passphrase were cleared.')
      }
    } catch (error) { if (valid(id)) fail(error) }
    finally { if (valid(id)) setBusy(false) }
  }
  async function stage() {
    if (busy || !checked || !token.current) return
    const id = generation.current; const value = token.current
    // Consume locally before the request. A lost response must never repeat Stage.
    token.current = ''; setPreview(null); setBusy(true); setNotice(''); setDiagnostic(null)
    try {
      const result = await post('stage', { token: value, nativeSettingsChecked: true })
      if (!valid(id)) return
      if (!result.saved || !/^[a-f0-9]{64}$/.test(result.digest)) throw new Error('Save outcome unavailable. Inspect saved configurations; do not repeat this transfer.')
      setStaged(true); onStaged?.(); setNotice('Configurations saved. Review the pending set in the configuration editor; Restart applies it. Discard restores the pre-transfer files.')
      await onRefresh?.()
    } catch (error) { if (valid(id)) { setStaged(true); onStaged?.(); fail(error) } }
    finally { if (valid(id)) setBusy(false) }
  }
  const refs = preview?.references || []
  return <div className="flex flex-col gap-4">
    {notice && <Alert><AlertDescription>{notice}</AlertDescription></Alert>}
    {diagnostic && <Card><CardHeader><CardTitle>Xray validation</CardTitle></CardHeader><CardContent><pre className="max-h-80 overflow-auto whitespace-pre-wrap">{diagnostic.output || diagnostic.message || JSON.stringify(diagnostic, null, 2)}</pre></CardContent></Card>}
    <Card><CardHeader><CardTitle>Back up native settings</CardTitle><CardDescription>Encrypted Xray configuration, nodes and subscriptions. Executables, panel login, native operating mode and cron jobs are not copied.</CardDescription></CardHeader><CardContent>
      <form onSubmit={exportBackup}><FieldGroup>
        <Field><FieldLabel htmlFor="transfer-password">Current panel password</FieldLabel><Input id="transfer-password" type="password" autoComplete="current-password" value={secret.currentPassword} disabled={busy} onChange={(e) => setSecret({ ...secret, currentPassword: e.target.value })} /></Field>
        <Field><FieldLabel htmlFor="transfer-export-passphrase">Encryption passphrase</FieldLabel><Input id="transfer-export-passphrase" type="password" autoComplete="new-password" value={secret.passphrase} disabled={busy} onChange={(e) => setSecret({ ...secret, passphrase: e.target.value })} /></Field>
        <Field><FieldLabel htmlFor="transfer-confirm">Confirm passphrase</FieldLabel><Input id="transfer-confirm" type="password" autoComplete="new-password" value={secret.confirmation} disabled={busy} onChange={(e) => setSecret({ ...secret, confirmation: e.target.value })} /></Field>
        <Button type="submit" disabled={busy}>Download encrypted backup</Button>
      </FieldGroup></form>
    </CardContent></Card>
    <Card><CardHeader><CardTitle>Transfer native settings</CardTitle><CardDescription>Preview never changes the router. Saving replaces the fixed configurations and managed nodes together, without restarting XKeen.</CardDescription></CardHeader><CardContent><FieldGroup>
      <Field><FieldLabel htmlFor="transfer-file">Backup bundle</FieldLabel><Input id="transfer-file" ref={upload} type="file" accept="application/json,.json" disabled={busy} onChange={(e) => { invalidate(); setFile(e.target.files?.[0] || null); setMapping({}); setPassphrase(''); setNotice('') }} /></Field>
      <Field><FieldLabel htmlFor="transfer-passphrase">Backup passphrase</FieldLabel><Input id="transfer-passphrase" type="password" autoComplete="off" value={passphrase} disabled={busy || Boolean(preview?.token)} onChange={(e) => { invalidate(); setPassphrase(e.target.value) }} /></Field>
      {preview?.mappingRequired && refs.map(({ source }) => <Field key={source}><FieldLabel htmlFor={`transfer-interface-${source}`}>Interface {source}</FieldLabel><NativeSelect id={`transfer-interface-${source}`} value={mapping[source] || ''} disabled={busy} onChange={(e) => { setMapping({ ...mapping, [source]: e.target.value }); setChecked(false) }}><option value="">Choose destination</option>{preview.interfaces.map((name) => <option key={name} value={name}>{name}</option>)}</NativeSelect></Field>)}
      {!preview?.token && <Button type="button" onClick={previewUpload} disabled={busy || !file || !passphrase || preview?.mappingRequired && refs.some(({ source }) => !mapping[source])}>{busy ? 'Checking…' : 'Preview transfer'}</Button>}
      {preview?.token && <>
        <p>{preview.files.length} configuration files · {preview.nodes} nodes · {preview.subscriptions} subscriptions</p>
        <p>Interfaces: {refs.map((r) => `${r.source} → ${r.destination}`).join(', ') || 'No fixed interface references'}</p>
        <p>Preview expires {new Date(preview.expiresAt).toLocaleTimeString()}.</p>
        <Field orientation="horizontal"><Checkbox id="transfer-native-settings" checked={checked} onCheckedChange={setChecked} disabled={busy} /><FieldLabel htmlFor="transfer-native-settings">I checked this router's native mode, client policy and update schedules; those settings stay on this router.</FieldLabel></Field>
        <div className="flex flex-wrap gap-2"><Button type="button" onClick={stage} disabled={busy || !checked}>Save transferred settings</Button><Button type="button" variant="outline" disabled={busy} onClick={() => { invalidate(); setNotice('Transfer preview canceled.') }}>Cancel preview</Button></div>
      </>}
    </FieldGroup></CardContent></Card>
    {staged && <Button type="button" variant="outline" onClick={onInspectConfigs}>Review saved configurations</Button>}
  </div>
}
