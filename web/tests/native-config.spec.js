import { openSection } from './fixtures/disclosures.js'
async function selectConfig(page, file) {
  await openSection(page, 'Configurations')
  // Opening Configuration first shows Routing; read back fixture edits made since.
  const reload = page.getByRole('button', { name: 'Reload current configuration', exact: true })
  if (await reload.isEnabled()) await reload.click()
  await page.getByLabel('Configuration file', { exact: true }).selectOption(file)
}
import { expect, test } from '@playwright/test'
import { mountFeatureCompleteDashboard } from './fixtures/feature-complete-model.js'

test('global service search selects host and IP categories as separate alternative rules', async ({ page }) => {
  const { documents, writes } = await mountEditor(page)
  documents['05_routing.json'].text = '{"routing":{"rules":[],"balancers":[]}}'
  await page.route('**/api/v1/geodata', (route) => route.fulfill({ json: [{ name: 'geosite_vendor.dat', kind: 'geosite', available: true, size: 100 },{ name: 'geoip_vendor.dat', kind: 'geoip', available: true, size: 100 }] }))
  await page.route('**/api/v1/geodata/query', (route) => { const body=route.request().postDataJSON(); expect(body.view).toBe('search'); expect(body.search).toBe('microsoft'); return route.fulfill({ json: { file: body.file, kind: body.file.startsWith('geosite') ? 'geosite' : 'geoip', snapshot: 'c'.repeat(64), items: [{category:'microsoft',type:'category',value:'microsoft',count:2}], total:1,offset:0,more:false } }) })
  await selectConfig(page,'05_routing.json')
  await page.getByRole('button',{name:'Browse installed geodata',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'Search installed geodata'})
  await dialog.getByLabel('Find a service across all databases').fill('microsoft')
  await dialog.getByRole('button',{name:'Search all databases',exact:true}).click()
  await dialog.getByRole('checkbox',{name:'Select geosite_vendor.dat microsoft',exact:true}).check()
  await dialog.getByRole('checkbox',{name:'Select geoip_vendor.dat microsoft',exact:true}).check()
  await dialog.getByLabel('Rule destination').selectOption('outbound:vpn')
  await expect(dialog.getByLabel('Rule preview')).toContainText('From LAN devices, Local SOCKS')
  await dialog.getByRole('button',{name:'Add 2 rules',exact:true}).click()
  await expect(dialog.getByRole('status')).toContainText('Added as rules 1–2')
  await dialog.getByRole('button',{name:'Done browsing',exact:true}).click()
  await page.getByRole('button',{name:'Text',exact:true}).click()
  const raw=page.getByRole('textbox',{name:'Configuration text'})
  await expect(raw).toContainText('ext:geosite_vendor.dat:microsoft')
  await expect(raw).toContainText('ext:geoip_vendor.dat:microsoft')
  const rules=JSON.parse(await raw.innerText()).routing.rules
  expect(rules).toHaveLength(2);expect(rules.every((rule)=>!(rule.domain && rule.ip))).toBe(true)
  expect(rules.every((rule)=>JSON.stringify(rule.inboundTag)==='["redirect","tproxy","socks"]')).toBe(true)
  expect(writes).toEqual([])
})

async function mountEditor(page) {
  const model = await mountFeatureCompleteDashboard(page)
  const writes = []
  let reads = 0
  const state = { digest: 'a'.repeat(64), pending: null, hasPrevious: false, targets: [{ tag: 'direct', kind: 'outbound', protocol: 'freedom' }, { tag: 'vpn', kind: 'outbound', protocol: 'vless' }] }
  const original = '{/* keep DNS */"dns":{"queryStrategy":"UseIP","future":9007199254740993}}'
  const documents = { '02_dns.json': { text: original }, '05_routing.json': { text: '{"routing":{"domainStrategy":"AsIs","rules":[]}}' } }
  await page.route('**/api/v1/xkeen/config/workspace', (route) => {
    reads++
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ ...state, documents: Object.fromEntries(Object.keys(documents).map((id) => [id, {}])) }) })
  })
  await page.route('**/api/v1/xkeen/config/document', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: state.digest, document: documents[route.request().postDataJSON().file] }) }))
  await page.route('**/api/v1/xkeen/config/text', (route) => {
    writes.push(route.request().postDataJSON())
    state.digest = 'b'.repeat(64)
    const body = route.request().postDataJSON()
    documents[body.file].text = body.text
    state.pending = { files: [body.file], restartRequired: true }
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: state.digest, saved: true, restartRequired: true }) })
  })
  await page.goto('/')
  await openSection(page, 'Components / Updates')
  expect(reads).toBe(0)
  await openSection(page, 'DNS')
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIP')
  return { model, writes, original, state, documents }
}

