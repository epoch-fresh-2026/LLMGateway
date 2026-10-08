import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it } from 'vitest'
import { QuotasPage } from '../src/pages/ConsolePages'
import { fail, list, policy, quotaUsage, renderPage, requests, respond } from './support'

it('matches bucket policy IDs with exact money and displays zero when a policy has no bucket', async () => {
  respond('/admin/quota-policies', list([{ ...policy, id: 1, policy_name: 'daily', token_limit: 100 }, { ...policy, id: 2, policy_name: 'monthly', period_type: 'month', token_limit: null, cost_limit: '2.000000' }, { ...policy, id: 3, policy_name: 'no bucket' }]))
  respond('/admin/quota-usage', list([{ ...quotaUsage, policy_id: 2, used_tokens: 42, used_cost: '9007199254740993.123456' }, { ...quotaUsage, policy_id: 1, used_tokens: 7, used_cost: '0.000001' }]))
  renderPage(<QuotasPage />, true)
  await screen.findByText('monthly')
  const daily = screen.getByText('daily').closest('tr')!
  expect(within(daily).getByText('7')).toBeInTheDocument(); expect(within(daily).getByText('0.000001')).toBeInTheDocument()
  const monthly = screen.getByText('monthly').closest('tr')!
  expect(within(monthly).getByText('42')).toBeInTheDocument(); expect(within(monthly).getByText('9007199254740993.123456')).toBeInTheDocument()
  const empty = screen.getByText('no bucket').closest('tr')!
  expect(within(empty).getByText('0')).toBeInTheDocument(); expect(within(empty).getByText('0.000000')).toBeInTheDocument()
  expect(screen.queryByText('999')).not.toBeInTheDocument(); expect(screen.queryByText('888.000000')).not.toBeInTheDocument()
})

it('uses compact token formatting and preserves null and zero limits', async () => {
  respond('/admin/quota-policies', list([{ ...policy, id: 1, policy_name: 'small', token_limit: 1500, cost_limit: null }, { ...policy, id: 2, policy_name: 'large', token_limit: 2500000, cost_limit: '2.000000' }, { ...policy, id: 3, policy_name: 'zero', token_limit: 0, cost_limit: '0.000000' }]))
  respond('/admin/quota-usage', list([{ ...quotaUsage, policy_id: 1, used_tokens: 1200000, used_cost: '0.123456' }, { ...quotaUsage, policy_id: 2, used_tokens: 2300, used_cost: '9007199254740993.123456' }]))
  renderPage(<QuotasPage />, true)
  await screen.findByText('large')
  expect(screen.getByText('1.5K')).toBeInTheDocument(); expect(screen.getByText('1.2M')).toBeInTheDocument()
  expect(screen.getByText('2.5M')).toBeInTheDocument(); expect(screen.getByText('2.3K')).toBeInTheDocument()
  expect(within(screen.getByText('small').closest('tr')!).getByText('—')).toBeInTheDocument()
  expect(within(screen.getByText('zero').closest('tr')!).getAllByText('0')).toHaveLength(2)
})

it.each(['/admin/quota-policies', '/admin/quota-usage'])('failed %s refetch hides stale quota rows and retry refreshes both queries', async path => {
  renderPage(<QuotasPage />, true)
  await screen.findByText('daily cap')
  const before = requests.filter(r => r.path === '/admin/quota-policies' || r.path === '/admin/quota-usage').length
  respond(path, fail('quota refresh failed'))
  await userEvent.click(screen.getByRole('button', { name: '刷新' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('quota refresh failed')
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  await waitFor(() => expect(requests.filter(r => r.path === '/admin/quota-policies' || r.path === '/admin/quota-usage').length).toBe(before + 2))
})
