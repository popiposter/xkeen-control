import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

async function mountEditor(page) {
  const model = await mountFeatureCompleteDashboard(page)
  const writes = []
  let reads = 0
  const original = '{/* keep DNS */"dns":{"queryStrategy":"UseIP","future":9007199254740993}}'
  const documents = { '02_dns.json': { text: original }, '05_routing.json': { text: '{"routing":{"domainStrategy":"AsIs","rules":[]}}' } }
  await page.route('**/api/v1/xkeen/config/workspace', (route) => {
    reads++
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: 'a'.repeat(64), documents: Object.fromEntries(Object.keys(documents).map((id) => [id, {}])) }) })
  })
  await page.route('**/api/v1/xkeen/config/document', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: 'a'.repeat(64), document: documents[route.request().postDataJSON().file] }) }))
  await page.route('**/api/v1/xkeen/config/text', (route) => {
    writes.push(route.request().postDataJSON())
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: 'b'.repeat(64), saved: true, restartRequired: true }) })
  })
  await page.goto('/')
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  expect(reads).toBe(0)
  await page.getByRole('button', { name: 'Edit native configuration', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIP')
  return { model, writes, original }
}

test('Form/Text share edits, formatting and undo without saving or restarting', async ({ page }) => {
  const { model, writes } = await mountEditor(page)
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv4')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'Configuration text' })
  await expect(editor).toContainText('UseIPv4')
  await expect(editor).toContainText('9007199254740993')
  await page.getByRole('button', { name: 'Format JSON', exact: true }).click()
  await expect(editor).toContainText('keep DNS')
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv4')
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv4')
  await page.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved and validated' })).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0].text).toContain('keep DNS')
  expect(writes[0].text).toContain('9007199254740993')
  expect(model.requests.filter((request) => request.path === '/api/v1/xkeen/jobs/start')).toEqual([])
  expect(model.issues).toEqual([])
})

test('invalid Text remains a savable private draft and cannot replace native files', async ({ page }) => {
  const { writes } = await mountEditor(page)
  const drafts = []
  await page.route('**/api/v1/xkeen/config/draft', (route) => {
    drafts.push(route.request().postDataJSON())
    return route.fulfill({ contentType: 'application/json', body: '{"saved":true}' })
  })
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'Configuration text' })
  await editor.fill('{ invalid')
  await expect(page.getByRole('button', { name: 'Save configuration', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('line')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(editor).toContainText('{ invalid')
  await page.getByRole('button', { name: 'Save draft', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Draft saved' })).toBeVisible()
  expect(drafts).toEqual([{ file: '02_dns.json', text: '{ invalid' }])
  expect(writes).toEqual([])
})