test('slow validation write survives old 60s timer and retains busy ownership', async ({ page }) => {
  const { state } = await mountEditor(page)
  await page.clock.install()
  let pendingRoute
  let writes = 0
  await page.route('**/api/v1/xkeen/config/text', (route) => { writes++; pendingRoute = route })
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv4')
  await page.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect.poll(() => writes).toBe(1)
  await page.clock.fastForward(70_000)
  await expect(page.getByRole('button', { name: 'Save configuration', exact: true })).toBeDisabled()
  await expect(page.getByText('Saved and validated. Apply when ready.')).not.toBeVisible()
  await pendingRoute.fulfill({ json: { digest: state.digest, saved: true, restartRequired: true } })
  await expect(page.getByRole('status').filter({ hasText: 'Saved and validated' })).toBeVisible()
  expect(writes).toBe(1)
})

test('Form/Text share edits, formatting and undo without saving or restarting', async ({ page }) => {
  const { model, writes } = await mountEditor(page)
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv4')
  const done = page.getByRole('dialog').getByRole('button', { name: 'Done', exact: true }); if (await done.isVisible()) await done.click()
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'Configuration text' })
  await expect(editor).toContainText('UseIPv4')
  await expect(editor).toContainText('9007199254740993')
  await page.getByRole('button', { name: 'Format JSON', exact: true }).click()
  await expect(editor).toContainText('keep DNS')
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv4')
  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await openSection(page, 'DNS')
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv4')
  await page.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved and validated' })).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0].text).toContain('keep DNS')
  expect(writes[0].text).toContain('9007199254740993')
  expect(model.requests.filter((request) => request.path === '/api/v1/xkeen/jobs/start')).toEqual([])
  expect(model.issues).toEqual([])
})

test('quality Apply is blocked by unfinished native editor work and does not erase it', async ({ page }) => {
  const {model,writes}=await mountEditor(page)
  model.quality={state:'completed',digest:'a'.repeat(64),canStage:true,progress:{state:'completed',candidates:[]}}
  await page.getByLabel('DNS address family',{exact:true}).selectOption('UseIPv6')
  await page.getByRole('button',{name:'Performance',exact:true}).click()
  await expect(page.getByRole('button',{name:'Apply recommendation',exact:true})).toBeDisabled()
  await expect(page.getByText('Save or discard unfinished configuration edits before applying the recommendation.',{exact:true})).toBeVisible()
  await openSection(page, 'DNS')
  await expect(page.getByLabel('DNS address family',{exact:true})).toHaveValue('UseIPv6')
  expect(writes).toEqual([])
  expect(model.requests.filter(({path})=>path==='/api/v1/performance/quality/apply')).toEqual([])
})

test('DNS and Routing navigation use one native workspace and retain edits across pages and console navigation', async ({ page }) => {
  const { model, writes } = await mountEditor(page)
  await openSection(page, 'DNS')
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv6')
  await openSection(page, 'Routing')
  await expect(page.getByLabel('Configuration file', { exact: true })).toHaveCount(0)
  await page.getByLabel('Routing domain resolution', { exact: true }).selectOption('IPIfNonMatch')
  await openSection(page, 'Components / Updates')
  await openSection(page, 'DNS')
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv6')
  await openSection(page, 'Routing')
  await expect(page.getByLabel('Routing domain resolution', { exact: true })).toHaveValue('IPIfNonMatch')
  expect(writes).toEqual([])
  expect(model.requests.filter(({ path }) => /^\/api\/v1\/appliance\/(policy|dns-observatory)/.test(path))).toEqual([])
})

