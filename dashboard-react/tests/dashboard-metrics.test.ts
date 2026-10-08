import { test } from 'vitest'
import assert from 'node:assert/strict'
import { ratioChange, pointChange, trendDirection, modelDistribution, MODEL_TOP_N, tokenTrend } from '../src/pages/dashboardMetrics'

test('ratio change hides a zero baseline instead of fabricating a percent', () => {
  assert.equal(ratioChange(12, 0), null)
  assert.equal(ratioChange(0, 0), null)
})

test('ratio change formats sign and one decimal place', () => {
  assert.equal(ratioChange(112, 100), '+12.0%')
  assert.equal(ratioChange(88, 100), '-12.0%')
})

test('success-rate delta is reported in percentage points', () => {
  assert.equal(pointChange(99.4, 99.0), '+0.4pp')
  assert.equal(pointChange(99.0, 99.4), '-0.4pp')
  assert.equal(pointChange(99.0, null), null)
})

test('trend direction drives arrow and color independently of text', () => {
  assert.equal(trendDirection(12, 10), 'up')
  assert.equal(trendDirection(8, 10), 'down')
  assert.equal(trendDirection(10, 10), 'flat')
  assert.equal(trendDirection(10, null), 'none')
})


test('token trend computes cache hit rate over input and zero on empty input', () => {
  const [filled, empty] = tokenTrend([
    { stat_date: '2025-10-01', input_tokens: 1000, output_tokens: 500, cached_input_tokens: 250 },
    { stat_date: '2025-10-02', input_tokens: 0, output_tokens: 0, cached_input_tokens: 0 },
  ])
  assert.equal(filled.input, 1000)
  assert.equal(filled.output, 500)
  assert.equal(filled.cacheRead, 250)
  assert.equal(filled.cacheHitRate, 25)
  assert.equal(empty.cacheHitRate, 0)
})

test('model distribution keeps top N and aggregates the rest into 其他', () => {
  const rows = Array.from({ length: 9 }, (_, index) => ({ model: `m${index}`, request_count: index + 1, total_tokens: (index + 1) * 100, total_cost: '0.100000' }))
  const { slices, total } = modelDistribution(rows)
  assert.equal(slices.length, MODEL_TOP_N + 1)
  assert.equal(slices[0].name, 'm8') // highest tokens first
  assert.equal(slices.at(-1).name, '其他')
  assert.equal(slices.at(-1).tokens, 100 + 200 + 300)
  assert.equal(total, rows.reduce((sum, row) => sum + row.total_tokens, 0))
})

test('model distribution omits 其他 when there are at most N models', () => {
  const { slices, total } = modelDistribution([{ model: 'a', request_count: 3, total_tokens: 30, total_cost: '0.010000' }])
  assert.deepEqual(slices.map(slice => slice.name), ['a'])
  assert.equal(total, 30)
})

test('model distribution is empty for an empty range', () => {
  assert.deepEqual(modelDistribution([]), { slices: [], total: 0 })
})
