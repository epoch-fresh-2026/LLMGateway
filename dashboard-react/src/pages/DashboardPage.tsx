import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { listChannels, listChannelHealth, resetChannelHealth } from '../api/catalog'
import { daily, logs, overview, ttft, usageStats } from '../api/usage'
import { AsyncState } from '../components/feedback/AsyncState'
import { compact, settlementLabel, liveTokens as settledLiveTokens, modelDistribution, pointChange, ratioChange, recentUtcDays, tokenTrend, trendDirection, type ModelSlice, type TrendDirection } from './dashboardMetrics'
import { TokenTrendChart } from '../components/charts/TokenTrendChart'
import type { Channel, DailyStats, UsageLog } from '../types/api'

const n = (value: unknown) => Number(value || 0)
const money = (value: unknown) => Number(value || 0).toFixed(2)
const TREND_DAYS = 14
// Range options drive the model distribution and channel health panels below.
const RANGE_OPTIONS = [
  { key: 'today', label: '今天', days: 1 },
  { key: '7d', label: '近 7 天', days: 7 },
  { key: '14d', label: '近 14 天', days: 14 },
  { key: '30d', label: '近 30 天', days: 30 },
] as const
type RangeKey = typeof RANGE_OPTIONS[number]['key']
const LIVE_WINDOW_MS = 60_000
const emptyDay = (key: string): DailyStats => ({ stat_date: key, request_count: 0, success_count: 0, error_count: 0, total_tokens: 0, actual_tokens: 0, estimated_tokens: 0, input_tokens: 0, output_tokens: 0, cached_input_tokens: 0, total_cost: '0' })

function Panel({ title, desc, children, className = '' }: { title: string; desc: string; children: React.ReactNode; className?: string }) {
  return <section className={`panel ${className}`}><div className="panel-head"><div><h3>{title}</h3><p className="muted">{desc}</p></div></div>{children}</section>
}

// TrendCard renders a day-over-day metric; the arrow and color come from the
// direction, never from the text, so a decrease is a red down arrow.
function TrendCard({ label, value, delta, direction }: { label: string; value: string; delta: string | null; direction: TrendDirection }) {
  const arrow = direction === 'up' ? '↑ ' : direction === 'down' ? '↓ ' : ''
  return <section className="metric"><span>{label}</span><strong>{value}</strong><small className={`trend ${direction}`}>{delta === null ? '—' : `${arrow}${delta}`} 较昨日</small></section>
}

// StatCard renders a point-in-time value with a plain caption (no trend arrow).
function StatCard({ label, value, hint }: { label: string; value: string; hint: string }) {
  return <section className="metric"><span>{label}</span><strong>{value}</strong><small>{hint}</small></section>
}

const DONUT_COLORS = ['#22d3ee', '#3b82f6', '#34d399', '#fbbf24', '#fb7185', '#a78bfa', '#71717a']
const DONUT_RADIUS = 15.9155
const DONUT_LENGTH = 100

// ModelDonut renders a proportional SVG donut: one sector per slice (by tokens)
// with a custom hover tooltip showing the model name and its token count.
function ModelDonut({ slices, total }: { slices: ModelSlice[]; total: number }) {
  const [hovered, setHovered] = useState<{ index: number; x: number; y: number } | null>(null)
  const scale = Math.max(total, 1)
  let offset = 0
  const arcs = slices.map((slice, index) => {
    const dash = slice.tokens / scale * DONUT_LENGTH
    const arc = { slice, index, dash, offset }
    offset += dash
    return arc
  })
  const active = hovered ? slices[hovered.index] : null
  return <div className="donut-wrap">
    <svg className="donut-svg" viewBox="0 0 42 42" role="img" aria-label="模型用量分布">
      <circle className="donut-track" cx="21" cy="21" r={DONUT_RADIUS} fill="none" />
      {arcs.map(arc => <circle key={arc.slice.name} className="donut-arc" cx="21" cy="21" r={DONUT_RADIUS} fill="none" stroke={DONUT_COLORS[arc.index % DONUT_COLORS.length]} strokeDasharray={`${arc.dash} ${DONUT_LENGTH - arc.dash}`} strokeDashoffset={-arc.offset} transform="rotate(-90 21 21)" onMouseEnter={() => setHovered({ index: arc.index, x: 0, y: 0 })} onMouseMove={event => { const rect = event.currentTarget.ownerSVGElement?.getBoundingClientRect(); if (rect) setHovered({ index: arc.index, x: event.clientX - rect.left, y: event.clientY - rect.top }) }} onMouseLeave={() => setHovered(null)} />)}
    </svg>
    <div className="donut-center"><strong>{compact(total)}</strong><small>tokens</small></div>
    {active && hovered && <div className="donut-tooltip" style={{ left: hovered.x, top: hovered.y }}><b>{active.name}</b><span>{compact(active.tokens)} tokens</span></div>}
  </div>
}

