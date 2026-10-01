import test from 'node:test'
import assert from 'node:assert/strict'

// These mirror src/pages/dashboardMetrics.ts. The node test runner cannot import
// TypeScript directly, so this documents the day-over-day contract the page relies on.
const ratioChange = (current, previous) => {
  if (!(previous > 0)) return null
  const percent = (current - previous) / previous * 100
  return `${percent >= 0 ? '+' : ''}${percent.toFixed(1)}%`
}

const pointChange = (currentRate, previousRate) => {
  if (previousRate === null) return null
  const points = currentRate - previousRate
  return `${points >= 0 ? '+' : ''}${points.toFixed(1)}pp`
}

const trendDirection = (current, previous) => {
  if (previous === null) return 'none'
  if (current > previous) return 'up'
  if (current < previous) return 'down'
  return 'flat'
}

const MODEL_TOP_N = 6
const modelDistribution = (rows) => {
  const sorted = rows
    .map(row => ({ name: row.model || 'unknown', requests: Number(row.request_count || 0), tokens: Number(row.total_tokens || 0), cost: row.total_cost || '0' }))
    .sort((a, b) => b.tokens - a.tokens)
  const total = sorted.reduce((sum, row) => sum + row.tokens, 0)
  const top = sorted.slice(0, MODEL_TOP_N)
  const rest = sorted.slice(MODEL_TOP_N)
  if (rest.length > 0) {
    top.push(rest.reduce((acc, row) => ({
      name: '其他',
      requests: acc.requests + row.requests,
      tokens: acc.tokens + row.tokens,
      cost: (Number(acc.cost || 0) + Number(row.cost || 0)).toFixed(6),
    }), { name: '其他', requests: 0, tokens: 0, cost: '0' }))
  }
  return { slices: top, total }
}

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

const tokenTrend = (rows) => rows.map(row => {
  const input = Number(row.input_tokens || 0)
  const cacheRead = Number(row.cached_input_tokens || 0)
  return { date: row.stat_date, input, output: Number(row.output_tokens || 0), cacheRead, cacheHitRate: input > 0 ? cacheRead / input * 100 : 0 }
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
