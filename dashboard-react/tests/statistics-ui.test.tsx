import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { DashboardPage, LogTable } from '../src/pages/DashboardPage'
import { UsagePage } from '../src/pages/UsagePage'
import { LogsPage } from '../src/pages/LogsPage'
import { TokenTrendChart } from '../src/components/charts/TokenTrendChart'
import { aggregate, channel, counts, dailyPoint, fail, list, log, renderPage, requests, respond } from './support'

it('renders dashboard settled metrics, ranged distribution and donut hover, changes ranges, and resets breakers', async () => {
  const user = userEvent.setup(); const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
  const today = new Date().toISOString().slice(0, 10); const yesterday = new Date(Date.now() - 86400000).toISOString().slice(0, 10)
  respond('/admin/stats/daily', list([{ ...dailyPoint, stat_date: today }, { ...dailyPoint, stat_date: yesterday, request_count: 20, success_count: 10, total_tokens: 100, actual_tokens: 100, total_cost: '0.100000' }]))
  renderPage(<DashboardPage />)
  await screen.findByText('20 ms'); await screen.findByRole('img', { name: '模型用量分布' })
  expect(screen.getByText('↓ -50.0% 较昨日')).toBeInTheDocument()
  const arc = document.querySelector('.donut-arc')!
  fireEvent.mouseEnter(arc); fireEvent.mouseMove(arc, { clientX: 50, clientY: 70 })
  expect(document.querySelector('.donut-tooltip')).toHaveTextContent('gpt')
  fireEvent.mouseLeave(arc); expect(document.querySelector('.donut-tooltip')).toBeNull()
  for (const name of ['今天', '近 14 天', '近 30 天', '近 7 天']) await user.click(screen.getByRole('button', { name, exact: true }))
  fireEvent.change(screen.getByLabelText('开始'), { target: { value: '2026-10-01' } })
  fireEvent.change(screen.getByLabelText('结束'), { target: { value: '2026-10-03' } })
  await waitFor(() => expect(requests.some(r => r.query.get('date_to') === '2026-10-03')).toBe(true))
  confirm.mockReturnValueOnce(false); await user.click(screen.getByRole('button', { name: '恢复' }))
  respond('/admin/channels/2/health/reset', fail('reset failed'), 'POST')
  await user.click(screen.getByRole('button', { name: '恢复' })); await screen.findByText('reset failed')
  respond('/admin/channels/2/health/reset', { deleted: true }, 'POST')
  await user.click(screen.getByRole('button', { name: '恢复' }))
  await waitFor(() => expect(requests.filter(r => r.path === '/admin/channels/2/health/reset')).toHaveLength(2))
})

it('dashboard panels expose retries and empty model ranges never use recent logs', async () => {
  for (const path of ['/admin/stats/overview', '/admin/stats/daily', '/admin/channels', '/admin/channels/health', '/admin/usage-logs', '/admin/stats/usage']) respond(path, fail('offline'))
  renderPage(<DashboardPage />)
  await waitFor(() => expect(screen.getAllByRole('button', { name: '重试' }).length).toBeGreaterThanOrEqual(4))
  expect(screen.getByText('暂无数据')).toBeInTheDocument()
  respond('/admin/stats/overview', { ...counts, active_key_count: 1 }); respond('/admin/stats/daily', list([]))
  respond('/admin/channels', list([])); respond('/admin/channels/health', list([])); respond('/admin/stats/usage', list([])); respond('/admin/usage-logs', list([]))
  for (const button of screen.getAllByRole('button', { name: '重试' })) await userEvent.click(button)
  await screen.findByText('暂无请求日志'); await screen.findByText('暂无渠道')
  expect(screen.queryByRole('img', { name: '模型用量分布' })).not.toBeInTheDocument()
})

it('renders settled log source, deleted channel, missing TTFT and upstream errors', () => {
  renderPage(<LogTable logs={[{ ...log, channel_id: null, channel_name: '', ttft_ms: null, status: 'error', error_code: 'partial_estimated_stream_error' }, { ...log, id: 9, channel_name: '', channel_id: 99, status: 'error', error_code: '' }]} />)
  expect(screen.getByText('渠道已删除')).toBeInTheDocument(); expect(screen.getByText('#99')).toBeInTheDocument()
  expect(screen.getByText('本地估算·未结算')).toBeInTheDocument(); expect(screen.getByText('失败')).toBeInTheDocument()
})

