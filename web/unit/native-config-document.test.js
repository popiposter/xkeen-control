import assert from 'node:assert/strict'
import test from 'node:test'
import { documentField, editDocumentField, formatDocument, inspectDocument } from '../src/native-config-document.js'

test('form edits and formatting retain comments and exact unknown numeric tokens', () => {
  const original = '{/* keep */"dns":{"queryStrategy":"UseIP","opaque":9007199254740993},"future":true}'
  const edited = editDocumentField(original, 'dns', 'queryStrategy', 'UseIPv4')
  assert.equal(documentField(inspectDocument(edited).tree, 'dns', 'queryStrategy'), 'UseIPv4')
  const pretty = formatDocument(edited)
  assert.ok(pretty.includes('/* keep */'))
  assert.ok(pretty.includes('9007199254740993'))
  assert.ok(pretty.includes('"future": true'))
})

test('invalid, duplicate, trailing-comma and nonobject drafts remain invalid', () => {
  for (const input of ['{"dns":', '{"dns":{},"dns":{}}', '{"dns":{},}', '[]']) {
    assert.ok(inspectDocument(input).error)
    assert.throws(() => formatDocument(input))
  }
  assert.throws(() => editDocumentField('{}', 'dns', 'queryStrategy', 'UseIP'))
})
