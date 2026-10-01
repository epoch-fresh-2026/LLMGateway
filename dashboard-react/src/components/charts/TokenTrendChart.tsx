import { useState } from 'react'
import { Area, CartesianGrid, ComposedChart, Line, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { compact, type TokenTrendPoint } from '../../pages/dashboardMetrics'

type SeriesKey = 'input' | 'output' | 'cacheRead' | 'cacheHitRate'

const SERIES: { key: SeriesKey; label: string; color: string; rate: boolean }[] = [
  { key: 'input', label: 'Input', color: '#3b82f6', rate: false },
  { key: 'output', label: 'Output', color: '#34d399', rate: false },
  { key: 'cacheRead', label: 'Cache Read', color: '#22d3ee', rate: false },
  { key: 'cacheHitRate', label: 'Cache Hit Rate', color: '#a78bfa', rate: true },
]

const CHART_HEIGHT = 280

// Recharts injects active/payload/label into the custom tooltip content.
function TrendTooltip({ active, label, payload, visible }: { active?: boolean; label?: string; payload?: { payload: TokenTrendPoint }[]; visible: Record<SeriesKey, boolean> }) {
  const point = payload?.[0]?.payload
  if (!active || !point) return null
  return <div className="token-tooltip"><b>{label}</b>{SERIES.filter(series => visible[series.key]).map(series => <span key={series.key}><i style={{ background: series.color }} />{series.label}：{series.rate ? `${point.cacheHitRate.toFixed(1)}%` : compact(point[series.key])}</span>)}</div>
}

export function TokenTrendChart({ points }: { points: TokenTrendPoint[] }) {
  const [visible, setVisible] = useState<Record<SeriesKey, boolean>>({ input: true, output: true, cacheRead: true, cacheHitRate: true })
  if (points.length === 0) return <div className="empty">暂无数据</div>

  return <div className="token-trend">
    <div className="token-legend">{SERIES.map(series => <button type="button" key={series.key} className={`token-legend-item${visible[series.key] ? '' : ' off'}`} onClick={() => setVisible(current => ({ ...current, [series.key]: !current[series.key] }))}><i style={{ background: series.color }} />{series.label}</button>)}</div>
    <ResponsiveContainer width="100%" height={CHART_HEIGHT}>
      <ComposedChart data={points} margin={{ top: 10, right: 4, bottom: 0, left: 0 }}>
        <defs>{SERIES.filter(series => !series.rate).map(series => <linearGradient key={series.key} id={`token-grad-${series.key}`} x1="0" y1="0" x2="0" y2="1"><stop offset="5%" stopColor={series.color} stopOpacity={0.35} /><stop offset="95%" stopColor={series.color} stopOpacity={0} /></linearGradient>)}</defs>
        <CartesianGrid stroke="#27272a" vertical={false} />
        <XAxis dataKey="date" tickFormatter={value => String(value).slice(5)} tick={{ fill: '#71717a', fontSize: 10 }} axisLine={{ stroke: '#27272a' }} tickLine={false} minTickGap={24} />
        <YAxis yAxisId="tokens" tickFormatter={value => compact(Number(value))} tick={{ fill: '#71717a', fontSize: 10 }} axisLine={false} tickLine={false} width={52} />
        {visible.cacheHitRate && <YAxis yAxisId="rate" orientation="right" domain={[0, 100]} tickFormatter={value => `${value}%`} tick={{ fill: '#71717a', fontSize: 10 }} axisLine={false} tickLine={false} width={44} />}
        <Tooltip content={<TrendTooltip visible={visible} />} cursor={{ stroke: '#52525b', strokeDasharray: '3 3' }} />
        {SERIES.filter(series => !series.rate && visible[series.key]).map(series => <Area key={series.key} yAxisId="tokens" type="monotone" dataKey={series.key} name={series.label} stroke={series.color} strokeWidth={2} fill={`url(#token-grad-${series.key})`} dot={false} activeDot={false} isAnimationActive={false} />)}
        {visible.cacheHitRate && <Line yAxisId="rate" type="monotone" dataKey="cacheHitRate" name="Cache Hit Rate" stroke="#a78bfa" strokeWidth={2} strokeDasharray="6 5" dot={{ r: 3, fill: '#a78bfa', strokeWidth: 0 }} activeDot={{ r: 4 }} isAnimationActive={false} />}
      </ComposedChart>
    </ResponsiveContainer>
  </div>
}
