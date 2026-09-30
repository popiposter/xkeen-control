import { useEffect, useRef } from 'react'
import { IconChevronRight, IconArrowUp, IconArrowDown, IconTrash } from '@tabler/icons-react'

export function RowAction({ action, label, className = '', ...props }) {
  const Icon = { up: IconArrowUp, down: IconArrowDown, remove: IconTrash }[action]
  return <button type="button" className={`icon-button ghost ${action === 'remove' ? 'danger-action' : ''} ${className}`} aria-label={label} title={label} data-tooltip={label} {...props}><Icon size={16} aria-hidden="true" /></button>
}

// Native disclosure preserves keyboard behavior and keeps controller ownership
// independent of presentation. An operation needing attention reveals itself.
export function Disclosure({ title, children, defaultOpen = false, attention = false, className = '', id }) {
  const region = useRef(null)
  useEffect(() => { if (attention && region.current) region.current.open = true }, [attention])
  return <details ref={region} id={id} className={`disclosure ${className}`} open={defaultOpen || undefined}>
    <summary><IconChevronRight size={16} aria-hidden="true" /><span>{title}</span></summary>
    <div className="disclosure-body">{children}</div>
  </details>
}

export function WorkflowSteps({ stage = 'edit', first = 'Edit' }) {
  const steps = [['edit', first], ['preview', 'Review'], ['apply', 'Confirm']]
  return <ol className="workflow-steps" aria-label="Operation steps">{steps.map(([key, label], index) => <li key={key} aria-current={stage === key ? 'step' : undefined}><span>{index + 1}</span>{label}</li>)}</ol>
}
