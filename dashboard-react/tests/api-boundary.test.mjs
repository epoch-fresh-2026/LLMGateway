import test from 'node:test'
import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { createRequire } from 'node:module'
import ts from 'typescript'
import { renderToStaticMarkup } from 'react-dom/server'

const require = createRequire(import.meta.url)
const metricsSource = await readFile(new URL('../src/pages/dashboardMetrics.ts', import.meta.url), 'utf8')
const metrics = {}
new Function('exports', ts.transpileModule(metricsSource, { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText)(metrics)

async function quotaPage({ policies, usage, creating = false, create = async () => {}, remove = async () => {} }) {
  const source = await readFile(new URL('../src/pages/ConsolePages.tsx', import.meta.url), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX } }).outputText
  const queries = [policies, usage, { data: { list: [] } }]
  let stateIndex = 0
  const exports = {}
  const load = name => {
    if (name === 'react') return { useState: initial => [stateIndex++ === 0 ? creating : initial, () => {}] }
    if (name === '@tanstack/react-query') return { useQuery: () => queries.shift() }
    if (name === './dashboardMetrics') return metrics
    if (name === '../auth/AuthProvider') return { useSession: () => ({ account: { id: 1 } }) }
    if (name === '../api/quota') return { createQuotaPolicy: create, deleteQuotaPolicy: remove }
    if (name === '../components/feedback/Modal') return { Modal: () => null }
    if (name.startsWith('../api/')) return {}
    return require(name)
  }
  const FormDataStub = class {
    get(name) { return { policy_name: 'new', token_limit: '100', period_type: 'day', enabled: 'on' }[name] ?? null }
  }
  new Function('require', 'exports', 'FormData', compiled)(load, exports, FormDataStub)
  return exports.QuotasPage()
}

function findElement(node, predicate) {
  if (!node || typeof node !== 'object') return undefined
  if (predicate(node)) return node
  for (const child of [node.props?.children].flat(Infinity)) {
    const found = findElement(child, predicate)
    if (found) return found
  }
}

const quotaPolicies = { data: { list: [
  { id: 1, policy_name: 'daily', scope_type: 'user', period_type: 'day', token_limit: 100, cost_limit: '1.000000', enabled: true },
  { id: 2, policy_name: 'monthly', scope_type: 'user', period_type: 'month', token_limit: null, cost_limit: '2.000000', enabled: true },
] } }

