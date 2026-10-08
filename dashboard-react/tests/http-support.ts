import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'

export const stamp = '2026-10-08T10:00:00Z'
export const account = { id: 1, username: 'alice', nickname: 'Alice' }
export const key = { id: 11, key_name: 'production', prefix: 'sk-', key_suffix: '1234567890', is_active: true, created_at: stamp, last_used_at: stamp, expires_at: null }
export const channel = { id: 2, name: 'Primary', base_url: 'https://upstream.example/v1', auth_type: 'bearer', status: 1, weight: 100, priority: 0, balance: '10.000000', model_count: 1 }
export const health = { channel_id: 2, state: 'open', consecutive_failures: 2, success_count: 5, failure_count: 2, opened_at: stamp, updated_at: stamp }
export const mapping = { id: 3, model_name: 'gpt', upstream_model: 'gpt-real', enabled: true }
export const pricing = { id: 4, channel_id: 2, channel_name: 'Primary', upstream_model: 'gpt-real', input_price_per_1m: '0.100000', output_price_per_1m: '0.200000', cached_input_price_per_1m: '0.010000', currency: 'USD' }
export const counts = { request_count: 10, success_count: 8, error_count: 2, total_tokens: 300, actual_tokens: 300, estimated_tokens: 40, total_cost: '0.300000' }
export const dailyPoint = { ...counts, stat_date: new Date().toISOString().slice(0, 10), input_tokens: 200, output_tokens: 100, cached_input_tokens: 50 }
export const aggregate = { ...counts, user_id: 1, api_key_id: 11, channel_id: 2, model: 'gpt', duration_ms: 100 }
export const log = { id: 7, request_id: 'req-7', user_id: 1, api_key_id: 11, channel_id: 2, channel_name: 'Primary', model: 'gpt', upstream_model: 'gpt-real', input_tokens: 200, output_tokens: 100, cached_input_tokens: 50, total_tokens: 300, unit_price_input_per_1m: '0.100000', unit_price_output_per_1m: '0.200000', total_cost: '0.000040', duration_ms: 100, ttft_ms: 20, status: 'success', error_code: '', client_ip: '127.0.0.1', created_at: stamp }
export const rule = { id: 5, rule_name: 'minute cap', target_type: 'user', target_value: '*', metric: 'rpm', limit_value: 1000, action: 'reject', priority: 0, enabled: true, extras: {} }
export const policy = { id: 6, policy_name: 'daily cap', scope_type: 'user', scope_id: 1, period_type: 'day', token_limit: 1500, cost_limit: '1.000000', enabled: true }
export const quotaUsage = { policy_id: 6, policy_name: 'daily cap', scope_type: 'user', scope_id: 1, period_type: 'day', period_start: '2026-10-08T00:00:00Z', period_end: '2026-10-09T00:00:00Z', token_limit: 1500, cost_limit: '1.000000', used_tokens: 7, reserved_tokens: 999, used_cost: '0.000001', reserved_cost: '888.000000' }
export const breaker = { window_seconds: 60, minimum_samples: 10, error_rate_percent: 50, timeout_rate_percent: 50, cooldown_seconds: 30 }
export const list = (items: unknown[], total = items.length) => ({ list: items, total })
export const ok = (data: unknown) => HttpResponse.json({ code: 0, message: 'ok', data })
export const fail = (message = 'request failed', status = 500, code = 'failure') => HttpResponse.json({ code: 1, message, data: {}, error_code: code }, { status })
export const requests: { method: string; path: string; query: URLSearchParams; body: unknown }[] = []
let responses = new Map<string, unknown>()
export function resetHttp() { responses = new Map(); requests.length = 0 }
export function respond(path: string, data: unknown, method = 'GET') { responses.set(`${method} ${path}`, data) }
function defaultData(path: string, method: string): unknown {
  if (method === 'DELETE') return { deleted: true }
  if (path.includes('/auth/logout')) return {}
  if (path.includes('/auth/') || path === '/admin/profile') return account
  if (path === '/admin/keys') return method === 'POST' ? { id: 11, full_key: 'sk-test-only-secret' } : list([key])
  if (/\/keys\/\d+$/.test(path)) return key
  if (path === '/admin/channels/health') return list([health])
  if (path === '/admin/breaker-config') return breaker
  if (path.endsWith('/health/reset')) return { deleted: true }
  if (path.endsWith('/remote-models')) return { ok: true, models: [{ id: 'gpt-new' }, { id: 'gpt' }] }
  if (path.endsWith('/test')) return { list: [{ model_alias: 'gpt', upstream_model: 'gpt-real', http_status: 200, latency_ms: 5, ok: true, error: '' }] }
  if (/\/models\/\d+$/.test(path)) return mapping
  if (/\/channels\/\d+\/models$/.test(path)) return method === 'GET' ? list([mapping]) : mapping
  if (path === '/admin/channels') return method === 'GET' ? list([channel]) : channel
  if (/\/channels\/\d+/.test(path)) return channel
  if (path === '/admin/models') return list([{ model_name: 'gpt', status: 1, channels: [{ channel_id: 2, channel_name: 'Primary', upstream_model: 'gpt-real', enabled: true }] }])
  if (path === '/admin/pricing') return method === 'GET' ? list([pricing]) : pricing
  if (path === '/admin/rate-limits') return method === 'GET' ? list([rule]) : rule
  if (/\/rate-limits\/\d+$/.test(path)) return rule
  if (path === '/admin/quota-policies') return method === 'GET' ? list([policy]) : policy
  if (path === '/admin/quota-usage') return list([quotaUsage])
  if (path === '/admin/stats/overview') return { ...counts, active_key_count: 1 }
  if (path === '/admin/stats/daily') return list([dailyPoint])
  if (path === '/admin/stats/channels') return list([{ ...counts, channel_id: 2, channel_name: 'Primary' }])
  if (path === '/admin/stats/ttft') return { sample_count: 2, average_ms: 20, p50_ms: 20, p95_ms: 30, p99_ms: 35 }
  if (path === '/admin/stats/usage') return list([aggregate])
  if (path === '/admin/usage-logs') return list([log], 41)
  if (/\/usage-logs\/\d+$/.test(path)) return log
  throw new Error(`No HTTP fixture for ${method} ${path}`)
}
export const server = setupServer(http.all('http://localhost/admin/*', async ({ request }) => {
  const url = new URL(request.url)
  const body = request.body ? await request.json() : undefined
  requests.push({ method: request.method, path: url.pathname, query: url.searchParams, body })
  const override = responses.get(`${request.method} ${url.pathname}`)
  if (override instanceof Response) return override.clone()
  return ok(override === undefined ? defaultData(url.pathname, request.method) : override)
}))