export function DashboardPage() {
  const client = useQueryClient(); const [actionError, setActionError] = useState('')
  const windowDays = recentUtcDays(TREND_DAYS); const windowFrom = windowDays[0]; const windowTo = windowDays[windowDays.length - 1]
  const [rangeFrom, setRangeFrom] = useState(() => recentUtcDays(7)[0])
  const [rangeTo, setRangeTo] = useState(() => recentUtcDays(1)[0])
  const [rangeKey, setRangeKey] = useState<RangeKey | 'custom'>('7d')
  const applyRange = (key: RangeKey) => {
    const days = RANGE_OPTIONS.find(item => item.key === key)?.days ?? 7
    const keys = recentUtcDays(days)
    setRangeFrom(keys[0]); setRangeTo(keys[keys.length - 1]); setRangeKey(key)
  }
  const overviewQuery = useQuery({ queryKey: ['overview'], queryFn: () => overview(), staleTime: 30_000 })
  const dailyQuery = useQuery({ queryKey: ['daily', windowFrom, windowTo], queryFn: () => daily({ date_from: windowFrom, date_to: windowTo }), staleTime: 30_000 })
  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: () => listChannels(), staleTime: 60_000 })
  const healthQuery = useQuery({ queryKey: ['channel-health'], queryFn: listChannelHealth, staleTime: 30_000 })
  const logsQuery = useQuery({ queryKey: ['logs'], queryFn: () => logs(), staleTime: 10_000 })
  const modelUsage = useQuery({ queryKey: ['model-usage', rangeFrom, rangeTo], queryFn: () => usageStats({ group_by: 'model', date_from: rangeFrom, date_to: rangeTo, page: 1, page_size: 100 }) })
  const channelUsage = useQuery({ queryKey: ['dashboard-channel-usage', rangeFrom, rangeTo], queryFn: () => usageStats({ group_by: 'channel', date_from: rangeFrom, date_to: rangeTo, page: 1, page_size: 1000 }) })
  // Live throughput and latency share one 60s window; 10s polling keeps the
  // "current" cards fresh without a dedicated backend endpoint.
  const liveQuery = useQuery({
    queryKey: ['dashboard-live'],
    queryFn: async () => {
      const end = new Date(); const start = new Date(end.getTime() - LIVE_WINDOW_MS)
      const range = { start_time: start.toISOString(), end_time: end.toISOString() }
      const [usage, latency] = await Promise.all([
        usageStats({ group_by: 'model', page: 1, page_size: 1000, ...range }),
        ttft(range),
      ])
      return { usage, latency }
    },
    staleTime: 5_000,
    refetchInterval: 10_000,
  })
  const stats = overviewQuery.data
  const dailyRows = dailyQuery.data?.list || []
  const channelRows = channelsQuery.data?.list || []
  const healthRows = healthQuery.data?.list || []
  const logRows = logsQuery.data?.list || []
  const healthMap = new Map(healthRows.map(item => [item.channel_id, item]))
  const channelUsageMap = new Map((channelUsage.data?.list || []).map(row => [row.channel_id ?? 0, row]))
  const dailyByDate = new Map(dailyRows.map(row => [row.stat_date, row]))
  const trend = windowDays.map(key => dailyByDate.get(key) ?? emptyDay(key))
  const today = dailyByDate.get(windowTo)
  const yesterday = dailyByDate.get(windowDays[windowDays.length - 2])
  const todayRequests = n(today?.request_count)
  const yesterdayRequests = n(yesterday?.request_count)
  const todaySuccessRate = todayRequests ? n(today?.success_count) / todayRequests * 100 : 0
  const yesterdaySuccessRate = yesterdayRequests ? n(yesterday?.success_count) / yesterdayRequests * 100 : null
  const directionOf = (delta: string | null, current: number, previous: number | null): TrendDirection => delta === null ? 'none' : trendDirection(current, previous)
  const requestDelta = ratioChange(todayRequests, yesterdayRequests)
  const rateDelta = pointChange(todaySuccessRate, yesterdaySuccessRate)
  const tokenDelta = ratioChange(n(today?.total_tokens), n(yesterday?.total_tokens))
  const costDelta = ratioChange(n(today?.total_cost), n(yesterday?.total_cost))
  const metrics = [
    { label: '总请求量（UTC 今日）', value: compact(todayRequests), delta: requestDelta, direction: directionOf(requestDelta, todayRequests, yesterdayRequests) },
    { label: '请求成功率（UTC 今日）', value: `${todaySuccessRate.toFixed(1)}%`, delta: rateDelta, direction: directionOf(rateDelta, todaySuccessRate, yesterdaySuccessRate) },
    { label: '上游确认消耗 Tokens（UTC 今日）', value: compact(n(today?.total_tokens)), delta: tokenDelta, direction: directionOf(tokenDelta, n(today?.total_tokens), n(yesterday?.total_tokens)) },
    { label: '费用（美元 · UTC 今日）', value: `$${money(today?.total_cost)}`, delta: costDelta, direction: directionOf(costDelta, n(today?.total_cost), n(yesterday?.total_cost)) },
  ]
  const balanceTotal = channelRows.reduce((sum, channel) => sum + n(channel.balance), 0)
  const liveRows = liveQuery.data?.usage.list ?? []
  const liveRequests = liveRows.reduce((sum, row) => sum + n(row.request_count), 0)
  const liveTokens = settledLiveTokens(liveRows)
  const liveLatency = liveQuery.data?.latency
  const modelStats = modelDistribution(modelUsage.data?.list || [])
  const reset = async (channel: Channel) => { if (!window.confirm(`确认恢复渠道「${channel.name}」的熔断状态？`)) return; try { await resetChannelHealth(channel.id); await client.invalidateQueries({ queryKey: ['channel-health'] }) } catch (error) { setActionError(error instanceof Error ? error.message : '操作失败') } }
  return <>
    {actionError && <div className="error action-error">{actionError}</div>}
     <div className="metric-grid">
       <StatCard label="余额（美元）" value={`$${money(balanceTotal)}`} hint="全部渠道余额合计" />
       <StatCard label="API 密钥" value={n(stats?.active_key_count).toLocaleString()} hint="启用" />
       <section className="metric"><span>性能指标</span><div className="metric-pair"><div><strong>{liveRequests.toLocaleString()}</strong><small>RPM</small></div><div><strong>{compact(liveTokens)}</strong><small>TPM</small></div></div></section>
       <StatCard label="平均响应" value={liveLatency && liveLatency.sample_count > 0 ? `${liveLatency.average_ms} ms` : '—'} hint="TTFT · 最近 60 秒" />
     </div>
     <div className="metric-grid">{overviewQuery.error && <AsyncState loading={false} error={overviewQuery.error} hasData={Boolean(overviewQuery.data)} onRetry={() => void overviewQuery.refetch()} />}{metrics.map(metric => <TrendCard key={metric.label} {...metric} />)}</div>
     <p className="muted">消耗仅含上游确认并已结算的 {compact(n(today?.actual_tokens))} Tokens（UTC 今日）；本地估算 {compact(n(today?.estimated_tokens))} Tokens 仅作零费用审计，不计费用、配额、限流 Token 消耗或渠道余额扣减。</p>
     <div className="bento-grid">
      <Panel title="Token 使用趋势（近 14 天）" desc="仅上游确认已结算 usage · UTC 自然日汇总 · Input / Output / Cache Read / Cache Hit Rate" className="span-12"><AsyncState loading={dailyQuery.isLoading} error={dailyQuery.error} hasData={Boolean(dailyQuery.data)} onRetry={() => void dailyQuery.refetch()} />{dailyQuery.data && <TokenTrendChart points={tokenTrend(trend)} />}</Panel>
      <div className="range-bar span-12"><span className="muted filter-caption">时间范围</span>{RANGE_OPTIONS.map(item => <button key={item.key} className={`button ${item.key === rangeKey ? 'primary' : 'ghost'}`} onClick={() => applyRange(item.key)}>{item.label}</button>)}<label>开始<input type="date" value={rangeFrom} max={rangeTo} onChange={event => { setRangeFrom(event.target.value); setRangeKey('custom') }} /></label><label>结束<input type="date" value={rangeTo} min={rangeFrom} onChange={event => { setRangeTo(event.target.value); setRangeKey('custom') }} /></label></div>
      <Panel title="模型用量分布" desc={`按对外模型名聚合 · ${rangeFrom} 至 ${rangeTo}`} className="span-6">{modelStats.total > 0 ? <div className="distribution"><ModelDonut slices={modelStats.slices} total={modelStats.total} /><div className="table-wrap model-table"><table><thead><tr><th>模型</th><th>请求</th><th>消耗 Tokens</th><th>上游确认</th><th>本地估算（审计·不计费）</th><th>费用</th></tr></thead><tbody>{modelStats.slices.map(row => <tr key={row.name}><td>{row.name}</td><td className="mono">{row.requests.toLocaleString()}</td><td className="mono">{compact(row.tokens)}</td><td className="mono">{compact(row.actual)}</td><td className="mono">{compact(row.estimated)}</td><td className="mono">${money(row.cost)}</td></tr>)}</tbody></table></div></div> : <div className="empty">暂无数据</div>}</Panel>
      <Panel title="渠道健康状态" desc={`路由 · 熔断 · ${rangeFrom} 至 ${rangeTo}`} className="span-6"><AsyncState loading={channelsQuery.isLoading || healthQuery.isLoading || channelUsage.isLoading} error={channelsQuery.error || healthQuery.error || channelUsage.error} hasData={Boolean(channelsQuery.data || healthQuery.data || channelUsage.data)} onRetry={() => { void channelsQuery.refetch(); void healthQuery.refetch(); void channelUsage.refetch() }} />{(channelsQuery.data || healthQuery.data) && <div className="channel-list">{channelRows.slice(0, 6).map(channel => { const health = healthMap.get(channel.id); const open = health?.state === 'open'; const usage = channelUsageMap.get(channel.id); return <div className="channel-row" key={channel.id}><i className={`dot ${open ? 'danger' : 'ok'}`} /><b>{channel.name}</b><span className="url">{healthQuery.error ? '状态未知' : open ? '熔断中' : `${n(usage?.success_count)} 成功 / ${n(usage?.error_count)} 失败 · 消耗 ${compact(n(usage?.total_tokens))}`}</span>{open && <button className="button ghost" onClick={() => void reset(channel)}>恢复</button>}</div> })}{!channelRows.length && <div className="empty">暂无渠道</div>}</div>}</Panel>
      <Panel title="实时请求日志" desc="usage_logs 审计 Tokens（含未入账诊断，不等于消费）；partial_actual 为上游确认，partial_estimated 为本地估算·未结算，仅审计不计费" className="span-12"><AsyncState loading={logsQuery.isLoading} error={logsQuery.error} hasData={Boolean(logsQuery.data)} onRetry={() => void logsQuery.refetch()} />{logsQuery.data && <LogTable logs={logRows} />}</Panel>
    </div>
  </>
}

export function LogTable({ logs }: { logs: UsageLog[] }) { return logs.length ? <div className="table-wrap"><table><thead><tr><th>时间</th><th>模型</th><th>路由渠道</th><th>Tokens</th><th>TTFT</th><th>费用</th><th>状态</th></tr></thead><tbody>{logs.slice(0, 8).map(row => <tr key={row.id}><td className="mono">{new Date(row.created_at).toLocaleTimeString('zh-CN', { hour12: false })}</td><td><b>{row.model}</b></td><td>{row.channel_name || (row.channel_id == null ? '渠道已删除' : `#${row.channel_id}`)}</td><td className="mono">{row.total_tokens.toLocaleString()}<br /><small>{settlementLabel(row)}</small></td><td className="mono">{row.ttft_ms == null ? '—' : `${row.ttft_ms}ms`}</td><td className="mono">${row.total_cost}</td><td><span className={`badge ${row.status === 'success' ? 'ok-bg' : 'danger-bg'}`}>{row.status === 'success' ? '成功' : row.error_code || '失败'}</span></td></tr>)}</tbody></table></div> : <div className="empty">暂无请求日志</div> }