test('two configs save as one set, Apply is one native job, and previous restore awaits another explicit Apply', async ({ page }) => {
  const { state, documents, original } = await mountEditor(page)
  const originalRouting = documents['05_routing.json'].text
  const saves = [], applies = []
  let job = null
  await page.route('**/api/v1/xkeen/config/save-set', (route) => {
    const body = route.request().postDataJSON(); saves.push(body)
    for (const [id, text] of Object.entries(body.documents)) documents[id].text = text
    state.digest = 'b'.repeat(64); state.pending = { files: Object.keys(body.documents), restartRequired: true }
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: state.digest, saved: true }) })
  })
  await page.route('**/api/v1/xkeen/config/apply', (route) => {
    applies.push(route.request().postDataJSON())
    job = { id: '1'.repeat(32), action: 'restart', state: 'running', interactive: false, output: '', cursor: 0, truncated: false }
    state.pending.applyId = job.id; state.pending.applyState = 'running'
    return route.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify(job) })
  })
  await page.route('**/api/v1/xkeen/jobs/read', (route) => {
    if (!job) return route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"no job"}' })
    job = { ...job, state: 'completed', exitCode: 0, configurationState: 'applied' }
    state.pending = null; state.hasPrevious = true
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify(job) })
  })
  await page.route('**/api/v1/xkeen/config/restore-previous', (route) => {
    documents['02_dns.json'].text = original; documents['05_routing.json'].text = originalRouting
    state.digest = 'c'.repeat(64); state.pending = { files: ['02_dns.json', '05_routing.json'], restartRequired: true }
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: state.digest, saved: true }) })
  })
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv4')
  await selectConfig(page, '05_routing.json')
  await expect(page.getByLabel('Routing domain resolution', { exact: true })).toHaveValue('AsIs')
  await page.getByLabel('Routing domain resolution', { exact: true }).selectOption('IPOnDemand')
  await page.getByRole('button', { name: 'Save all configurations', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'All working changes saved' })).toBeVisible()
  expect(saves).toHaveLength(1); expect(Object.keys(saves[0].documents)).toHaveLength(2)
  await page.getByRole('button', { name: 'Apply saved configurations', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'new running Xray process' })).toBeVisible()
  expect(applies).toEqual([{ digest: 'b'.repeat(64) }])
  await page.getByRole('button', { name: 'Restore previous configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Previous configuration saved' })).toBeVisible()
  await expect(page.getByLabel('Routing domain resolution', { exact: true })).toHaveValue('AsIs')
  await expect(page.getByRole('button', { name: 'Apply saved configurations', exact: true })).toBeEnabled()
  expect(applies).toHaveLength(1)
})

test('discarding saved files preserves a different unfinished working document', async ({ page }) => {
  const { state, documents, original } = await mountEditor(page)
  await page.route('**/api/v1/xkeen/config/restore-saved', (route) => {
    documents['02_dns.json'].text = original; state.pending = null; state.digest = 'c'.repeat(64)
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ digest: state.digest, saved: true }) })
  })
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv4')
  await page.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved and validated' })).toBeVisible()
  await selectConfig(page, '05_routing.json')
  await page.getByLabel('Routing domain resolution', { exact: true }).selectOption('IPOnDemand')
  await page.getByRole('button', { name: 'Discard saved changes', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved changes discarded' })).toBeVisible()
  await expect(page.getByLabel('Routing domain resolution', { exact: true })).toHaveValue('IPOnDemand')
  await expect(page.getByRole('button', { name: 'Save configuration', exact: true })).toBeEnabled()
})

for (const failure of ['lost', 'malformed']) test(`${failure} Apply acceptance prevents replay and leaves explicit inspection available`, async ({ page }) => {
  const { state } = await mountEditor(page)
  let calls = 0
  await page.route('**/api/v1/xkeen/config/apply', (route) => { calls++; state.pending.applyState = 'unknown'; return failure === 'lost' ? route.abort() : route.fulfill({ status: 202, contentType: 'application/json', body: '{"state":"completed"}' }) })
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv4')
  await page.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved and validated' })).toBeVisible()
  await page.getByRole('button', { name: 'Apply saved configurations', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Apply saved configurations', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Inspect existing Apply', exact: true })).toBeVisible()
  expect(calls).toBe(1)
})

test('graphical routing lists retain typed newlines and opaque fields through Text and undo', async ({ page }) => {
  const { documents, writes } = await mountEditor(page)
  documents['05_routing.json'].text = '{"routing":{"rules":[{"type":"field","domain":["example.test"],"outboundTag":"direct","opaque":9007199254740993}]}}'
  await selectConfig(page, '05_routing.json')
  await page.getByRole('button', { name: 'Edit rule 1', exact: true }).click()
  const domains = page.getByLabel('Rule 1 domains / geosite (one per line)', { exact: true })
  await domains.fill('example.test\n')
  await expect(domains).toHaveValue('example.test\n')
  await domains.pressSequentially('geosite:synthetic')
  await expect(domains).toHaveValue('example.test\ngeosite:synthetic')
  const done = page.getByRole('dialog').getByRole('button', { name: 'Done', exact: true }); if (await done.isVisible()) await done.click()
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'Configuration text' })
  await expect(editor).toContainText('9007199254740993')
  await expect(editor).toContainText('geosite:synthetic')
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await page.getByRole('button', { name: 'Add traffic rule', exact: true }).click()
  await expect(page.getByLabel('Traffic destination', { exact: true })).toHaveValue('outbound:direct')
  await page.getByRole('dialog').getByRole('button', { name: 'Done', exact: true }).click()
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Edit rule 2', exact: true })).toHaveCount(0)
  expect(writes).toEqual([])
})

