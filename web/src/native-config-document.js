import { applyEdits, findNodeAtLocation, format, getNodeValue, modify, parseTree, printParseErrorCode } from 'jsonc-parser'

export function inspectDocument(text) {
  const errors = []
  const tree = parseTree(text, errors, { allowTrailingComma: false })
  if (errors.length) {
    const error = errors[0]
    const prefix = text.slice(0, error.offset).split('\n')
    return { error: `${printParseErrorCode(error.error)} at line ${prefix.length}, column ${prefix.at(-1).length + 1}` }
  }
  if (!tree || tree.type !== 'object') return { error: 'The configuration must be a JSON object.' }
  function check(node, depth) {
    if (depth > 64) throw new Error('Configuration nesting exceeds 64 levels.')
    if (node.type === 'object') {
      const names = new Set()
      for (const child of node.children || []) {
        const key = child.children[0].value
        if (names.has(key)) throw new Error('Duplicate property in configuration.')
        names.add(key)
      }
    }
    for (const child of node.children || []) check(child, depth + 1)
  }
  try { check(tree, 0) } catch (error) { return { error: error.message } }
  return { tree }
}

export function documentField(tree, area, field) {
  const node = findNodeAtLocation(tree, [area, field])
  return node ? getNodeValue(node) : undefined
}

export function editDocumentField(text, area, field, value) {
  const parsed = inspectDocument(text)
  if (parsed.error || !findNodeAtLocation(parsed.tree, [area])) throw new Error(parsed.error || 'This native section is absent; edit it in Text mode.')
  return applyEdits(text, modify(text, [area, field], value, { formattingOptions: { insertSpaces: true, tabSize: 2 } }))
}

export function formatDocument(text) {
  const parsed = inspectDocument(text)
  if (parsed.error) throw new Error(parsed.error)
  return applyEdits(text, format(text, undefined, { insertSpaces: true, tabSize: 2, eol: '\n' }))
}
