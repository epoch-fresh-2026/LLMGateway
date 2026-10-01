// Dashboard metric helpers. Backend daily stats aggregate usage_logs by UTC
// natural day, so the console must build its "today/yesterday" keys in UTC too
// or the comparison silently reads the wrong rows.
export type DailyPoint = {
  stat_date: string
  request_count: number
  success_count: number
  error_count: number
  total_tokens: number
  total_cost: string
}

export type TrendDirection = 'up' | 'down' | 'flat' | 'none'

export const utcDayKey = (date: Date): string => date.toISOString().slice(0, 10)

// trendDirection classifies a delta for arrow/color styling. A null baseline
// (see ratioChange) is "none" so the card renders a neutral dash.
export function trendDirection(current: number, previous: number | null): TrendDirection {
  if (previous === null) return 'none'
  if (current > previous) return 'up'
  if (current < previous) return 'down'
  return 'flat'
}

// recentUtcDays returns the last count UTC day keys, oldest first, ending today.
export function recentUtcDays(count: number, today: Date = new Date()): string[] {
  const end = Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate())
  const keys: string[] = []
  for (let offset = count - 1; offset >= 0; offset -= 1) {
    keys.push(utcDayKey(new Date(end - offset * 86_400_000)))
  }
  return keys
}

// ratioChange returns the relative change as display text. A zero or missing
// baseline yields null so the UI can show "—" instead of a fabricated percent.
export function ratioChange(current: number, previous: number): string | null {
  if (!(previous > 0)) return null
  const percent = (current - previous) / previous * 100
  return `${percent >= 0 ? '+' : ''}${percent.toFixed(1)}%`
}

// pointChange returns the percentage-point difference between two rates, or
// null when the baseline rate is unknown (no requests in the previous window).
export function pointChange(currentRate: number, previousRate: number | null): string | null {
  if (previousRate === null) return null
  const points = currentRate - previousRate
  return `${points >= 0 ? '+' : ''}${points.toFixed(1)}pp`
}

// compact renders large token/request counts as K/M for statistics surfaces.
// Request-log detail views intentionally keep exact counts instead.
export const compact = (value: number): string =>
  value >= 1_000_000 ? `${(value / 1_000_000).toFixed(1)}M` : value >= 1_000 ? `${(value / 1_000).toFixed(1)}K` : value.toLocaleString()

export type TokenTrendPoint = { date: string; input: number; output: number; cacheRead: number; cacheHitRate: number }

// tokenTrend shapes daily rows into the token-usage line chart points. Cache
// hit rate is cached input over prompt input (input includes cached), and a day
// with no input reports 0%.
export function tokenTrend(rows: { stat_date: string; input_tokens: number; output_tokens: number; cached_input_tokens: number }[]): TokenTrendPoint[] {
  return rows.map(row => {
    const input = Number(row.input_tokens || 0)
    const cacheRead = Number(row.cached_input_tokens || 0)
    return {
      date: row.stat_date,
      input,
      output: Number(row.output_tokens || 0),
      cacheRead,
      cacheHitRate: input > 0 ? cacheRead / input * 100 : 0,
    }
  })
}

export type ModelSlice = { name: string; requests: number; tokens: number; cost: string }

export const MODEL_TOP_N = 6

// modelDistribution shapes the ranged model aggregate into donut slices/table
// rows: the top MODEL_TOP_N models by tokens plus an aggregated "其他" row. It
// only uses the ranged aggregate, so an empty range yields no rows.
export function modelDistribution(rows: { model: string; request_count: number; total_tokens: number; total_cost: string }[]): { slices: ModelSlice[]; total: number } {
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
