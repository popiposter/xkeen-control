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

export function documentNode(tree, path) { return findNodeAtLocation(tree, path) }
export function nodeValue(tree, path) {
  const node = documentNode(tree, path)
  return node ? getNodeValue(node) : undefined
}
export function editDocumentPath(text, path, value) {
  const parsed = inspectDocument(text)
  if (parsed.error) throw new Error(parsed.error)
  return applyEdits(text, modify(text, path, value, { formattingOptions: { insertSpaces: true, tabSize: 2 } }))
}
export function appendDocumentItem(text, path, value) {
  const parsed = inspectDocument(text)
  if (parsed.error) throw new Error(parsed.error)
  const array = documentNode(parsed.tree, path)
  if (!array) return editDocumentPath(text, path, [value])
  if (array.type !== 'array') throw new Error('This native value is not an array. Edit it in Text mode.')
  const at = array.offset + array.length - 1
  return text.slice(0, at) + `${array.children?.length ? ',' : ''}\n${JSON.stringify(value)}\n` + text.slice(at)
}
export function moveDocumentItem(text, path, index, destination) {
  const parsed = inspectDocument(text)
  if (parsed.error) throw new Error(parsed.error)
  const children = documentNode(parsed.tree, path)?.children || []
  if (Math.abs(destination - index) !== 1 || !children[index] || !children[destination]) return text
  const a = children[index], b = children[destination]
  const first = a.offset < b.offset ? a : b, second = a.offset < b.offset ? b : a
  // Swap raw node tokens. No parse/stringify of opaque rule fields/numbers.
  return text.slice(0, first.offset) + text.slice(second.offset, second.offset + second.length) + text.slice(first.offset + first.length, second.offset) + text.slice(first.offset, first.offset + first.length) + text.slice(second.offset + second.length)
}
