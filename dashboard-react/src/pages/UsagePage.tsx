import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { daily as dailyAPI, overview, usageStats } from '../api/usage'
import { channelStats } from '../api/usage'
import { AsyncState } from '../components/feedback/AsyncState'
import { TokenTrendChart } from '../components/charts/TokenTrendChart'
import { compact, tokenTrend } from './dashboardMetrics'
import type { ChannelStats, DailyStats, ListResponse, Stats, UsageAggregate } from '../types/api'

const day = (date: Date) => date.toISOString().slice(0, 10)
const num = (value: unknown) => Number(value || 0)
const emptyDay = (date: string): DailyStats => ({ stat_date: date, request_count: 0, success_count: 0, error_count: 0, total_tokens: 0, actual_tokens: 0, estimated_tokens: 0, input_tokens: 0, output_tokens: 0, cached_input_tokens: 0, total_cost: '0.000000' })
const nextDay = (date: string) => { const value = new Date(`${date}T00:00:00Z`); value.setUTCDate(value.getUTCDate() + 1); return value.toISOString().slice(0, 10) }

function fillDays(from: string, to: string, source: DailyStats[]) {
  const values = new Map(source.map(row => [row.stat_date, row]))
  const result: DailyStats[] = []
  for (const cursor = new Date(`${from}T00:00:00`); cursor <= new Date(`${to}T00:00:00`); cursor.setDate(cursor.getDate() + 1)) result.push(values.get(day(cursor)) || emptyDay(day(cursor)))
  return result
}

function PanelState({ query, children }: { query: { isLoading: boolean; error: unknown; data?: unknown; refetch: () => unknown }; children: React.ReactNode }) {
  return <><AsyncState loading={query.isLoading} error={query.error} hasData={Boolean(query.data)} onRetry={() => void query.refetch()} />{query.data && children}</>
}

export function UsagePage() {
  const today = new Date()
  const first = new Date(today)
  first.setDate(first.getDate() - 6)
  const initial = { from: day(first), to: day(today) }
  const [range, setRange] = useState(initial)
  const [active, setActive] = useState(initial)
  const [error, setError] = useState('')
  const daily = useQuery({ queryKey: ['usage-daily', active], queryFn: () => dailyAPI({ date_from: active.from, date_to: active.to, page: 1, page_size: 100 }), staleTime: 30_000 })
  const summary = useQuery({ queryKey: ['usage-overview', active], queryFn: () => overview({ start_time: `${active.from}T00:00:00Z`, end_time: `${nextDay(active.to)}T00:00:00Z` }), staleTime: 30_000 })
  const channels = useQuery({ queryKey: ['usage-channels', active], queryFn: () => channelStats({ start_time: `${active.from}T00:00:00Z`, end_time: `${nextDay(active.to)}T00:00:00Z` }), staleTime: 30_000 })
  const models = useQuery({ queryKey: ['usage-models', active], queryFn: () => usageStats({ group_by: 'model', date_from: active.from, date_to: active.to, page: 1, page_size: 100 }), staleTime: 30_000 })
  const rows = useMemo(() => fillDays(active.from, active.to, daily.data?.list || []), [active, daily.data])
  const total = num(summary.data?.request_count)
  const success = num(summary.data?.success_count)
  const tokens = num(summary.data?.total_tokens)
  const cost = num(summary.data?.total_cost)
  const submit = () => { if (range.from > range.to) setError('开始日期必须早于或等于结束日期'); else { setError(''); setActive(range) } }

  return <div className="bento-grid">
    <div className="metric-grid"><AsyncState loading={summary.isLoading} error={summary.error} hasData={Boolean(summary.data)} onRetry={() => void summary.refetch()} />{summary.data && [['请求总量', compact(total)], ['成功率', total ? `${(success / total * 100).toFixed(1)}%` : '0.0%'], ['消耗 Tokens（上游确认·已结算）', compact(tokens)], ['上游确认 Tokens', compact(num(summary.data.actual_tokens))], ['本地估算 Tokens（审计·不计费）', compact(num(summary.data.estimated_tokens))], ['费用（美元）', `$${cost.toFixed(2)}`]].map(([label, value]) => <div className="metric" key={label}><span>{label}</span><strong>{value}</strong><small>{active.from} 至 {active.to}</small></div>)}</div>
    <section className="panel span-8"><div className="panel-head"><div><h3>Token 使用趋势</h3><p className="muted">消耗仅含上游确认并已结算的 usage；本地估算仅作零费用审计，不计费用、配额、限流 Token 消耗或渠道余额扣减 · Input / Output / Cache Read / Cache Hit Rate</p></div></div><PanelState query={daily}><TokenTrendChart points={tokenTrend(rows)} /></PanelState></section>
    <section className="panel span-4"><div className="panel-head"><div><h3>模型用量分布</h3><p className="muted">当前日期范围 · 按模型聚合</p></div></div><PanelState query={models}><div className="distribution-list">{models.data?.list.map((row: UsageAggregate) => <div key={row.model}><span>{row.model || 'unknown'}</span><b>{compact(num(row.total_tokens))}<small> 上游确认 {compact(row.actual_tokens)}；本地估算审计·不计费 {compact(row.estimated_tokens)}</small></b></div>)}</div></PanelState></section>
    <section className="panel span-12"><div className="panel-head"><div><h3>日期范围</h3><p className="muted">按统计自然日查询用户日汇总与渠道成本</p></div></div><div className="date-filter"><label><span>开始日期</span><input type="date" value={range.from} onChange={event => setRange({ ...range, from: event.target.value })} /></label><label><span>结束日期</span><input type="date" value={range.to} onChange={event => setRange({ ...range, to: event.target.value })} /></label><button className="button primary" onClick={submit}>查询</button>{error && <span className="error">{error}</span>}</div></section>
    <section className="panel span-7"><div className="panel-head"><div><h3>每日明细</h3><p className="muted">仅上游确认已结算消耗 · UTC 自然日聚合</p></div></div><PanelState query={daily}><div className="table-wrap"><table><thead><tr><th>日期</th><th>请求</th><th>成功</th><th>失败</th><th>消耗 Tokens</th><th>上游确认</th><th>本地估算（审计·不计费）</th><th>费用</th></tr></thead><tbody>{rows.map(row => <tr key={row.stat_date}><td className="mono">{row.stat_date}</td><td>{row.request_count}</td><td>{row.success_count}</td><td>{row.error_count}</td><td>{compact(num(row.total_tokens))}</td><td>{compact(row.actual_tokens)}</td><td>{compact(row.estimated_tokens)}</td><td>{row.total_cost}</td></tr>)}</tbody></table></div></PanelState></section>
    <section className="panel span-5"><div className="panel-head"><div><h3>渠道统计</h3><p className="muted">渠道请求与费用</p></div></div><PanelState query={channels}><div className="channel-list">{channels.data?.list.filter(row => row.channel_name.trim()).map(row => <div className="channel-row" key={row.channel_id}><i className="dot ok" /><b>{row.channel_name}</b><span className="url">{row.request_count} 请求 · {compact(num(row.total_tokens))} 消耗 tokens · 上游确认 {compact(row.actual_tokens)}；本地估算审计·不计费 {compact(row.estimated_tokens)}</span><em>${row.total_cost}</em></div>)}</div></PanelState></section>
  </div>
}
