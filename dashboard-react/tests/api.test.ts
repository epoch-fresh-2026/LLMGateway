import { describe, expect, it, vi } from 'vitest'
import { z } from 'zod'
import * as accounts from '../src/api/accounts'
import * as auth from '../src/api/auth'
import * as catalog from '../src/api/catalog'
import * as quota from '../src/api/quota'
import * as ratelimit from '../src/api/ratelimit'
import * as usage from '../src/api/usage'
import { adminGet, adminSend, ApiError, AuthRequiredError, ApiContractError } from '../src/api/client'
import { apiPaths } from '../src/api/paths'
import { authErrorMessage } from '../src/api/errorMessages'
import { schemaName } from '../src/api/runtime/common'
import { AccountSchema, KeySchema } from '../src/api/runtime/accounts'
import { QuotaUsageListSchema, QuotaPolicyListSchema } from '../src/api/runtime/rules'
import { StatsSchema, DailyStatsSchema, ChannelStatsSchema, UsageAggregateSchema, UsageLogSchema } from '../src/api/runtime/usage'
import { account, aggregate, channel, counts, dailyPoint, fail, key, log, policy, quotaUsage, requests, respond, rule } from './support'

describe('real domain API transport', () => {
  it('sends every domain operation with its response schema, concrete URL, and JSON body', async () => {
    expect(await accounts.getProfile()).toEqual(account)
    await accounts.updateProfile({ nickname: 'Changed' })
    await accounts.listKeys(); await accounts.listKeys({ page: 2 })
    await accounts.createKey({ key_name: 'test', prefix: 'sk-' })
    await accounts.updateKey(11, { is_active: false }); await accounts.deleteKey(11)
    await auth.getMe(); await auth.login({ username: 'alice', password: 'testpassword' }); await auth.register({ username: 'bob', password: 'testpassword' }); await auth.logout()
    await catalog.listChannels(); await catalog.listChannels({ page: 2 }); await catalog.listChannelHealth()
    await catalog.createChannel({ name: 'test', base_url: channel.base_url, api_key: 'test-placeholder' })
    await catalog.updateChannel(2, { name: 'changed' }); await catalog.deleteChannel(2)
    await catalog.updateChannelStatus(2, { status: 0 }); await catalog.updateChannelBalance(2, { balance: '5.000000' })
    await catalog.resetChannelHealth(2); await catalog.getUserBreakerConfig()
    await catalog.updateUserBreakerConfig({ window_seconds: 60, minimum_samples: 10, error_rate_percent: 50, timeout_rate_percent: 50, cooldown_seconds: 30 }); await catalog.deleteUserBreakerConfig()
    await catalog.listChannelModels(2); await catalog.createChannelModel(2, { model_name: 'a', upstream_model: 'b', enabled: true })
    await catalog.updateChannelModel(2, 3, { model_name: 'a', enabled: false }); await catalog.deleteChannelModel(2, 3)
    await catalog.loadRemoteModels(2); await catalog.testChannel(2, false); await catalog.listModels(); await catalog.listModels({ status: 0 })
    await catalog.listPricing(); await catalog.listPricing({ page_size: 10 })
    await catalog.createPricing({ channel_id: 2, upstream_model: 'gpt-real', input_price_per_1m: '1.0', output_price_per_1m: '2.0', currency: 'USD' })
    await catalog.deletePricing({ channel_id: 2, upstream_model: 'gpt-real' })
    await ratelimit.listRateLimits(); await ratelimit.listRateLimits({ enabled: false })
    await ratelimit.createRateLimit({ ...rule, target_type: 'user', metric: 'rpm', action: 'reject' }); await ratelimit.updateRateLimit(5, { enabled: false }); await ratelimit.deleteRateLimit(5)
    await quota.listQuotaPolicies(); await quota.listQuotaPolicies({ enabled: false }); await quota.listQuotaUsage(); await quota.listQuotaUsage({ scope_id: 1 })
    await quota.createQuotaPolicy({ policy_name: 'cap', scope_type: 'user', scope_id: 1, period_type: 'day', token_limit: 100, enabled: true }); await quota.deleteQuotaPolicy(6)
    await usage.overview(); await usage.overview({ start_time: '2026-10-01T00:00:00Z' })
    await usage.logs(); await usage.logs({ page: 2 }); await usage.daily(); await usage.daily({ page: 2 })
    await usage.usageStats({ group_by: 'model' }); await usage.listUsageLogs(); await usage.listUsageLogs({ model: undefined }); await usage.getUsageLog(7)
    await usage.channelStats(); await usage.channelStats({ date_from: '2026-10-01' }); await usage.ttft(); await usage.ttft({ end_time: '2026-10-08T00:00:00Z' })
    expect(requests.find(r => r.method === 'DELETE' && r.path === '/admin/pricing')?.body).toEqual({ channel_id: 2, upstream_model: 'gpt-real' })
    expect(requests.find(r => r.path === '/admin/rate-limits' && r.query.has('enabled'))?.query.get('enabled')).toBe('false')
    expect(requests.filter(r => r.path === '/admin/usage-logs').every(r => !r.query.has('model'))).toBe(true)
    expect(apiPaths.channelHealth(2)).toBe('/admin/channels/2/health')
  })
  it('rejects bad envelopes, invalid runtime data, unauthenticated requests, and schema-less calls', async () => {
    respond('/admin/profile', { id: 'broken' })
    await expect(accounts.getProfile()).rejects.toBeInstanceOf(ApiContractError)
    respond('/admin/profile', fail('backend failure'))
    await expect(accounts.getProfile()).rejects.toMatchObject({ status: 500, code: 'failure', message: 'backend failure' })
    respond('/admin/profile', new Response(JSON.stringify({ code: 9, message: 'bad envelope' }), { headers: { 'Content-Type': 'application/json' } }))
    await expect(accounts.getProfile()).rejects.toMatchObject({ message: 'bad envelope' })
    respond('/admin/profile', new Response('{}', { headers: { 'Content-Type': 'application/json' } }))
    await expect(accounts.getProfile()).rejects.toMatchObject({ message: '/admin/profile failed' })
    const event = vi.fn(); window.addEventListener('auth:unauthorized', event)
    respond('/admin/profile', fail('', 401, 'unauthenticated'))
    await expect(accounts.getProfile()).rejects.toBeInstanceOf(AuthRequiredError)
    expect(event).toHaveBeenCalledOnce(); window.removeEventListener('auth:unauthorized', event)
    respond('/admin/profile', account)
    // The implementation preserves a schema-less transport fallback for legacy callers.
    expect(await (adminGet as unknown as (path: string, params: object) => Promise<unknown>)('/admin/profile', {})).toEqual(account)
    expect(await adminSend('PUT', '/admin/profile', { nickname: 'Alice' }, z.unknown())).toEqual(account)
  })
})

