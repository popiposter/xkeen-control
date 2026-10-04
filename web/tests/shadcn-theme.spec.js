import { expect, test } from '@playwright/test'

for (const width of [375, 1440]) {
  test(`standard shadcn login at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.route('**/api/v1/session', (route) => route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":"unauthorized"}' }))
    await page.goto('/')
    await expect(page.getByRole('heading', { name: 'XKeen Control' })).toBeVisible()
    await expect(page.locator('[data-slot="card"]')).toBeVisible()
    await expect(page.getByLabel('Appearance', { exact: true })).toHaveCount(0)
    await expect(page.getByLabel('Panel password', { exact: true })).toBeFocused()
    await page.keyboard.press('Tab')
    await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeFocused()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    if (process.env.XKEEN_UI_SCREENSHOT_DIR) await page.screenshot({ path: `${process.env.XKEEN_UI_SCREENSHOT_DIR}/login-${width}.png`, fullPage: true })
  })
}
