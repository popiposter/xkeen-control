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
    return () => { observer.disconnect(); input.dispose(); clipboard.dispose(); links.dispose(); terminal.current = null; term.dispose() }
  }, [])
  useEffect(() => { if (terminal.current) terminal.current.options.disableStdin = !interactive }, [interactive])
  useEffect(() => {
    if (!chunk || !terminal.current) return
    if (chunk.truncated) terminal.current.writeln('\r\n[Earlier output omitted]\r\n')
    if (chunk.output) terminal.current.write(Uint8Array.from(atob(chunk.output), (character) => character.charCodeAt(0)), () => callbacks.current.onConsumed?.())
    else callbacks.current.onConsumed?.()
  }, [chunk])
  return <div ref={container} className="h-80 w-full overflow-hidden rounded-md bg-black p-2" aria-label="Native XKeen console" />
}
