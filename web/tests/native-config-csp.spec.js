import { openSection } from './fixtures/disclosures.js'
import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

test('production text editor keeps its layout and syntax styles under document CSP', async ({ page }) => {
  const model = await mountFeatureCompleteDashboard(page)
  const root = path.resolve('..', 'internal/webassets/dist')
  const nonce = 'synthetic-document-style-nonce'
  const policy = `default-src 'self'; script-src 'self'; style-src 'self' 'nonce-${nonce}'; connect-src 'self'; img-src 'self' data:`
  const html = (await readFile(path.join(root, 'index.html'), 'utf8')).replace('<head>', `<head><meta name="style-nonce" content="${nonce}">`)
  await page.route('**/assets/*', async (route) => {
    const filename = path.basename(new URL(route.request().url()).pathname)
    const types = { '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' }
    await route.fulfill({ body: await readFile(path.join(root, 'assets', filename)), contentType: types[path.extname(filename)] || 'application/octet-stream' })
  })
  await page.route('http://127.0.0.1:*/', (route) => route.fulfill({ body: html, contentType: 'text/html', headers: { 'Content-Security-Policy': policy } }))
  await page.goto('/')
  await openSection(page, 'DNS')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).toBeVisible()
  await expect(page.locator('.cm-scroller')).toHaveCSS('display', 'flex')
  await expect(page.locator('.cm-editor')).toHaveCSS('min-height', '288px')
  await expect(page.locator('.cm-gutters')).toHaveCSS('display', 'flex')
  expect(await page.locator('style').evaluateAll((styles, expected) => styles.some((style) => style.nonce === expected), nonce)).toBe(true)
  const colors = await page.locator('.cm-line span').evaluateAll((spans) => [...new Set(spans.map((span) => getComputedStyle(span).color))])
  expect(colors.length).toBeGreaterThan(1)
  expect(model.issues).toEqual([])
})
