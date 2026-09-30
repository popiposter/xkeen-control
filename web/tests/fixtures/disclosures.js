// Exercise native summaries rather than mutating open attributes in tests.
// Core regression suites reveal optional context before inspecting its contract.
export async function revealDetails(page, title) {
  const summary = page.locator('details > summary').filter({ hasText: title }).first()
  await summary.waitFor({ state: 'visible' })
  if (await summary.locator('..').getAttribute('open') === null) await summary.click()
}

export async function revealSystemSettings(page) {
  for (const title of ['Password', 'Panel releases', 'Notifications', 'Runtime facts']) await revealDetails(page, title)
}

export async function revealNavigation(page) {
  await page.locator('.workspace').waitFor({ state: 'visible' })
  const navigation = page.getByRole('navigation', { name: 'Dashboard sections' })
  if (!await navigation.isVisible()) await page.getByRole('button', { name: 'Toggle navigation' }).click()
  return navigation
}