test('invalid Text remains a savable private draft and cannot replace native files', async ({ page }) => {
  const { writes } = await mountEditor(page)
  const drafts = []
  await page.route('**/api/v1/xkeen/config/draft', (route) => {
    drafts.push(route.request().postDataJSON())
    return route.fulfill({ contentType: 'application/json', body: '{"saved":true}' })
  })
  const done = page.getByRole('dialog').getByRole('button', { name: 'Done', exact: true }); if (await done.isVisible()) await done.click()
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'Configuration text' })
  await editor.fill('{ invalid')
  await expect(page.getByRole('button', { name: 'Save configuration', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('line')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(editor).toContainText('{ invalid')
  await page.getByRole('button', { name: 'Save draft', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Draft saved' })).toBeVisible()
  expect(drafts).toEqual([{ file: '02_dns.json', text: '{ invalid' }])
  expect(writes).toEqual([])
})

test('installed geodata adds an ordered category rule only to the shared draft', async ({ page }) => {
  const { model, writes, state, documents } = await mountEditor(page)
  state.targets = [{ tag: 'direct', kind: 'outbound', protocol: 'freedom' }, { tag: 'vpn', kind: 'outbound', protocol: 'vless' }]
  documents['05_routing.json'].text = '{"routing":{"domainStrategy":"AsIs","rules":[/* keep catch-all */{"type":"field","future":9007199254740993,"outboundTag":"direct"}]}}'
  let inventoryReads = 0
  const queries = []
  await page.route('**/api/v1/geodata', (route) => {
    inventoryReads++
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ name: 'geosite_vendor.dat', kind: 'geosite', size: 1024, available: true }]) })
  })
  await page.route('**/api/v1/geodata/query', (route) => {
    const body = route.request().postDataJSON()
    queries.push(body)
    if (body.view === 'entries') return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ file: body.file, kind: 'geosite', snapshot: 'c'.repeat(64), offset: 0, total: 2, more: false, items: [{ category: 'video', type: 'domain', value: 'cdn.video.example' }, { category: 'video', type: 'full', value: 'video.example' }] }) })
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ file: body.file, kind: 'geosite', snapshot: 'c'.repeat(64), offset: 0, total: 1, more: false, items: [{ category: 'video', type: 'category', value: 'video', count: 2 }] }) })
  })
  await selectConfig(page, '05_routing.json')
  expect(inventoryReads).toBe(0)
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  await expect(page.getByLabel('Or list every category of one database', { exact: true })).toHaveValue('geosite_vendor.dat')
  await page.getByRole('button', { name: 'Show categories', exact: true }).click()
  await expect(page.getByText('2 domain entries', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Show contents of video', exact: true }).click()
  await expect(page.getByText('cdn.video.example', { exact: true })).toBeVisible()
  await page.getByRole('checkbox', { name: 'Select geosite_vendor.dat video', exact: true }).check()
  await page.getByLabel('Rule destination', { exact: true }).selectOption('outbound:vpn')
  await page.getByLabel('Rule position', { exact: true }).selectOption('first')
  await page.getByRole('button', { name: 'Add rule', exact: true }).click()
  await page.getByRole('button', { name: 'Done browsing', exact: true }).click()
  await page.getByRole('button', { name: 'Edit rule 1', exact: true }).click()
  await expect(page.getByLabel('Rule 1 domains / geosite (one per line)', { exact: true })).toHaveValue('ext:geosite_vendor.dat:video')
  const done = page.getByRole('dialog').getByRole('button', { name: 'Done', exact: true }); if (await done.isVisible()) await done.click()
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const editor = page.getByRole('textbox', { name: 'Configuration text' })
  await expect(editor).toContainText('9007199254740993')
  await expect(editor).toContainText('keep catch-all')
  await page.getByRole('button', { name: 'Undo', exact: true }).click()
  await expect(editor).not.toContainText('geosite_vendor.dat')
  expect(queries.map((query) => query.view)).toEqual(['categories', 'entries'])
  expect(queries[1]).toMatchObject({ category: 'video', snapshot: 'c'.repeat(64) })
  expect(writes).toEqual([])
  expect(model.requests.filter((request) => request.path === '/api/v1/xkeen/jobs/start')).toEqual([])
})

