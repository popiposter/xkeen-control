import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { CSPProvider } from '@base-ui/react/csp-provider'
import { useEffect, useRef } from 'react'
import { IconChevronRight, IconArrowUp, IconArrowDown, IconTrash } from '@tabler/icons-react'

export function Modal({ label, busy, onCancel, returnFocus, children }) {
  return <CSPProvider disableStyleElements><Dialog open onOpenChange={(open, details) => {
    if (busy) { details.cancel(); return }
    if (!open) onCancel()
  }}>
        <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl" showCloseButton={false} finalFocus={returnFocus}>
          <DialogTitle className="sr-only">{label}</DialogTitle>
          {children}
        </DialogContent>
  </Dialog></CSPProvider>
}
export function MobileNavigationDrawer({ open, onClose, returnFocus, children }) {
  useEffect(() => {
    if (!open) return undefined
    const mobile = window.matchMedia('(max-width: 760px)')
    if (!mobile.matches) { onClose(); return undefined }
    const onViewportChange = () => { if (!mobile.matches) onClose() }
    mobile.addEventListener('change', onViewportChange)
    return () => mobile.removeEventListener('change', onViewportChange)
  }, [open, onClose])
  return <CSPProvider disableStyleElements><Sheet open={open} onOpenChange={(next) => { if (!next) onClose() }}>
    <SheetContent side="left" showCloseButton={false} className="overflow-y-auto p-4" finalFocus={returnFocus}>
      <SheetTitle className="sr-only">Navigation menu</SheetTitle>
      {open ? children : null}
    </SheetContent>
  </Sheet></CSPProvider>
}

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
