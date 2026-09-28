import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { listChannels, listChannelHealth, resetChannelHealth } from '../api/catalog'
import { daily, logs, overview, ttft, usageStats } from '../api/usage'
import { AsyncState } from '../components/feedback/AsyncState'
import { pointChange, ratioChange, recentUtcDays, trendDirection, type TrendDirection } from './dashboardMetrics'
import type { Channel, DailyStats, UsageLog } from '../types/api'

const n = (value: unknown) => Number(value || 0)
const money = (value: unknown) => Number(value || 0).toFixed(2)
const compact = (value: number) => value >= 1_000_000 ? `${(value / 1_000_000).toFixed(1)}M` : value >= 1_000 ? `${(value / 1_000).toFixed(1)}K` : value.toLocaleString()
const TREND_DAYS = 14
const LIVE_WINDOW_MS = 60_000
const emptyDay = (key: string): DailyStats => ({ stat_date: key, request_count: 0, success_count: 0, error_count: 0, total_tokens: 0, total_cost: '0' })

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

export function DashboardPage() {
  const client = useQueryClient(); const [actionError, setActionError] = useState('')
  const windowDays = recentUtcDays(TREND_DAYS); const windowFrom = windowDays[0]; const windowTo = windowDays[windowDays.length - 1]
  const overviewQuery = useQuery({ queryKey: ['overview'], queryFn: () => overview(), staleTime: 30_000 })
  const dailyQuery = useQuery({ queryKey: ['daily', windowFrom, windowTo], queryFn: () => daily({ date_from: windowFrom, date_to: windowTo }), staleTime: 30_000 })
  const channelsQuery = useQuery({ queryKey: ['channels'], queryFn: () => listChannels(), staleTime: 60_000 })
  const healthQuery = useQuery({ queryKey: ['channel-health'], queryFn: listChannelHealth, staleTime: 30_000 })
  const logsQuery = useQuery({ queryKey: ['logs'], queryFn: () => logs(), staleTime: 10_000 })
  const modelUsage = useQuery({ queryKey: ['model-usage'], queryFn: () => usageStats({ group_by: 'model', page: 1, page_size: 100 }) })
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
  const successRate = stats ? stats.success_count / Math.max(stats.request_count, 1) * 100 : 0
  const dailyByDate = new Map(dailyRows.map(row => [row.stat_date, row]))
  const trend = windowDays.map(key => dailyByDate.get(key) ?? emptyDay(key))
  const max = Math.max(1, ...trend.map(row => n(row.request_count)))
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
    { label: '消耗 Tokens（UTC 今日）', value: compact(n(today?.total_tokens)), delta: tokenDelta, direction: directionOf(tokenDelta, n(today?.total_tokens), n(yesterday?.total_tokens)) },
    { label: '费用（美元 · UTC 今日）', value: `$${money(today?.total_cost)}`, delta: costDelta, direction: directionOf(costDelta, n(today?.total_cost), n(yesterday?.total_cost)) },
  ]
  const balanceTotal = channelRows.reduce((sum, channel) => sum + n(channel.balance), 0)
  const liveRows = liveQuery.data?.usage.list ?? []
  const liveRequests = liveRows.reduce((sum, row) => sum + n(row.request_count), 0)
  const liveTokens = liveRows.reduce((sum, row) => sum + n(row.total_tokens), 0)
  const liveLatency = liveQuery.data?.latency
  const dist = new Map<string, number>()
  logRows.forEach(row => dist.set(row.model || 'unknown', (dist.get(row.model || 'unknown') || 0) + row.total_tokens))
  const modelRows = modelUsage.data?.list?.length ? modelUsage.data.list.map(row => ({ name: row.model || 'unknown', value: n(row.total_tokens || row.request_count) })) : [...dist.entries()].map(([name, value]) => ({ name, value }))
  const modelTotal = modelRows.reduce((sum, row) => sum + row.value, 0)
  const modelScale = Math.max(1, modelTotal)
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
    <div className="bento-grid">
      <Panel title="请求趋势（近 14 天）" desc="按 user_daily_stats 自然日汇总 · 含成功 / 失败" className="span-12"><AsyncState loading={dailyQuery.isLoading} error={dailyQuery.error} hasData={Boolean(dailyQuery.data)} onRetry={() => void dailyQuery.refetch()} />{dailyQuery.data && <><div className="trend-meta"><span>今日 <b>{n(trend.at(-1)?.request_count).toLocaleString()}</b> 次</span><span>成功率 <b>{successRate.toFixed(1)}%</b></span><span>错误 <b>{(stats?.error_count || 0).toLocaleString()}</b> 次</span></div><div className="chart trend-chart">{trend.map((row, index) => <div className="bar" key={String(row.stat_date || index)}><i style={{ '--h': `${n(row.request_count) / max * 100}%` } as React.CSSProperties} /><small>{String(row.stat_date || '').slice(5)}</small></div>)}</div></>}</Panel>
      <Panel title="模型用量分布" desc="按对外模型名聚合 · tokens" className="span-6">{modelTotal > 0 ? <div className="distribution"><div className="donut"><strong>{compact(modelTotal)}</strong><small>tokens</small></div><div className="distribution-list">{modelRows.slice(0, 6).map(row => <div key={row.name}><span>{row.name}</span><b>{(row.value / modelScale * 100).toFixed(1)}%</b></div>)}</div></div> : <div className="empty">暂无数据</div>}</Panel>
      <Panel title="渠道健康状态" desc="路由 · 熔断 · 成功率" className="span-6"><AsyncState loading={channelsQuery.isLoading || healthQuery.isLoading} error={channelsQuery.error || healthQuery.error} hasData={Boolean(channelsQuery.data || healthQuery.data)} onRetry={() => { void channelsQuery.refetch(); void healthQuery.refetch() }} />{(channelsQuery.data || healthQuery.data) && <div className="channel-list">{channelRows.slice(0, 6).map(channel => { const item = healthMap.get(channel.id); const open = item?.state === 'open'; return <div className="channel-row" key={channel.id}><i className={`dot ${open ? 'danger' : 'ok'}`} /><b>{channel.name}</b><span className="url">{healthQuery.error ? '状态未知' : open ? '熔断中' : `${item?.success_count || 0} 成功 / ${item?.failure_count || 0} 失败`}</span>{open && <button className="button ghost" onClick={() => void reset(channel)}>恢复</button>}</div> })}{!channelRows.length && <div className="empty">暂无渠道</div>}</div>}</Panel>
      <Panel title="实时请求日志" desc="usage_logs · 预冻结 → 按实际 usage 结算" className="span-12"><AsyncState loading={logsQuery.isLoading} error={logsQuery.error} hasData={Boolean(logsQuery.data)} onRetry={() => void logsQuery.refetch()} />{logsQuery.data && <LogTable logs={logRows} />}</Panel>
    </div>
  </>
}

export function LogTable({ logs }: { logs: UsageLog[] }) { return logs.length ? <div className="table-wrap"><table><thead><tr><th>时间</th><th>模型</th><th>路由渠道</th><th>Tokens</th><th>TTFT</th><th>费用</th><th>状态</th></tr></thead><tbody>{logs.slice(0, 8).map(row => <tr key={row.id}><td className="mono">{new Date(row.created_at).toLocaleTimeString('zh-CN', { hour12: false })}</td><td><b>{row.model}</b></td><td>{row.channel_name || (row.channel_id == null ? '渠道已删除' : `#${row.channel_id}`)}</td><td className="mono">{row.total_tokens.toLocaleString()}</td><td className="mono">{row.ttft_ms == null ? '—' : `${row.ttft_ms}ms`}</td><td className="mono">${row.total_cost}</td><td><span className={`badge ${row.status === 'success' ? 'ok-bg' : 'danger-bg'}`}>{row.status === 'success' ? '成功' : row.error_code || '失败'}</span></td></tr>)}</tbody></table></div> : <div className="empty">暂无请求日志</div> }