it('queries UTC date boundaries, fills missing days, validates order, and retries every usage panel', async () => {
  const user = userEvent.setup()
  for (const path of ['/admin/stats/overview', '/admin/stats/daily', '/admin/stats/channels', '/admin/stats/usage']) respond(path, fail('offline'))
  renderPage(<UsagePage />)
  await waitFor(() => expect(screen.getAllByRole('button', { name: '重试' })).toHaveLength(5))
  respond('/admin/stats/overview', { ...counts, active_key_count: 1 }); respond('/admin/stats/daily', list([dailyPoint]))
  respond('/admin/stats/channels', list([{ ...counts, channel_id: 2, channel_name: 'Primary' }, { ...counts, channel_id: 9, channel_name: ' ' }]))
  respond('/admin/stats/usage', list([aggregate]))
  for (const button of screen.getAllByRole('button', { name: '重试' })) await user.click(button)
  await screen.findByText('Primary'); await screen.findByText('gpt')
  fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-10-09' } })
  fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-10-06' } })
  await user.click(screen.getByRole('button', { name: '查询' })); await screen.findByText('开始日期必须早于或等于结束日期')
  fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-10-07' } })
  fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-10-08' } })
  await user.click(screen.getByRole('button', { name: '查询' }))
  await waitFor(() => expect(requests.some(r => r.path === '/admin/stats/overview' && r.query.get('end_time') === '2026-10-09T00:00:00Z')).toBe(true))
})

it('filters/paginates logs, validates time ranges, loads details, refreshes and resets', async () => {
  const user = userEvent.setup()
  renderPage(<LogsPage />)
  await screen.findByText('req-7')
  await user.click(screen.getByRole('button', { name: '下一页' }))
  await waitFor(() => expect(requests.some(r => r.path === '/admin/usage-logs' && r.query.get('page') === '2')).toBe(true))
  await user.click(screen.getByRole('button', { name: '上一页' }))
  await user.type(screen.getByPlaceholderText('用户 ID'), '1'); await user.type(screen.getByPlaceholderText('渠道 ID'), '2'); await user.type(screen.getByPlaceholderText('模型'), 'gpt')
  await user.selectOptions(screen.getByRole('combobox'), 'error')
  await user.click(screen.getByRole('button', { name: '查询' }))
  await waitFor(() => expect(requests.some(r => r.query.get('user_id') === '1' && r.query.get('status') === 'error')).toBe(true))
  for (const name of ['今天', '最近 24 小时', '最近 7 天']) await user.click(screen.getByRole('button', { name, exact: true }))
  fireEvent.change(screen.getByLabelText('开始时间'), { target: { value: '2026-10-09T00:00' } }); fireEvent.change(screen.getByLabelText('结束时间'), { target: { value: '2026-10-08T00:00' } })
  await user.click(screen.getByRole('button', { name: '查询' })); await screen.findByText('开始时间必须早于结束时间')
  await user.click(screen.getByRole('button', { name: '重置' })); await user.click(screen.getByRole('button', { name: '刷新' }))
  respond('/admin/usage-logs/7', fail('detail offline'))
  await user.click(screen.getByRole('button', { name: '查看' })); await screen.findByText('请求详情加载失败')
  respond('/admin/usage-logs/7', log)
  await user.click(screen.getByRole('button', { name: '查看' }))
  expect(await screen.findByRole('dialog')).toHaveTextContent('gpt / gpt-real')
  await user.click(screen.getByRole('button', { name: '取消' }))
  respond('/admin/usage-logs', list([])); await user.click(screen.getByRole('button', { name: '刷新' })); await screen.findByText('当前筛选条件下暂无日志')
  respond('/admin/usage-logs', fail('offline')); await user.click(screen.getByRole('button', { name: '刷新' })); await screen.findByText('加载失败，请重试。')
})

it('renders real Recharts axes and tooltip, toggles every series, and handles no points', async () => {
  const user = userEvent.setup()
  const view = renderPage(<TokenTrendChart points={[{ date: '2026-10-07', input: 1000, output: 100, cacheRead: 250, cacheHitRate: 25 }, { date: '2026-10-08', input: 2000, output: 500, cacheRead: 1000, cacheHitRate: 50 }]} />)
  await waitFor(() => expect(document.querySelector('.recharts-surface')).toBeInTheDocument())
  fireEvent.mouseMove(document.querySelector('.recharts-wrapper')!, { clientX: 200, clientY: 100 })
  await waitFor(() => expect(document.querySelector('.token-tooltip')).toBeInTheDocument())
  expect(document.querySelector('.token-tooltip')).toHaveTextContent('Cache Hit Rate')
  for (const name of ['Input', 'Output', 'Cache Read', 'Cache Hit Rate']) {
    await user.click(screen.getByRole('button', { name, exact: true }))
    expect(screen.getByRole('button', { name, exact: true })).toHaveClass('off')
    await user.click(screen.getByRole('button', { name, exact: true }))
  }
  view.rerender(<TokenTrendChart points={[]} />)
  expect(screen.getByText('暂无数据')).toBeInTheDocument()
})
