import { Dialog } from '@base-ui/react/dialog'
import { CSPProvider } from '@base-ui/react/csp-provider'
import { useEffect, useRef } from 'react'
import { IconChevronRight, IconArrowUp, IconArrowDown, IconTrash } from '@tabler/icons-react'

export function Modal({ label, busy, onCancel, returnFocus, children }) {
  return <CSPProvider disableStyleElements><Dialog.Root open onOpenChange={(open, details) => {
    if (busy) { details.cancel(); return }
    if (!open) onCancel()
  }}>
    <Dialog.Portal>
      <Dialog.Backdrop className="dialog-backdrop" />
      <Dialog.Viewport className="dialog-viewport">
        <Dialog.Popup className="preview-dialog" finalFocus={returnFocus}>
          <Dialog.Title className="visually-hidden">{label}</Dialog.Title>
          {children}
        </Dialog.Popup>
      </Dialog.Viewport>
    </Dialog.Portal>
  </Dialog.Root></CSPProvider>
}
export function MobileNavigationDrawer({ open, onClose, children }) {
  const dialogRef = useRef(null)
  useEffect(() => {
    if (!open) return undefined
    const dialog = dialogRef.current
    const mobile = window.matchMedia('(max-width: 760px)')
    if (!mobile.matches) { onClose(); return undefined }
    const previousOverflow = document.body.style.overflow
    dialog.showModal()
    document.body.style.overflow = 'hidden'
    const onViewportChange = () => { if (!mobile.matches) onClose() }
    mobile.addEventListener('change', onViewportChange)
    return () => {
      mobile.removeEventListener('change', onViewportChange)
      dialog.close()
      document.body.style.overflow = previousOverflow
    }
  }, [open, onClose])
  return <dialog ref={dialogRef} className="mobile-navigation-drawer" aria-label="Navigation menu" onClose={onClose} onKeyDown={(event) => {
    if (event.key !== 'Tab') return
    const buttons = [...event.currentTarget.querySelectorAll('button:not(:disabled)')].filter((button) => button.getClientRects().length)
    const first = buttons[0], last = buttons[buttons.length - 1]
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
  }} onClick={(event) => {
    if (event.target !== event.currentTarget) return
    const bounds = event.currentTarget.getBoundingClientRect()
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) onClose()
  }}>{open ? children : null}</dialog>
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
