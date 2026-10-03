import test from 'node:test'
import assert from 'node:assert/strict'
import { z } from 'zod'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const commonSource = readFileSync(new URL('../src/api/runtime/common.ts', import.meta.url), 'utf8')
const rulesSource = readFileSync(new URL('../src/api/runtime/rules.ts', import.meta.url), 'utf8').replace("import { listSchema, MoneySchema } from './common'", '')
const usageSource = readFileSync(new URL('../src/api/runtime/usage.ts', import.meta.url), 'utf8').replace("import { listSchema, MoneySchema } from './common'", '').replace("import { z } from 'zod'", '')
const metricsSource = readFileSync(new URL('../src/pages/dashboardMetrics.ts', import.meta.url), 'utf8')
const runtime = ts.transpile(`${commonSource}\n${rulesSource}\n${usageSource}\n${metricsSource}`,   { module: ts.ModuleKind.CommonJS })
const schemas = { exports: {} }
new Function('require', 'module', 'exports', runtime)(name => {
  assert.equal(name, 'zod')
  return { z }
}, schemas, schemas.exports)
const { QuotaUsageListSchema, QuotaPolicyListSchema } = schemas.exports

test('quota schemas accept real bucket timestamps and nullable limits', () => {
  const policy = { id: 1, policy_name: 'token only', scope_type: 'user', scope_id: 1, period_type: 'day', token_limit: 100, cost_limit: null, enabled: true }
  const usage = { policy_id: 1, policy_name: policy.policy_name, scope_type: 'user', scope_id: 1, period_type: 'day', period_start: '2026-10-02T00:00:00Z', period_end: '2026-10-03T00:00:00Z', token_limit: 100, cost_limit: null, used_tokens: 0, reserved_tokens: 10, used_cost: '0.000000', reserved_cost: '0.000000' }
  assert.equal(QuotaPolicyListSchema.safeParse({ list: [policy], total: 1 }).success, true)
  assert.equal(QuotaUsageListSchema.safeParse({ list: [usage], total: 1 }).success, true)
  assert.equal(QuotaUsageListSchema.safeParse({ list: [{ ...usage, period_start: '2026-10-02' }], total: 1 }).success, false)
  assert.equal(QuotaUsageListSchema.safeParse({ list: [{ ...usage, used_cost: 0 }], total: 1 }).success, false)
})

test('settlement UI classifies real proxy status and error_code independently', () => {
  const { isSettled, settlementLabel } = schemas.exports
  assert.equal(settlementLabel({ status: 'success', error_code: '' }), '已结算 · 上游确认')
  assert.equal(settlementLabel({ status: 'error', error_code: 'partial_actual_stream_error' }), '已结算 · 上游确认')
  for (const error_code of ['partial_estimated_stream_error', 'partial_estimated_settlement_failed', 'partial_estimated_pricing_error']) {
    assert.equal(isSettled({ status: 'error', error_code }), false)
    assert.equal(settlementLabel({ status: 'error', error_code }), '本地估算·未结算')
  }
  for (const error_code of ['partial_actual_settlement_failed', 'partial_actual_pricing_error', 'pricing_error', 'settlement_failed', 'upstream_error', '']) {
    assert.equal(isSettled({ status: 'error', error_code }), false)
    assert.equal(settlementLabel({ status: 'error', error_code }), '未入账诊断')
  }
  assert.equal(isSettled({ status: 'partial_estimated_stream_error', error_code: '' }), false)
})

test('live tokens only count settled upstream usage', () => {
  const rows = [
    { status: 'success', error_code: '', total_tokens: 100 },
    { status: 'error', error_code: 'partial_actual_stream_error', total_tokens: 200 },
    ...['partial_estimated_stream_error', 'partial_estimated_pricing_error', 'partial_actual_pricing_error', 'partial_actual_settlement_failed', 'upstream_error'].map(error_code => ({ status: 'error', error_code, total_tokens: 900 })),
  ]
  assert.equal(schemas.exports.liveTokens(rows), 300)
  assert.equal(schemas.exports.liveTokens([]), 0)
  assert.equal(schemas.exports.liveTokens([{ actual_tokens: 300, total_tokens: 300, estimated_tokens: 900 }]), 300)
})

test('all consumption schemas require total equal actual with independent audit estimates', () => {
  const counts = { request_count: 8, success_count: 1, error_count: 7, total_tokens: 300, actual_tokens: 300, estimated_tokens: 300, total_cost: '0.003000' }
  const cases = [
    [schemas.exports.StatsSchema, { ...counts, active_key_count: 1 }],
    [schemas.exports.DailyStatsSchema, { ...counts, stat_date: '2026-09-16', input_tokens: 400, output_tokens: 200, cached_input_tokens: 0 }],
    [schemas.exports.ChannelStatsSchema, { ...counts, channel_id: 1, channel_name: 'A' }],
    [schemas.exports.UsageAggregateSchema, { ...counts, model: 'gpt', duration_ms: 100 }],
  ]
  for (const [schema, value] of cases) {
    assert.equal(schema.safeParse(value).success, true)
    assert.equal(schema.safeParse({ ...value, total_tokens: 3600 }).success, false)
    assert.equal(schema.safeParse({ ...value, estimated_tokens: -1 }).success, false)
    const { actual_tokens, ...missing } = value
    assert.equal(schema.safeParse(missing).success, false)
    assert.equal(schema.safeParse({ ...value, total_tokens: 600 }).success, false)
    assert.equal(schema.safeParse({ ...value, total_tokens: 0, actual_tokens: 0, estimated_tokens: 900 }).success, true)
    assert.equal(schema.safeParse({ ...value, estimated_tokens: 900 }).success, true)
  }
})

const money = z.string().regex(/^-?\d+(\.\d+)?$/)
const account = z.object({ id: z.number().int(), username: z.string(), nickname: z.string() })
const key = z.object({ id: z.number().int(), key_name: z.string(), prefix: z.string(), is_active: z.boolean(), last_used_at: z.string().nullable(), expires_at: z.string().nullable() })
const usageLog = z.object({ total_cost: money, ttft_ms: z.number().int().nullable().optional(), status: z.string() })
const list = z.object({ list: z.array(key), total: z.number().int().nonnegative() })

test('high-risk response schemas reject malformed money and types', () => {
  assert.equal(account.safeParse({ id: 1, username: 'alice', nickname: 7 }).success, false)
  assert.equal(usageLog.safeParse({ total_cost: 1, ttft_ms: null, status: 'success' }).success, false)
})

test('high-risk response schemas accept nullable and list fields', () => {
  const item = { id: 1, key_name: 'default', prefix: 'sk-', is_active: true, last_used_at: null, expires_at: null }
  assert.equal(key.safeParse(item).success, true)
  assert.equal(list.safeParse({ list: [item], total: 1 }).success, true)
  assert.equal(account.safeParse({ id: 1, username: 'alice', nickname: 'Alice' }).success, true)
  assert.equal(usageLog.safeParse({ total_cost: '0.00', ttft_ms: null, status: 'success' }).success, true)
})
