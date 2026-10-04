import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'
import { revealNavigation } from './fixtures/disclosures.js'

async function fixture(page) {
  const model = await mountFeatureCompleteDashboard(page)
  const documents = {
    '02_dns.json': { text: '{"dns":{"servers":[{"address":"https://dns.example.test/dns-query","domains":["geosite:video"]}]}}' },
    '05_routing.json': { text: '{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["example.test"],"outboundTag":"direct"}]}}' },
  }
  await page.route('**/api/v1/xkeen/config/workspace', (route) => route.fulfill({ json: { digest: 'a'.repeat(64), documents: { '02_dns.json': {}, '05_routing.json': {} }, pending: null, targets: [{ tag: 'direct', kind: 'outbound' }] } }))
  await page.route('**/api/v1/xkeen/config/document', (route) => route.fulfill({ json: { digest: 'a'.repeat(64), document: documents[route.request().postDataJSON().file] } }))
  await page.goto('/')
  return model
}

test('appearance persists without secrets and follows system changes until explicitly selected', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await fixture(page)
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.getByLabel('Appearance', { exact: true }).selectOption('light')
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  await page.reload()
  await expect(page.getByLabel('Appearance', { exact: true })).toHaveValue('light')
  await page.getByLabel('Appearance', { exact: true }).selectOption('system')
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  expect(await page.evaluate(() => ({ ...localStorage }))).toEqual({ 'xkeen-ui-theme-v1': 'system' })
})

for (const theme of ['light', 'dark']) test(`all workspaces use one readable ${theme} theme and fit mobile`, async ({ page }) => {
  test.setTimeout(45_000)
  const model = await fixture(page)
  await page.getByLabel('Appearance', { exact: true }).selectOption(theme)
  const output = path.resolve('..', 'dist', 'ui-audit')
  await mkdir(output, { recursive: true })
  for (const [index, name] of ['Overview', 'Nodes 1', 'Routing', 'DNS', 'Performance', 'Components / Updates', 'Backup & Restore', 'System / Panel'].entries()) {
    await (await revealNavigation(page)).getByRole('button', { name, exact: true }).click()
    if (name === 'Routing' || name === 'DNS') await expect(page.getByLabel('Configuration file', { exact: true })).toHaveCount(0)
    await expect(page.locator('.workspace')).toBeVisible()
    await expect(page.locator('.legacy-workspace')).toHaveCount(0)
    // Regression for old inherited white labels on white cards: all visible
    // form labels must use the current foreground, including nested workspaces.
    const labels = await page.locator('[data-slot=field-label]').evaluateAll((elements) => elements.filter((el) => el.getBoundingClientRect().height > 0).map((el) => ({ color: getComputedStyle(el).color, inherited: getComputedStyle(el.closest('[data-slot=card]') || el.parentElement).color })))
    for (const label of labels) expect(label.color).toBe(label.inherited)
    await page.screenshot({ path: path.join(output, `${theme}-${index + 1}-${name.split(' ')[0].toLowerCase()}.png`), fullPage: true })
  }
  for (const width of [320, 375, 768]) {
    await page.setViewportSize({ width, height: 900 })
    for (const name of ['Overview', 'Nodes 1', 'Routing', 'DNS', 'Backup & Restore', 'System / Panel']) {
      await (await revealNavigation(page)).getByRole('button', { name, exact: true }).click()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    }
  }
  expect(model.requests.filter((request) => request.method !== 'GET' && !['/api/v1/xkeen/jobs/read', '/api/v1/xkeen/config/document'].includes(request.path))).toEqual([])
})
