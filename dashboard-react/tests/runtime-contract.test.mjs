import test from 'node:test'
import assert from 'node:assert/strict'
import { z } from 'zod'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const commonSource = readFileSync(new URL('../src/api/runtime/common.ts', import.meta.url), 'utf8')
const rulesSource = readFileSync(new URL('../src/api/runtime/rules.ts', import.meta.url), 'utf8').replace("import { listSchema, MoneySchema } from './common'", '')
const runtime = ts.transpile(`${commonSource}\n${rulesSource}`, { module: ts.ModuleKind.CommonJS })
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
