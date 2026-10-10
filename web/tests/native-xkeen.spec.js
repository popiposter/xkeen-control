import { expect, test } from '@playwright/test'

for (const width of [375]) {
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
          lifecycle: { applying: false, maintenance: false },
          setup: { state: 'blocked', reasonCode: 'interception-conflict' },
          native: { installation: 'available', version: '2.0.1', channel: 'beta', core: 'xray', panelIntegration: 'available', xrayRunning: false, geodataFiles: 6, geodataCron: 'available' },
        },
        '/api/v1/nodes': { total: 0, nodes: [], subscriptions: [] },
        '/api/v1/performance': { nodes: [] },
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
    expect(requests.filter(({ path, method }) => method !== 'GET' && path !== '/api/v1/xkeen/jobs/read')).toEqual([])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })
}

// The fact-to-guidance matrix is covered in web/unit/native-xkeen-state.test.js;
// these two cases keep the browser wiring for absent native facts and a running state.
for (const [installation, panelIntegration, running] of [[undefined, undefined], ['available', 'unknown', true]]) {
  test(`native installation and runtime facts stay distinct: ${installation}/${panelIntegration}/${!!running}`, async ({ page }) => {
    await page.route('**/api/v1/**', async (route) => {
      const path = new URL(route.request().url()).pathname
      const data = {
        '/api/v1/session': { csrfToken: 'synthetic-csrf' },
        '/api/v1/status': { controlPlane: {}, xray: {}, xkeen: {}, balancer: {}, observatory: {}, lifecycle: {}, native: installation === undefined ? undefined : { installation, panelIntegration, xrayRunning: running } },
        '/api/v1/nodes': { nodes: [], subscriptions: [] },
        '/api/v1/performance': { nodes: [] },
      }[path]
      await route.fulfill({ status: data ? 200 : 404, contentType: 'application/json', body: JSON.stringify(data || {}) })
    })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: running ? 'XKeen is running' : 'Check XKeen status' })).toBeVisible()
    await expect(page.getByText('Use the official XKeen installer, then refresh this page.')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Manage VPN profiles', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
    await expect(page.getByRole('heading', { name: running ? 'XKeen is running' : 'Check XKeen status' })).toBeVisible()
    if (running) await expect(page.getByText('The native installation state is unavailable. Inspect XKeen and refresh status.')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Preview update', exact: true })).toHaveCount(0)
  })
}
