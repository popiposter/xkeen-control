// Test-only mounted controller: change CSRF without replacing the React root.
// It is never imported by the application's production entrypoint.
import { StrictMode, useCallback, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ComponentsUpdatesSection, useComponentsController } from '../../src/components-updates.jsx'
import '../../src/styles.css'

const lifecycle = { maintenance: false, applying: false }

function SessionFixture() {
  const [csrfToken, setCSRFToken] = useState('synthetic-session-A')
  const [unauthorized, setUnauthorized] = useState(0)
  const onUnauthorized = useCallback(() => setUnauthorized((value) => value + 1), [])
  const controller = useComponentsController({ csrfToken, lifecycle, onUnauthorized })
  return <main>
    <button type="button" onClick={() => setCSRFToken('synthetic-session-B')}>Replace CSRF</button>
    <button type="button">Focus sentinel</button>
    <output data-testid="session">{csrfToken}</output>
    <output data-testid="unauthorized">{unauthorized}</output>
    <output data-testid="refresh-error">{controller.refreshError}</output>
    <ComponentsUpdatesSection controller={controller} lifecycle={lifecycle} onOpenSystem={() => {}} />
  </main>
}

createRoot(document.getElementById('root')).render(<StrictMode><SessionFixture /></StrictMode>)
