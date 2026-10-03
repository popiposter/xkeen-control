import assert from 'node:assert/strict'
import test from 'node:test'
import { appendDocumentItem, documentField, editDocumentField, editDocumentPath, formatDocument, inspectDocument, moveDocumentItem } from '../src/native-config-document.js'

test('form edits and formatting retain comments and exact unknown numeric tokens', () => {
  const original = '{/* keep */"dns":{"queryStrategy":"UseIP","opaque":9007199254740993},"future":true}'
  const edited = editDocumentField(original, 'dns', 'queryStrategy', 'UseIPv4')
  assert.equal(documentField(inspectDocument(edited).tree, 'dns', 'queryStrategy'), 'UseIPv4')
  const pretty = formatDocument(edited)
  assert.ok(pretty.includes('/* keep */'))
  assert.ok(pretty.includes('9007199254740993'))
  assert.ok(pretty.includes('"future": true'))
})

test('rule reorder and scoped form edits preserve opaque native tokens', () => {
  const original = '{"routing":{"rules":[{/* first */"type":"field","domain":["domain:a.example"],"outboundTag":"direct","future":9007199254740993},{"type":"field","domain":["domain:b.example"],"outboundTag":"vpn"}]}}'
  const moved = moveDocumentItem(original, ['routing', 'rules'], 0, 1)
  assert.ok(moved.indexOf('b.example') < moved.indexOf('a.example'))
  assert.ok(moved.includes('/* first */'))
  assert.ok(moved.includes('9007199254740993'))
  const edited = editDocumentPath(moved, ['routing', 'rules', 1, 'domain'], ['geosite:category-example'])
  assert.ok(edited.includes('9007199254740993'))
  const appended = appendDocumentItem(edited, ['routing', 'rules'], { type: 'field', ip: ['192.0.2.0/24'], outboundTag: 'block' })
  assert.ok(!inspectDocument(appended).error)
  assert.ok(appended.includes('9007199254740993'))
})

test('invalid, duplicate, trailing-comma and nonobject drafts remain invalid', () => {
  for (const input of ['{"dns":', '{"dns":{},"dns":{}}', '{"dns":{},}', '[]']) {
    assert.ok(inspectDocument(input).error)
    assert.throws(() => formatDocument(input))
  }
  assert.throws(() => editDocumentField('{}', 'dns', 'queryStrategy', 'UseIP'))
})