it('localizes auth codes and safely falls back for unknown failures', () => {
  expect(authErrorMessage(new ApiError(400, 'wrong_password', 'raw'), 'fallback')).toBe('密码错误')
  expect(authErrorMessage(new ApiError(500, 'unknown', 'server message'), 'fallback')).toBe('server message')
  expect(authErrorMessage(new ApiError(500, undefined, ''), 'fallback')).toBe('fallback')
  expect(authErrorMessage(new Error('network'), 'fallback')).toBe('network')
  expect(authErrorMessage(new Error(''), 'fallback')).toBe('fallback')
  expect(authErrorMessage(null, 'fallback')).toBe('fallback')
  expect(schemaName('/admin/profile', 'account')).toBe('/admin/profile returned invalid account')
})

it('validates actual quota buckets, money strings, nullable keys, and settled totals', () => {
  expect(QuotaPolicyListSchema.safeParse({ list: [policy], total: 1 }).success).toBe(true)
  expect(QuotaUsageListSchema.safeParse({ list: [quotaUsage], total: 1 }).success).toBe(true)
  expect(QuotaUsageListSchema.safeParse({ list: [{ ...quotaUsage, period_start: '2026-10-08' }], total: 1 }).success).toBe(false)
  expect(QuotaUsageListSchema.safeParse({ list: [{ ...quotaUsage, used_cost: 0 }], total: 1 }).success).toBe(false)
  expect(AccountSchema.safeParse({ ...account, nickname: 7 }).success).toBe(false)
  expect(KeySchema.safeParse({ ...key, last_used_at: null }).success).toBe(true)
  expect(UsageLogSchema.safeParse({ ...log, total_cost: 1 }).success).toBe(false)
  const cases = [[StatsSchema, { ...counts, active_key_count: 1 }], [DailyStatsSchema, dailyPoint], [ChannelStatsSchema, { ...counts, channel_id: 2, channel_name: 'Primary' }], [UsageAggregateSchema, aggregate]] as const
  for (const [schema, value] of cases) {
    expect(schema.safeParse(value).success).toBe(true)
    expect(schema.safeParse({ ...value, total_tokens: 3600 }).success).toBe(false)
    expect(schema.safeParse({ ...value, estimated_tokens: -1 }).success).toBe(false)
    expect(schema.safeParse({ ...value, total_tokens: 0, actual_tokens: 0, estimated_tokens: 900 }).success).toBe(true)
  }
})
