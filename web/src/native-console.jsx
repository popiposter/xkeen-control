import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

export default function NativeConsole({ chunk, interactive, onInput, onResize, onCancel, onReady, onConsumed }) {
  const container = useRef(null)
  const terminal = useRef(null)
  const callbacks = useRef({ onInput, onResize, onCancel, onReady, onConsumed })
  callbacks.current = { onInput, onResize, onCancel, onReady, onConsumed }
  useEffect(() => {
    const term = new Terminal({ rows: 24, cols: 100, scrollback: 500, fontSize: 13, disableStdin: true, allowProposedApi: false, linkHandler: { activate() {} } })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(container.current)
    // Native ANSI sequences are rendered by xterm, never interpreted as markup.
    const refreshTheme = () => { const dark = document.documentElement.classList.contains('dark'); term.options.theme = { background: dark ? '#171717' : '#ffffff', foreground: dark ? '#fafafa' : '#171717', cursor: dark ? '#fafafa' : '#171717', red: '#ef4444', green: '#22c55e', yellow: '#eab308', blue: '#3b82f6', magenta: '#a855f7', cyan: '#06b6d4', brightRed: '#f87171', brightGreen: '#4ade80', brightYellow: '#facc15', brightBlue: '#60a5fa' } }
    refreshTheme()
    const themeObserver = new MutationObserver(refreshTheme)
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    terminal.current = term
    callbacks.current.onReady?.()
    const clipboard = term.parser.registerOscHandler(52, () => true)
    const links = term.parser.registerOscHandler(8, () => true)
    term.attachCustomKeyEventHandler((event) => {
      if (event.type === 'keydown' && event.ctrlKey && event.key.toLowerCase() === 'c') {
        callbacks.current.onCancel?.()
        return false
      }
      return true
    })
    const input = term.onData((data) => callbacks.current.onInput?.(data))
    const resize = () => {
      fit.fit()
      callbacks.current.onResize?.(Math.max(20, Math.min(300, term.cols)), Math.max(5, Math.min(100, term.rows)))
    }
    const observer = new ResizeObserver(resize)
    observer.observe(container.current)
    resize()
    return () => { themeObserver.disconnect(); observer.disconnect(); input.dispose(); clipboard.dispose(); links.dispose(); terminal.current = null; term.dispose() }
  }, [])
  useEffect(() => { if (terminal.current) terminal.current.options.disableStdin = !interactive }, [interactive])
  useEffect(() => {
    if (!chunk || !terminal.current) return
    if (chunk.truncated) terminal.current.writeln('\r\n[Earlier output omitted]\r\n')
    if (chunk.output) terminal.current.write(Uint8Array.from(atob(chunk.output), (character) => character.charCodeAt(0)), () => callbacks.current.onConsumed?.())
    else callbacks.current.onConsumed?.()
  }, [chunk])
  return <div ref={container} className="h-80 w-full overflow-hidden rounded-md border bg-background p-2" aria-label="Native XKeen console" />
}