test('a category rule added after existing rules covers LAN inbounds and lands before the final catch-all', async ({ page }) => {
  const { documents, writes } = await mountEditor(page)
  const lan = ['redirect', 'tproxy', 'socks']
  documents['05_routing.json'].text = JSON.stringify({ routing: { rules: [
    { type: 'field', inboundTag: ['api'], outboundTag: 'api' },
    { type: 'field', domain: ['domain:legacy.example'], outboundTag: 'vpn' },
    { type: 'field', inboundTag: lan, domain: ['ext:geosite_refilter.dat:REFILTER'], outboundTag: 'vpn' },
    { type: 'field', inboundTag: lan, network: [], outboundTag: 'direct' },
  ], balancers: [] } })
  await page.route('**/api/v1/geodata', (route) => route.fulfill({ json: [{ name: 'geosite_vendor.dat', kind: 'geosite', available: true, size: 100 }] }))
  await page.route('**/api/v1/geodata/query', (route) => { const body = route.request().postDataJSON(); return route.fulfill({ json: { file: body.file, kind: 'geosite', snapshot: 'c'.repeat(64), items: [{ category: 'microsoft', type: 'category', value: 'microsoft', count: 2 }], total: 1, offset: 0, more: false } }) })
  await selectConfig(page, '05_routing.json')
  // A rule without inbound tags is flagged: it would pause node reviews.
  await expect(page.locator('[data-rule="2"]').getByText('All inbounds · pauses node reviews')).toBeVisible()
  await expect(page.locator('[data-rule="3"]').getByText('All inbounds · pauses node reviews')).toHaveCount(0)
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Search installed geodata' })
  await dialog.getByLabel('Find a service across all databases').fill('microsoft')
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await dialog.getByRole('checkbox', { name: 'Select geosite_vendor.dat microsoft', exact: true }).check()
  await dialog.getByLabel('Rule destination').selectOption('outbound:vpn')
  await expect(dialog.getByLabel('Rule position')).toHaveValue('last')
  await expect(dialog.getByLabel('Rule preview')).toContainText('Rule 4')
  await expect(dialog.getByLabel('Rule preview')).toContainText('Placed before the final rule that sends all other traffic to direct')
  await dialog.getByRole('button', { name: 'Add rule', exact: true }).click()
  await expect(dialog.getByRole('status')).toContainText('Added as rule 4')
  await dialog.getByRole('button', { name: 'Done browsing', exact: true }).click()
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const rules = JSON.parse(await page.getByRole('textbox', { name: 'Configuration text' }).innerText()).routing.rules
  expect(rules).toHaveLength(5)
  expect(rules[3]).toEqual({ type: 'field', domain: ['ext:geosite_vendor.dat:microsoft'], outboundTag: 'vpn', inboundTag: lan })
  expect(rules[4]).toEqual({ type: 'field', inboundTag: lan, network: [], outboundTag: 'direct' })
  expect(writes).toEqual([])
})

