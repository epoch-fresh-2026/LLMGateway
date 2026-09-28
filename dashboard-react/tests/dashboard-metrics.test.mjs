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
