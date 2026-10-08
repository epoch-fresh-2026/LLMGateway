import { expect, it } from 'vitest'
import { compact, isSettled, settlementLabel, liveTokens, modelDistribution, pointChange, ratioChange, recentUtcDays, tokenTrend, trendDirection, utcDayKey } from '../src/pages/dashboardMetrics'

it('compares real UTC statistics without fabricating a zero baseline', () => {
  expect(utcDayKey(new Date('2026-10-08T00:30:00+08:00'))).toBe('2026-10-07')
  expect(recentUtcDays(3, new Date('2026-10-08T12:00:00Z'))).toEqual(['2026-10-06', '2026-10-07', '2026-10-08'])
  expect(recentUtcDays(0)).toEqual([])
  expect(ratioChange(12, 0)).toBeNull(); expect(ratioChange(112, 100)).toBe('+12.0%'); expect(ratioChange(88, 100)).toBe('-12.0%')
  expect(pointChange(99.4, 99)).toBe('+0.4pp'); expect(pointChange(99, 99.4)).toBe('-0.4pp'); expect(pointChange(99, null)).toBeNull()
  expect([trendDirection(12, 10), trendDirection(8, 10), trendDirection(10, 10), trendDirection(10, null)]).toEqual(['up', 'down', 'flat', 'none'])
  expect([compact(10), compact(1500), compact(2500000)]).toEqual(['10', '1.5K', '2.5M'])
})
it('distinguishes settled upstream tokens from estimated zero-charge audit', () => {
  const cases = ['partial_actual_stream_error', 'partial_estimated_stream_error', 'partial_estimated_settlement_failed', 'partial_estimated_pricing_error', 'partial_actual_settlement_failed', 'partial_actual_pricing_error', 'pricing_error', 'settlement_failed', 'upstream_error', '']
  for (const error_code of cases) {
    const item = { status: 'error', error_code }
    expect(isSettled(item)).toBe(error_code === 'partial_actual_stream_error')
    expect(settlementLabel(item)).toBe(error_code === 'partial_actual_stream_error' ? '已结算 · 上游确认' : error_code.startsWith('partial_estimated_') ? '本地估算·未结算' : '未入账诊断')
  }
  expect(settlementLabel({ status: 'success', error_code: '' })).toBe('已结算 · 上游确认')
  expect(liveTokens([{ status: 'success', error_code: '', total_tokens: 100 }, { status: 'error', error_code: 'upstream_error', total_tokens: 999 }, { actual_tokens: 200 }])).toBe(300)
  expect(liveTokens([])).toBe(0)
})
it('builds trends and aggregates the top six model slices with an honest empty range', () => {
  expect(tokenTrend([{ stat_date: '2026-10-07', input_tokens: 1000, output_tokens: 500, cached_input_tokens: 250 }, { stat_date: '2026-10-08', input_tokens: 0, output_tokens: 0, cached_input_tokens: 0 }]).map(p => p.cacheHitRate)).toEqual([25, 0])
  expect(modelDistribution([])).toEqual({ slices: [], total: 0 })
  const rows = Array.from({ length: 9 }, (_, index) => ({ model: `m${index}`, request_count: index + 1, total_tokens: (index + 1) * 100, actual_tokens: (index + 1) * 100, estimated_tokens: 50, total_cost: '0.100000' }))
  const result = modelDistribution(rows)
  expect(result.total).toBe(4500); expect(result.slices).toHaveLength(7)
  expect(result.slices[0].name).toBe('m8')
  expect(result.slices.at(-1)).toEqual({ name: '其他', requests: 6, tokens: 600, actual: 600, estimated: 150, cost: '0.300000' })
  expect(modelDistribution([{ model: '', request_count: 0, total_tokens: 0, total_cost: '' }]).slices[0].name).toBe('unknown')
})
