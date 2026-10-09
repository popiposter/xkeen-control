import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

test('legacy DNS is read-only and cannot regenerate split configuration', async ({ page }) => {
 await mountFeatureCompleteDashboard(page)
 let state = { state: 'pending', running: true, entries: 85000 }
 let writes = 0
 await page.route('**/api/v1/dns/split', route => route.fulfill({json: state}))
 await page.route('**/api/v1/dns/split/sync', route => { writes++; return route.fulfill({status:410,json:{error:'retired'}}) })
 await page.goto('/')
 await page.getByRole('button',{name:'DNS',exact:true}).click()
 await expect(page.getByText('Waiting for Apply',{exact:true})).toBeVisible()
 await expect(page.getByText(/legacy LAN resolver is still configured/)).toBeVisible()
 await expect(page.getByRole('button',{name:'Check and synchronize'})).toHaveCount(0)
 await expect(page.getByRole('button',{name:'Prepare DNS from routing'})).toHaveCount(0)
 state={state:'unconfigured'}
 await page.getByRole('button',{name:'Refresh DNS status'}).click()
 await expect(page.getByText('No separate resolver',{exact:true})).toBeVisible()
 await expect(page.getByText(/legacy LAN resolver is still configured/)).toHaveCount(0)
 expect(writes).toBe(0)
})
