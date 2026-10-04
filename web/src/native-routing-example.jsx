import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from '@/components/ui/card'
import { Field, FieldLabel, FieldGroup } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'

export default function RoutingExample({ text, request, disabled }) {
  const [sample, setSample] = useState({ domain: '', ip: '', inbound: '', network: 'tcp', port: 443 })
  const [result, setResult] = useState(null)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState('')
  const generation = useRef(0)
  const current = useRef({ text, sample })
  current.current = { text, sample }
  useEffect(() => { generation.current++; setResult(null); setBusy(false); setNotice('') }, [text, sample])
  useEffect(() => () => { generation.current++ }, [])
  async function check() {
    const id = ++generation.current
    const inspected = current.current
    setBusy(true); setNotice(''); setResult(null)
    try {
      const value = await request('example', { text: inspected.text, sample: inspected.sample })
      if (generation.current !== id || current.current.text !== inspected.text || current.current.sample !== inspected.sample) return
      if (!['matched', 'unknown', 'default'].includes(value.state)) throw new Error('Routing example response unavailable.')
      setResult(value)
    } catch (error) { if (generation.current === id) setNotice(error.message) }
    finally { if (generation.current === id) setBusy(false) }
  }
  return <Card>
    <CardHeader><CardTitle>Check a routing example</CardTitle><CardDescription>Uses this draft and the facts you enter. No DNS lookup, traffic probe, Save or Restart. Missing facts and unsupported conditions are reported as uncertain.</CardDescription></CardHeader>
    <CardContent><FieldGroup>
      {['domain', 'ip', 'inbound'].map((key) => <Field key={key}><FieldLabel htmlFor={'example-' + key}>{key === 'domain' ? 'Domain name' : key === 'ip' ? 'Known destination IP' : 'Inbound tag'}</FieldLabel><Input id={'example-' + key} disabled={disabled} maxLength={key === 'domain' ? 253 : 128} value={sample[key]} onChange={(event) => setSample({ ...sample, [key]: event.target.value })} /></Field>)}
      <Field><FieldLabel htmlFor="example-port">Destination port</FieldLabel><Input id="example-port" type="number" min="1" max="65535" disabled={disabled} value={sample.port || ''} onChange={(event) => setSample({ ...sample, port: Number(event.target.value) })} /></Field>
      <Field><FieldLabel htmlFor="example-network">Network</FieldLabel><NativeSelect id="example-network" disabled={disabled} value={sample.network} onChange={(event) => setSample({ ...sample, network: event.target.value })}><option value="tcp">TCP</option><option value="udp">UDP</option></NativeSelect></Field>
      <Button variant="outline" disabled={disabled || busy || !sample.domain && !sample.ip} onClick={() => void check()}>{busy ? 'Checking example…' : 'Check routing example'}</Button>
      {notice && <p role="alert">{notice}</p>}
      {result && <p role="status">{result.state === 'matched' ? 'Rule ' + result.rule + ': ' + result.targetKind + ' ' + result.target : (result.rule ? 'Rule ' + result.rule + ': ' : '') + result.reason}</p>}
    </FieldGroup></CardContent>
  </Card>
}
