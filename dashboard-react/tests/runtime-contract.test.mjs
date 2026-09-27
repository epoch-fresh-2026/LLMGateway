import test from 'node:test'
import assert from 'node:assert/strict'
import { z } from 'zod'

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
