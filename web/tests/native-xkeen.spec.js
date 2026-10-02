import { expect, test } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`native installation replaces Setup and component updater at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const requests = []
    await page.route('**/api/v1/**', async (route) => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      requests.push({ path, method: request.method() })
      const data = {
        '/api/v1/session': { csrfToken: 'synthetic-native-csrf' },
        '/api/v1/status': {
          controlPlane: { version: 'test' }, xray: {}, xkeen: {}, balancer: {}, observatory: {},
          benchmark: { controlPlane: {} }, selection: {}, lifecycle: { applying: false, maintenance: false },
          setup: { state: 'blocked', reasonCode: 'interception-conflict' },
          native: { installation: 'available', version: '2.0.1', channel: 'beta', core: 'xray', panelIntegration: 'available', xrayRunning: false, geodataFiles: 6, geodataCron: 'available' },
        },
        '/api/v1/nodes': { total: 0, nodes: [], subscriptions: [] },
        '/api/v1/performance': { nodes: [] },
        '/api/v1/config-summary': { routing: {}, dns: {}, observatory: {} },
      }[path]
      await route.fulfill({ status: data ? 200 : 404, contentType: 'application/json', body: JSON.stringify(data || { error: 'unexpected route' }) })
    })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Add your VPN profiles' })).toBeVisible()
    await expect(page.getByText('Setup is unavailable', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Prepare setup' })).toHaveCount(0)
    if (width < 800) await page.getByRole('button', { name: 'Toggle navigation' }).click()
    await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'XKeen and components' })).toBeVisible()
    await expect(page.getByText('Configured in XKeen', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Open panel settings' })).toBeVisible()
    expect(requests.filter(({ path }) => path.startsWith('/api/v1/components') || path.startsWith('/api/v1/setup'))).toEqual([])
    expect(requests.filter(({ method }) => method !== 'GET')).toEqual([])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })
}

for (const [installation, panelIntegration] of [[undefined, undefined], ['unknown', 'missing'], ['unsupported', 'missing'], ['available', 'unknown'], ['available', 'unsupported']]) {
  test(`uncertain native state is inspection only: ${installation}/${panelIntegration}`, async ({ page }) => {
    await page.route('**/api/v1/**', async (route) => {
      const path = new URL(route.request().url()).pathname
      const data = {
        '/api/v1/session': { csrfToken: 'synthetic-csrf' },
        '/api/v1/status': { controlPlane: {}, xray: {}, xkeen: {}, balancer: {}, observatory: {}, benchmark: { controlPlane: {} }, selection: {}, lifecycle: {}, native: installation === undefined ? undefined : { installation, panelIntegration } },
        '/api/v1/nodes': { nodes: [], subscriptions: [] },
        '/api/v1/performance': { nodes: [] },
        '/api/v1/config-summary': {},
      }[path]
      await route.fulfill({ status: data ? 200 : 404, contentType: 'application/json', body: JSON.stringify(data || {}) })
    })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'Check XKeen status' })).toBeVisible()
    await expect(page.getByText('Use the official XKeen installer, then refresh this page.')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Manage VPN profiles', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Check XKeen status' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Preview update', exact: true })).toHaveCount(0)
  })
}
