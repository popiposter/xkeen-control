// Exercise native summaries rather than mutating open attributes in tests.
// Core regression suites reveal optional context before inspecting its contract.
export async function revealDetails(page, title) {
  const summary = page.locator('details > summary').filter({ hasText: title }).first()
  await summary.waitFor({ state: 'visible' })
  if (await summary.locator('..').getAttribute('open') === null) await summary.click()
}

export async function revealSystemSettings(page, section = 'Access') {
  await page.getByRole('tab', { name: section, exact: true }).click()
}

export async function revealNavigation(page) {
  await page.locator('.workspace').waitFor({ state: 'visible' })
  const navigation = page.getByRole('navigation', { name: 'Dashboard sections' })
  if (!await navigation.isVisible()) await page.getByRole('button', { name: 'Toggle navigation' }).click()
  return navigation
}

// Former navigation items now live inside Configuration, XKeen and System.
const sectionRoutes = {
  Routing: ['Configuration', 'Routing'], DNS: ['Configuration', 'DNS'], Configurations: ['Configuration', 'All files'],
  'Backup & Restore': ['System', 'Backup'], 'Components / Updates': ['XKeen'], 'System / Panel': ['System'],
}
export async function openSection(page, name) {
  const [item, tab] = sectionRoutes[name] || [name]
  const target = tab && page.getByRole('tab', { name: tab, exact: true })
  // Already inside the section: switch its tab only, like an operator would.
  if (target && await target.isVisible()) return target.click()
  const navigation = await revealNavigation(page)
  const escaped = item.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  await navigation.getByRole('button', { name: sectionRoutes[name] ? item : new RegExp('^' + escaped), exact: Boolean(sectionRoutes[name]) }).first().click()
  if (target) await target.click()
}
