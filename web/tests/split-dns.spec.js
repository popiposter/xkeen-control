import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

test('LAN DNS separates active service from pending internal config and inspects sync result', async ({ page }) => {
 await mountFeatureCompleteDashboard(page)
 let state = { state: 'pending', running: true, entries: 85000, conditionalRules: 2 }
 let syncs = 0
 await page.route('**/api/v1/dns/split', route => route.fulfill({json: state}))
 await page.route('**/api/v1/dns/split/sync', route => {
  expect(route.request().headers()['x-csrf-token']).toBeTruthy()
  expect(route.request().postDataJSON()).toEqual({})
  syncs++
  return route.fulfill({json: {state:'synced',running:true,entries:85000,lastSync:'2026-10-06T00:00:00Z'}})
 })
 await page.goto('/')
 await page.getByRole('button',{name:'DNS',exact:true}).click()
 await expect(page.getByText('Waiting for Apply',{exact:true})).toBeVisible()
 await expect(page.getByRole('button',{name:'Check and synchronize'})).toBeDisabled()
 state={...state,state:'failed',message:'Inspect interrupted DNS activation.'}
 await page.getByRole('button',{name:'Overview',exact:true}).click()
 await page.getByRole('button',{name:'DNS',exact:true}).click()
 await expect(page.getByText('Needs attention',{exact:true})).toBeVisible()
 await page.getByRole('button',{name:'Check and synchronize'}).click()
 await expect(page.getByText('Synchronized',{exact:true})).toBeVisible()
 expect(syncs).toBe(1)
})
