import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

test('LAN DNS separates active service from pending internal config and inspects sync result', async ({ page }) => {
 await mountFeatureCompleteDashboard(page)
 let state = { state: 'pending', running: true, entries: 85000, conditionalRules: 2 }
 let syncs = 0
 let syncedReads = 0
 await page.route('**/api/v1/dns/split', route => {
  if (state.state === 'synced') syncedReads++
  return route.fulfill({json: state})
 })
 await page.route('**/api/v1/dns/split/sync', route => {
  expect(route.request().headers()['x-csrf-token']).toBeTruthy()
  expect(route.request().postDataJSON()).toEqual({})
  syncs++
  state = {state:'synced',running:true,entries:85000,conditionalRules:2,lastSync:'2026-10-06T00:00:00Z'}
  return route.fulfill({json: state})
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
 await expect.poll(() => syncedReads).toBeGreaterThan(0)
 await expect(page.getByText('Synchronized',{exact:true})).toBeVisible()
 await page.getByRole('button',{name:'Overview',exact:true}).click()
 await page.getByRole('button',{name:'DNS',exact:true}).click()
 await expect.poll(() => syncedReads).toBeGreaterThan(1)
 await expect(page.getByText('Synchronized',{exact:true})).toBeVisible()
 expect(syncs).toBe(1)
})