test('category contents page contiguously, and a replaced database drops what was built from it', async ({ page }) => {
  const { writes } = await mountEditor(page)
  const queries = []
  let replaced = false
  await page.route('**/api/v1/geodata', (route) => route.fulfill({ json: [{ name: 'geosite_vendor.dat', kind: 'geosite', available: true, size: 100 }] }))
  await page.route('**/api/v1/geodata/query', (route) => {
    const body = route.request().postDataJSON(); queries.push(body)
    if (replaced) return route.fulfill({ status: 409, json: { error: 'installed geodata changed; reload the file' } })
    if (body.view === 'entries') {
      const all = Array.from({ length: 120 }, (_, index) => ({ category: 'video', type: 'domain', value: `host-${index}.example` }))
      return route.fulfill({ json: { file: body.file, kind: 'geosite', snapshot: 'c'.repeat(64), offset: body.offset, total: 120, more: body.offset + 50 < 120, items: all.slice(body.offset, body.offset + 50) } })
    }
    return route.fulfill({ json: { file: body.file, kind: 'geosite', snapshot: 'c'.repeat(64), offset: 0, total: 2, more: false, items: [{ category: 'video', type: 'category', value: 'video', count: 120 }, { category: 'music', type: 'category', value: 'music', count: 3 }] } })
  })
  await selectConfig(page, '05_routing.json')
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Search installed geodata' })
  await dialog.getByRole('button', { name: 'Show categories', exact: true }).click()
  await dialog.getByRole('button', { name: 'Show contents of video', exact: true }).click()
  await dialog.getByRole('button', { name: 'Show more entries', exact: true }).click()
  await dialog.getByRole('button', { name: 'Show more entries', exact: true }).click()
  await expect(dialog.getByText('Showing 120 of 120', { exact: true })).toBeVisible()
  await expect(dialog.getByText('host-100.example', { exact: true })).toBeVisible()
  expect(queries.filter((query) => query.view === 'entries').map((query) => query.offset)).toEqual([0, 50, 100])
  await expect(dialog.getByRole('button', { name: 'Show more entries', exact: true })).toHaveCount(0)
  // The database is replaced after a category was selected.
  await dialog.getByRole('checkbox', { name: 'Select geosite_vendor.dat music', exact: true }).check()
  await expect(dialog.getByRole('button', { name: 'Add rule', exact: true })).toBeEnabled()
  replaced = true
  await dialog.getByRole('button', { name: 'Show contents of music', exact: true }).click()
  await expect(dialog.getByRole('status')).toContainText('geosite_vendor.dat changed or could not be read')
  await expect(dialog.getByRole('checkbox', { name: 'Select geosite_vendor.dat music', exact: true })).toHaveCount(0)
  await expect(dialog.getByText('2. Selected categories (0)', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Add rule', exact: true })).toBeDisabled()
  expect(writes).toEqual([])
})

test('an address finds categories by native membership, and a rescanned database drops a stale selection', async ({ page }) => {
  const { documents, writes } = await mountEditor(page)
  documents['05_routing.json'].text = JSON.stringify({ routing: { rules: [
    { type: 'field', inboundTag: ['redirect', 'tproxy'], domain: ['ext:geosite_refilter.dat:REFILTER'], outboundTag: 'vpn' },
    { type: 'field', inboundTag: ['socks'], network: 'udp', outboundTag: 'direct' },
  ], balancers: [] } })
  let snapshot = 'c'.repeat(64)
  const queries = []
  await page.route('**/api/v1/geodata', (route) => route.fulfill({ json: [{ name: 'geoip_vendor.dat', kind: 'geoip', available: true, size: 100 }] }))
  await page.route('**/api/v1/geodata/query', (route) => {
    const body = route.request().postDataJSON(); queries.push(body)
    if (body.view === 'match') return route.fulfill({ json: { file: body.file, kind: 'geoip', snapshot, offset: 0, total: 1, more: false, items: [{ category: 'office', type: 'cidr', value: '192.0.2.0/24' }] } })
    return route.fulfill({ json: { file: body.file, kind: 'geoip', snapshot, offset: 0, total: 0, more: false, items: [] } })
  })
  await selectConfig(page, '05_routing.json')
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Search installed geodata' })
  await dialog.getByLabel('Find a service across all databases').fill('192.0.2.5')
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await expect(dialog.getByText('IP ranges · matches via 192.0.2.0/24', { exact: true })).toBeVisible()
  expect(queries.map((query) => query.view)).toEqual(['search', 'match'])
  expect(queries[1]).toMatchObject({ search: '192.0.2.5', snapshot: 'c'.repeat(64) })
  await dialog.getByRole('checkbox', { name: 'Select geoip_vendor.dat office', exact: true }).check()
  await dialog.getByLabel('Rule destination').selectOption('outbound:vpn')
  // The final UDP-only rule is not a catch-all: the new rule goes after it,
  // and covers both the LAN and the SOCKS inbounds the draft uses.
  await expect(dialog.getByLabel('Rule preview')).toContainText('Rule 3')
  await expect(dialog.getByLabel('Rule preview')).not.toContainText('Placed before the final rule')
  // Rescanning a replaced database drops the selection made from the old one.
  snapshot = 'e'.repeat(64)
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await expect(dialog.getByText('2. Selected categories (0)', { exact: true })).toBeVisible()
  await dialog.getByRole('checkbox', { name: 'Select geoip_vendor.dat office', exact: true }).check()
  await dialog.getByRole('button', { name: 'Add rule', exact: true }).click()
  await dialog.getByRole('button', { name: 'Done browsing', exact: true }).click()
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  const rules = JSON.parse(await page.getByRole('textbox', { name: 'Configuration text' }).innerText()).routing.rules
  expect(rules[2]).toEqual({ type: 'field', inboundTag: ['redirect', 'tproxy', 'socks'], ip: ['ext:geoip_vendor.dat:office'], outboundTag: 'vpn' })
  expect(writes).toEqual([])
})

test('membership matches page past one large category, and an empty or failed rescan still drops a stale selection', async ({ page }) => {
  const { writes } = await mountEditor(page)
  let snapshot = 'c'.repeat(64), failing = false
  const queries = []
  await page.route('**/api/v1/geodata', (route) => route.fulfill({ json: [{ name: 'geoip_vendor.dat', kind: 'geoip', available: true, size: 100 }] }))
  await page.route('**/api/v1/geodata/query', (route) => {
    const body = route.request().postDataJSON(); queries.push(body)
    if (failing) return route.fulfill({ status: 409, json: { error: 'installed geodata changed; reload the file' } })
    if (body.view === 'match' && body.search === '192.0.2.5') {
      // Fifty matching entries of one category fill the first page.
      if (body.offset === 0) return route.fulfill({ json: { file: body.file, kind: 'geoip', snapshot, offset: 0, total: 51, more: true, items: Array.from({ length: 50 }, (_, index) => ({ category: 'big', type: 'cidr', value: `192.0.${index}.0/16` })) } })
      return route.fulfill({ json: { file: body.file, kind: 'geoip', snapshot, offset: 50, total: 51, more: false, items: [{ category: 'late', type: 'cidr', value: '192.0.2.0/24' }] } })
    }
    return route.fulfill({ json: { file: body.file, kind: 'geoip', snapshot, offset: body.offset, total: 0, more: false, items: [] } })
  })
  await selectConfig(page, '05_routing.json')
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Search installed geodata' })
  const find = dialog.getByLabel('Find a service across all databases')
  await find.fill('192.0.2.5')
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await expect(dialog.getByRole('checkbox', { name: 'Select geoip_vendor.dat big', exact: true })).toBeVisible()
  await expect(dialog.getByRole('checkbox', { name: 'Select geoip_vendor.dat late', exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'More categories in geoip_vendor.dat', exact: true }).click()
  await expect(dialog.getByText('IP ranges · matches via 192.0.2.0/24', { exact: true })).toBeVisible()
  expect(queries.filter((query) => query.view === 'match').map((query) => [query.offset, query.snapshot])).toEqual([[0, 'c'.repeat(64)], [50, 'c'.repeat(64)]])
  await expect(dialog.getByRole('button', { name: 'More categories in geoip_vendor.dat', exact: true })).toHaveCount(0)
  // The database is replaced; a rescan that finds nothing still drops the
  // selection made from the old snapshot.
  await dialog.getByRole('checkbox', { name: 'Select geoip_vendor.dat late', exact: true }).check()
  snapshot = 'e'.repeat(64)
  await find.fill('absent')
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await expect(dialog.getByRole('status')).toContainText('No category in the installed databases matches')
  await expect(dialog.getByText('2. Selected categories (0)', { exact: true })).toBeVisible()
  // A rescan that fails also drops it.
  await find.fill('192.0.2.5')
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await dialog.getByRole('checkbox', { name: 'Select geoip_vendor.dat big', exact: true }).check()
  failing = true
  await dialog.getByRole('button', { name: 'Search all databases', exact: true }).click()
  await expect(dialog.getByRole('status')).toContainText('Some databases could not be read: geoip_vendor.dat')
  await expect(dialog.getByText('2. Selected categories (0)', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Add rule', exact: true })).toBeDisabled()
  expect(writes).toEqual([])
})

test('native database replacement invalidates pagination without changing configuration', async ({ page }) => {
  const { writes, state } = await mountEditor(page)
  state.targets = [{ tag: 'direct', kind: 'outbound', protocol: 'freedom' }]
  const queries = []
  await page.route('**/api/v1/geodata', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ name: 'geoip.dat', kind: 'geoip', size: 1024, available: true }]) }))
  await page.route('**/api/v1/geodata/query', (route) => {
    const body = route.request().postDataJSON(); queries.push(body)
    if (body.offset) return route.fulfill({ status: 409, contentType: 'application/json', body: '{"error":"installed geodata changed; reload the file"}' })
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ file: body.file, kind: 'geoip', snapshot: 'd'.repeat(64), offset: 0, total: 100, more: true, items: Array.from({ length: 50 }, (_, index) => ({ category: 'region-'+index, type: 'category', value: 'region-'+index, count: 10 })) }) })
  })
  await selectConfig(page, '05_routing.json')
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  await expect(page.getByLabel('Or list every category of one database', { exact: true })).toHaveValue('geoip.dat')
  await page.getByRole('button', { name: 'Show categories', exact: true }).click()
  await page.getByRole('checkbox', { name: 'Select geoip.dat region-1', exact: true }).check()
  await page.getByRole('button', { name: 'More categories in geoip.dat', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'installed geodata changed' })).toBeVisible()
  expect(queries[1]).toMatchObject({ snapshot: 'd'.repeat(64), offset: 50 })
  // A changed database clears the list and the selection built from it.
  await expect(page.getByRole('checkbox', { name: 'Select geoip.dat region-1', exact: true })).toHaveCount(0)
  await expect(page.getByText('2. Selected categories (0)', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Done browsing', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Add traffic rule', exact: true })).toBeEnabled()
  expect(writes).toEqual([])
})
test('routing examples use the current draft and clear stale results without saving', async ({ page }) => {
  const { writes, model } = await mountEditor(page)
  await selectConfig(page, '05_routing.json')
  let finish
  const queries = []
  await page.route('**/api/v1/xkeen/config/example', (route) => {
    queries.push(route.request().postDataJSON())
    return new Promise((resolve) => { finish = () => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ state: 'matched', rule: 1, targetKind: 'outbound', target: 'vpn' }) }).then(resolve) })
  })
  await page.getByRole('button', { name: 'Check a routing example', exact: true }).click()
  await page.getByLabel('Domain name', { exact: true }).fill('example.test')
  await page.getByRole('button', { name: 'Check routing example', exact: true }).click()
  await expect.poll(() => queries.length).toBe(1)
  await page.getByLabel('Domain name', { exact: true }).fill('other.test')
  await finish()
  await expect(page.getByText('Rule 1: outbound vpn', { exact: true })).toHaveCount(0)
  await page.route('**/api/v1/xkeen/config/example', (route) => {
    queries.push(route.request().postDataJSON())
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ state: 'unknown', rule: 1, reason: 'Earlier rule needs protocol facts.' }) })
  })
  await page.getByRole('button', { name: 'Check routing example', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Earlier rule needs protocol facts.' })).toBeVisible()
  expect(queries[1].sample.domain).toBe('other.test')
  expect(queries[1].text).toContain('"routing"')
  expect(writes).toEqual([])
  expect(model.requests.filter((request) => request.path === '/api/v1/xkeen/jobs/start')).toEqual([])
})


