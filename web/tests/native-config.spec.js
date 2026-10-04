async function selectConfig(page, file) {
  await page.getByRole('button', { name: 'Configurations', exact: true }).click()
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
  await dialog.getByRole('button',{name:'Add selected categories (2)',exact:true}).click()
  await dialog.getByRole('button',{name:'Done browsing',exact:true}).click()
  await page.getByRole('button',{name:'Text',exact:true}).click()
  const raw=page.getByRole('textbox',{name:'Configuration text'})
  await expect(raw).toContainText('ext:geosite_vendor.dat:microsoft')
  await expect(raw).toContainText('ext:geoip_vendor.dat:microsoft')
  const rules=JSON.parse(await raw.innerText()).routing.rules
  expect(rules).toHaveLength(2);expect(rules.every((rule)=>!(rule.domain && rule.ip))).toBe(true)
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
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  expect(reads).toBe(0)
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIP')
  return { model, writes, original, state, documents }
}

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
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv4')
  await page.getByRole('button', { name: 'Save configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Saved and validated' })).toBeVisible()
  expect(writes).toHaveLength(1)
  expect(writes[0].text).toContain('keep DNS')
  expect(writes[0].text).toContain('9007199254740993')
  expect(model.requests.filter((request) => request.path === '/api/v1/xkeen/jobs/start')).toEqual([])
  expect(model.issues).toEqual([])
})

test('DNS and Routing navigation use one native workspace and retain edits across pages and console navigation', async ({ page }) => {
  const { model, writes } = await mountEditor(page)
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await page.getByLabel('DNS address family', { exact: true }).selectOption('UseIPv6')
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
  await expect(page.getByLabel('Configuration file', { exact: true })).toHaveCount(0)
  await page.getByLabel('Routing domain resolution', { exact: true }).selectOption('IPIfNonMatch')
  await page.getByRole('button', { name: 'Components / Updates', exact: true }).click()
  await page.getByRole('button', { name: 'DNS', exact: true }).click()
  await expect(page.getByLabel('DNS address family', { exact: true })).toHaveValue('UseIPv6')
  await page.getByRole('button', { name: 'Routing', exact: true }).click()
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
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ file: body.file, kind: 'geosite', snapshot: 'c'.repeat(64), offset: 0, total: 1, more: false, items: [{ category: 'video', type: 'category', value: 'video', count: 2 }] }) })
  })
  await selectConfig(page, '05_routing.json')
  expect(inventoryReads).toBe(0)
  await page.getByRole('button', { name: 'Browse installed geodata', exact: true }).click()
  await expect(page.getByLabel('Database file', { exact: true })).toHaveValue('geosite_vendor.dat')
  await page.locator('summary').filter({ hasText: 'Browse one database' }).click()
  await page.getByRole('button', { name: 'Search installed database', exact: true }).click()
  await expect(page.getByText('video · category · 2 entries', { exact: true })).toBeVisible()
  await page.getByLabel('Rule destination', { exact: true }).selectOption('outbound:vpn')
  await page.getByRole('button', { name: 'Add category rule', exact: true }).click()
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
  expect(queries).toHaveLength(1)
  expect(writes).toEqual([])
  expect(model.requests.filter((request) => request.path === '/api/v1/xkeen/jobs/start')).toEqual([])
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
  await expect(page.getByLabel('Database file', { exact: true })).toHaveValue('geoip.dat')
  await page.locator('summary').filter({ hasText: 'Browse one database' }).click()
  await page.getByRole('button', { name: 'Search installed database', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Next page', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'installed geodata changed' })).toBeVisible()
  expect(queries[1]).toMatchObject({ snapshot: 'd'.repeat(64), offset: 50 })
  await expect(page.getByRole('button', { name: 'Add category rule', exact: true })).toHaveCount(0)
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
  await page.getByRole('button', { name: 'Reload current configuration', exact: true }).click()
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
