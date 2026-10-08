import { test, expect } from 'vitest'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { listRateLimits } from '../src/api/ratelimit'
import { UsagePage } from '../src/pages/UsagePage'
import { requests, renderPage } from './support'
test('real query serialization keeps false and omits undefined', async () => {
  await listRateLimits({ page: 1, enabled: false, page_size: undefined })
  expect(requests[0].query.get('enabled')).toBe('false')
  expect(requests[0].query.has('page_size')).toBe(false)
})
test('usage date range requests a half-open next-day end boundary', async () => {
  renderPage(<UsagePage />)
  await screen.findByText('Primary')
  fireEvent.change(screen.getByLabelText('开始日期'), { target: { value: '2026-09-16' } })
  fireEvent.change(screen.getByLabelText('结束日期'), { target: { value: '2026-09-16' } })
  await userEvent.click(screen.getByRole('button', { name: '查询' }))
  await waitFor(() => expect(requests.some(r => r.path === '/admin/stats/overview' && r.query.get('end_time') === '2026-09-17T00:00:00Z')).toBe(true))
})