test('purposeful native forms retain unknown fields, numeric types and exact probe spelling', async ({ page }) => {
  const { documents, writes } = await mountEditor(page)
  Object.assign(documents, {
    '01_log.json': { text: '{"log":{"loglevel":"warning","future":9007199254740993}}' },
    '03_inbounds.json': { text: '{"inbounds":[{"tag":"local","protocol":"socks","listen":"127.0.0.1","port":1080,"future":9007199254740993}]}' },
    '06_policy.json': { text: '{"policy":{"levels":{"0":{"connIdle":300,"future":9007199254740993}}}}' },
    '07_observatory.json': { text: '{"observatory":{"subjectSelector":["proxy-"],"probeUrl":"https://example.test/204","probeInterval":"30s"}}' },
    '08_api.json': { text: '{"api":{"tag":"api","services":["RoutingService"]},"inbounds":[]}' },
  })
  const reload = page.getByRole('button', { name: 'Reload current configuration', exact: true })
  if (await reload.isEnabled()) await reload.click()
  await selectConfig(page, '01_log.json')
  await page.getByLabel('Log detail', { exact: true }).selectOption('info')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).toContainText('9007199254740993')
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await selectConfig(page, '03_inbounds.json')
  await page.getByLabel('Listener 1 port', { exact: true }).fill('1081')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).toContainText('1081')
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).toContainText('9007199254740993')
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await selectConfig(page, '06_policy.json')
  await page.getByLabel('connIdle (seconds) - level 0', { exact: true }).fill('120')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).toContainText('9007199254740993')
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await selectConfig(page, '07_observatory.json')
  await page.getByLabel('Probe URL', { exact: true }).fill('https://example.test/check')
  await page.getByRole('button', { name: 'Text', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).toContainText('"probeUrl"')
  await expect(page.getByRole('textbox', { name: 'Configuration text' })).not.toContainText('"probeURL"')
  await page.getByRole('button', { name: 'Form', exact: true }).click()
  await selectConfig(page, '08_api.json')
  await expect(page.getByLabel('Enabled API services (one per line)', { exact: true })).toHaveValue('RoutingService')
  expect(writes).toEqual([])
})

test('routing summary names every rule condition and keeps catch-all wording for real catch-alls', async ({ page }) => {
  const { documents } = await mountEditor(page)
  documents['05_routing.json'].text = JSON.stringify({ routing: { rules: [
    { type: 'field', source: ['192.168.1.50'], outboundTag: 'direct' },
    { type: 'field', inboundTag: ['redirect'], localPort: '8080', outboundTag: 'direct' },
    { type: 'field', domain: ['keyword:example'], outboundTag: 'direct' },
    { type: 'field', network: 'tcp,udp', outboundTag: 'direct' },
  ] } })
  await selectConfig(page, '05_routing.json')
  const row = (n) => page.locator(`[data-rule="${n}"]`)
  await expect(row(1)).toContainText('Devices: 192.168.1.50')
  await expect(row(1)).not.toContainText('Everything else')
  await expect(row(2)).toContainText('Other conditions: localPort')
  await expect(row(2)).not.toContainText('all traffic')
  await expect(row(3)).toContainText('Sites: keyword:example')
  await expect(row(4)).toContainText('Everything else')
})
