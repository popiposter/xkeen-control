import { openSection } from './fixtures/disclosures.js'
import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

test('legacy DNS is read-only and cannot regenerate split configuration', async ({ page }) => {
 await mountFeatureCompleteDashboard(page)
 let state = { state: 'pending', running: true, entries: 85000 }
 let writes = 0
 await page.route('**/api/v1/dns/split', route => route.fulfill({json: state}))
 await page.route('**/api/v1/dns/split/sync', route => { writes++; return route.fulfill({status:410,json:{error:'retired'}}) })
 await page.goto('/')
 await openSection(page, 'DNS')
 await expect(page.getByText('Waiting for Apply',{exact:true})).toBeVisible()
 await expect(page.getByText(/legacy LAN resolver is still configured/)).toBeVisible()
 await expect(page.getByRole('button',{name:'Check and synchronize'})).toHaveCount(0)
 await expect(page.getByRole('button',{name:'Prepare DNS from routing'})).toHaveCount(0)
 state={state:'unconfigured'}
 await page.getByRole('button',{name:'Refresh DNS status'}).click()
 await expect(page.getByText('Waiting for Apply',{exact:true})).toHaveCount(0)
 await expect(page.getByText(/legacy LAN resolver is still configured/)).toHaveCount(0)
 expect(writes).toBe(0)
})

test('an unreadable DNS status stays visible instead of hiding a possible legacy resolver', async ({ page }) => {
 await mountFeatureCompleteDashboard(page)
 let fail = true
 await page.route('**/api/v1/dns/split', route => fail ? route.fulfill({status:500,json:{error:'unavailable'}}) : route.fulfill({json:{state:'unconfigured'}}))
 await page.goto('/')
 await openSection(page, 'DNS')
 await expect(page.getByRole('status').filter({hasText:'DNS status is unavailable.'})).toBeVisible()
 fail = false
 await page.getByRole('button',{name:'Refresh DNS status'}).click()
 await expect(page.getByText('DNS status is unavailable.')).toHaveCount(0)
})

test('refreshing a legacy resolver status keeps its warning visible until the next answer', async ({ page }) => {
 await mountFeatureCompleteDashboard(page)
 let release
 let hold = false
 await page.route('**/api/v1/dns/split', async route => {
   if (hold) await new Promise(resolve => { release = resolve })
   return route.fulfill({json:{state:'pending', running:true}})
 })
 await page.goto('/')
 await openSection(page, 'DNS')
 await expect(page.getByText(/legacy LAN resolver is still configured/)).toBeVisible()
 hold = true
 await page.getByRole('button',{name:'Refresh DNS status'}).click()
 await expect.poll(() => typeof release).toBe('function')
 await expect(page.getByText(/legacy LAN resolver is still configured/)).toBeVisible()
 release()
 await expect(page.getByText('Waiting for Apply',{exact:true})).toBeVisible()
})
