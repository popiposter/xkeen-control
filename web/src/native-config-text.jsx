import { useEffect, useRef } from 'react'
import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightSpecialChars, drawSelection } from '@codemirror/view'
import { Annotation, Compartment, EditorState } from '@codemirror/state'
import { defaultKeymap, indentWithTab } from '@codemirror/commands'
import { json } from '@codemirror/lang-json'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'

export default function NativeConfigText({ text, onChange, disabled, onUndo, onRedo }) {
  const element = useRef(null)
  const editor = useRef(null)
  const callbacks = useRef({ onChange, onUndo, onRedo, disabled })
  const readOnly = useRef(new Compartment())
  callbacks.current = { onChange, onUndo, onRedo, disabled }
  useEffect(() => {
    const view = new EditorView({ parent: element.current, state: EditorState.create({ doc: text, extensions: [
      EditorView.cspNonce.of(document.querySelector('meta[name="style-nonce"]')?.content || ''),
      lineNumbers(), highlightActiveLine(), highlightSpecialChars(), drawSelection(), json(), syntaxHighlighting(HighlightStyle.define([{ tag: tags.string, color: 'var(--syntax-string)' }, { tag: tags.number, color: 'var(--syntax-number)' }, { tag: [tags.bool, tags.null], color: 'var(--syntax-keyword)' }, { tag: tags.propertyName, color: 'var(--syntax-property)' }, { tag: tags.punctuation, color: 'var(--muted-foreground)' }, { tag: tags.comment, color: 'var(--muted-foreground)', fontStyle: 'italic' }])),
      EditorView.lineWrapping,
      readOnly.current.of([EditorState.readOnly.of(disabled), EditorView.editable.of(!disabled)]),
      EditorState.transactionFilter.of((transaction) => {
        if (!transaction.docChanged || transaction.annotation(external)) return transaction
        if (callbacks.current.disabled || new TextEncoder().encode(transaction.newDoc.toString()).length > (2 << 20)) return []
        return transaction
      }),
      EditorView.contentAttributes.of({ 'aria-label': 'Configuration text', spellcheck: 'false', autocapitalize: 'off' }),
      EditorView.theme({ '&': { color: 'var(--foreground)', backgroundColor: 'var(--background)', minHeight: '18rem', maxHeight: '36rem', border: '1px solid var(--border)', borderRadius: 'var(--radius)' }, '.cm-cursor': { borderLeftColor: 'var(--foreground)' }, '.cm-activeLine': { backgroundColor: 'var(--muted)' }, '.cm-activeLineGutter': { backgroundColor: 'var(--accent)' }, '.cm-selectionBackground': { backgroundColor: 'var(--accent) !important' }, '.cm-scroller': { overflow: 'auto', fontFamily: 'monospace' }, '.cm-gutters': { backgroundColor: 'var(--muted)', color: 'var(--muted-foreground)', border: 'none' } }),
      keymap.of([{ key: 'Mod-z', run: () => { callbacks.current.onUndo(); return true } }, { key: 'Mod-Shift-z', run: () => { callbacks.current.onRedo(); return true } }, ...defaultKeymap, indentWithTab]),
      EditorView.updateListener.of((update) => { if (update.docChanged && !update.transactions.some((t) => t.annotation(external))) callbacks.current.onChange(update.state.doc.toString()) }),
    ] }) })
    editor.current = view
    return () => { editor.current = null; view.destroy() }
  }, [])
  useEffect(() => { const view = editor.current; if (view && text !== view.state.doc.toString()) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text }, annotations: external.of(true) }) }, [text])
  useEffect(() => { editor.current?.dispatch({ effects: readOnly.current.reconfigure([EditorState.readOnly.of(disabled), EditorView.editable.of(!disabled)]) }) }, [disabled])
  return <div ref={element} />
}

const external = Annotation.define()
