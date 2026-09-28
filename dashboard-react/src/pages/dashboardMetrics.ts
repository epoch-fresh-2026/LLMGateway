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

export const utcDayKey = (date: Date): string => date.toISOString().slice(0, 10)

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