test('breaker configuration is user-scoped while channel health and testing remain independent', async () => {
  const files = ['api/catalog.ts', 'api/paths.ts', 'api/client.ts', 'api/runtime/catalog.ts', 'types/api.ts', 'pages/ChannelsPage.tsx']
  for (const file of files) {
    const source = await readFile(new URL(`../src/${file}`, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /ChannelBreakerConfig|channelBreaker|channel-breaker|\/breaker`|setGlobalBreaker|全局熔断配置|恢复全局默认/, file)
  }
  const catalog = await readFile(new URL('../src/api/catalog.ts', import.meta.url), 'utf8')
  const paths = await readFile(new URL('../src/api/paths.ts', import.meta.url), 'utf8')
  const page = await readFile(new URL('../src/pages/ChannelsPage.tsx', import.meta.url), 'utf8')
  assert.match(paths, /breakerConfig:.*\/admin\/breaker-config/)
  for (const operation of ['getUserBreakerConfig', 'updateUserBreakerConfig', 'deleteUserBreakerConfig']) {
    assert.match(catalog, new RegExp(`export const ${operation} = .*apiPaths\\.breakerConfig\\(\\)`))
    assert.match(page, new RegExp(operation))
  }
  for (const operation of ['listChannelHealth', 'resetChannelHealth', 'testChannel']) {
    assert.match(catalog, new RegExp(`export const ${operation}`))
    assert.match(page, new RegExp(operation))
  }
  assert.match(page, /title="用户熔断配置"/)
  assert.match(page, /setUserBreaker\(true\).*用户熔断配置/)
})

test('rate limit update contract only accepts a required enabled boolean and keeps PUT', async () => {
  const source = await readFile(new URL('../src/api/generated/schema.ts', import.meta.url), 'utf8')
  const ast = ts.createSourceFile('schema.ts', source, ts.ScriptTarget.Latest, true)
  let input
  const visit = node => {
    if (ts.isPropertySignature(node) && node.name.getText(ast) === 'RateLimitUpdateInput') input = node.type
    ts.forEachChild(node, visit)
  }
  visit(ast)
  assert.ok(input && ts.isTypeLiteralNode(input))
  assert.deepEqual(input.members.map(member => member.name.getText(ast)), ['enabled'])
  assert.equal(input.members[0].questionToken, undefined)
  assert.equal(input.members[0].type.kind, ts.SyntaxKind.BooleanKeyword)
  const api = await readFile(new URL('../src/api/ratelimit.ts', import.meta.url), 'utf8')
  assert.match(api, /updateRateLimit = \(id: number, input: RateLimitUpdateInput\) => adminSend\('PUT', apiPaths\.rateLimit\(id\), input, RateLimitSchema\)/)
})

test('quota update API and dedicated schema are absent', async () => {
  for (const file of ['api/quota.ts', 'types/api.ts', 'api/generated/schema.ts', 'api/runtime/rules.ts']) {
    const source = await readFile(new URL(`../src/${file}`, import.meta.url), 'utf8')
    assert.doesNotMatch(source, /updateQuotaPolicy|QuotaPolicyUpdateInput|QuotaPolicyUpdateSchema/)
  }
})

test('quota usage matches policy ids, preserves money strings and never displays reservations', async () => {
  const usage = { data: { list: [
    { policy_id: 2, used_tokens: 42, used_cost: '9007199254740993.123456', reserved_tokens: 999, reserved_cost: '888.000000' },
    { policy_id: 1, used_tokens: 7, used_cost: '0.000001', reserved_tokens: 999, reserved_cost: '888.000000' },
  ] } }
  const html = renderToStaticMarkup(await quotaPage({ policies: quotaPolicies, usage }))
  assert.match(html, /<th>Token 限额<\/th><th>已用Token<\/th><th>费用限额<\/th><th>已用费用<\/th>/)
  assert.doesNotMatch(html, /已用量/)
  assert.match(html, /daily[\s\S]*<td class="mono">100<\/td><td class="mono">7<\/td><td class="mono">1\.000000<\/td><td class="mono">0\.000001<\/td>[\s\S]*monthly[\s\S]*<td class="mono">—<\/td><td class="mono">42<\/td><td class="mono">2\.000000<\/td><td class="mono">9007199254740993\.123456<\/td>/)
  assert.doesNotMatch(html, /999|888\.000000|reserved/)
})

test('quota policies without a usage bucket display zero', async () => {
  for (const list of [[], [{ policy_id: 3, used_tokens: 123, used_cost: '5.000000' }]]) {
    const html = renderToStaticMarkup(await quotaPage({ policies: quotaPolicies, usage: { data: { list } } }))
    assert.equal(html.match(/<td class="mono">0<\/td><td class="mono">[12]\.000000<\/td><td class="mono">0\.000000<\/td>/g)?.length, 2)
  }
})

test('quota token limits and usage reuse compact formatting while nullable limits stay empty', async () => {
  const policies = { data: { list: [
    { ...quotaPolicies.data.list[0], token_limit: 1500, cost_limit: null },
    { ...quotaPolicies.data.list[1], token_limit: 2500000 },
    { ...quotaPolicies.data.list[0], id: 3, token_limit: 0, cost_limit: '0.000000' },
  ] } }
  const usage = { data: { list: [
    { policy_id: 1, used_tokens: 1200000, used_cost: '0.123456' },
    { policy_id: 2, used_tokens: 2300, used_cost: '9007199254740993.123456' },
  ] } }
  const html = renderToStaticMarkup(await quotaPage({ policies, usage }))
  assert.match(html, /<td class="mono">1\.5K<\/td><td class="mono">1\.2M<\/td><td class="mono">—<\/td><td class="mono">0\.123456<\/td>/)
  assert.match(html, /<td class="mono">2\.5M<\/td><td class="mono">2\.3K<\/td><td class="mono">2\.000000<\/td><td class="mono">9007199254740993\.123456<\/td>/)
  assert.match(html, /<td class="mono">0<\/td><td class="mono">0<\/td><td class="mono">0\.000000<\/td><td class="mono">0\.000000<\/td>/)
})

test('both quota queries gate loading and errors including failed refetches', async () => {
  for (const query of ['policies', 'usage']) {
    const queries = { policies: quotaPolicies, usage: { data: { list: [] } } }
    const loading = renderToStaticMarkup(await quotaPage({ ...queries, [query]: { isLoading: true } }))
    assert.match(loading, /加载中/)
    assert.doesNotMatch(loading, /Token 0|<table/)
    const failed = renderToStaticMarkup(await quotaPage({ ...queries, [query]: { ...queries[query], isError: true, error: new Error(`${query} failed`) } }))
    assert.match(failed, new RegExp(`${query} failed`))
    assert.doesNotMatch(failed, /Token 0|<table/)
  }
})

test('quota create, delete and manual refresh reload policies and usage', async () => {
  for (const action of ['create', 'delete', 'refresh']) {
    const calls = []
    const tree = await quotaPage({
      policies: { ...quotaPolicies, refetch: async () => calls.push('policies') },
      usage: { data: { list: [] }, refetch: async () => calls.push('usage') },
      creating: action === 'create',
      create: async () => calls.push('create'),
      remove: async id => calls.push(`delete:${id}`),
    })
    if (action === 'create') {
      const modal = findElement(tree, node => node.props?.title === '新建周期配额')
      await modal.props.onSubmit({ preventDefault() {}, currentTarget: {} })
    } else {
      const button = findElement(tree, node => node.type === 'button' && node.props.children === (action === 'delete' ? '删除' : '刷新'))
      button.props.onClick()
      await new Promise(resolve => setImmediate(resolve))
    }
    assert.deepEqual(calls, [...(action === 'create' ? ['create'] : action === 'delete' ? ['delete:1'] : []), 'policies', 'usage'])
  }
})

test('pages do not depend on the transport client', async () => {
  const pagesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'pages')
  const files = await readdir(pagesDir)
  for (const file of files.filter(name => name.endsWith('.tsx'))) {
    const source = await readFile(join(pagesDir, file), 'utf8')
    assert.doesNotMatch(source, /api\/client|adminGet|adminSend|catalogRequest|accountGet|accountRequest|rateLimitRequest|quotaRequest/, `${file} bypasses domain API modules`)
  }
})
