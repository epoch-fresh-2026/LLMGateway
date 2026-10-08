import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { expect, it, vi } from 'vitest'
import { http } from 'msw'
import App from '../src/App'
import { AuthProvider, useSession } from '../src/auth/AuthProvider'
import { ProfilePage } from '../src/pages/ProfilePage'
import { KeysPage } from '../src/pages/KeysPage'
import { Modal } from '../src/components/feedback/Modal'
import { AsyncState } from '../src/components/feedback/AsyncState'
import { account, fail, key, list, ok, renderPage, requests, respond, server } from './support'

it('requires a provider and clears cache on unauthorized events and logout failure', async () => {
  function SessionProbe() {
    const value = useSession()
    return <><span>{value.loading ? 'pending' : value.account?.username || 'guest'}</span><button onClick={() => value.logout().catch(() => {})}>logout</button></>
  }
  expect(() => renderPage(<SessionProbe />)).toThrow('useSession must be used within AuthProvider')
  const view = renderPage(<SessionProbe />, true)
  expect(screen.getByText('pending')).toBeInTheDocument()
  await screen.findByText('alice')
  view.client.setQueryData(['private'], 'secret')
  act(() => window.dispatchEvent(new Event('auth:unauthorized')))
  expect(screen.getByText('guest')).toBeInTheDocument(); expect(view.client.getQueryData(['private'])).toBeUndefined()
  respond('/admin/auth/logout', fail('offline'), 'POST')
  await userEvent.click(screen.getByText('logout'))
  await waitFor(() => expect(requests.some(r => r.path === '/admin/auth/logout')).toBe(true))
  expect(screen.getByText('guest')).toBeInTheDocument()
})

it('does not resolve an unmounted session request', async () => {
  let finish!: () => void
  server.use(http.get('http://localhost/admin/auth/me', async () => { await new Promise<void>(resolve => { finish = resolve }); return ok(account) }))
  const view = renderPage(<App />, true)
  await waitFor(() => expect(finish).toBeTypeOf('function'))
  view.unmount()
  await act(async () => { finish(); await new Promise(resolve => setTimeout(resolve, 20)) })
})

it('routes a guest to login, localizes failed login/register, and navigates after both successes', async () => {
  const user = userEvent.setup()
  respond('/admin/auth/me', fail('session missing', 401))
  respond('/admin/auth/login', fail('bad password', 400, 'wrong_password'), 'POST')
  respond('/admin/auth/register', fail('taken', 400, 'username_taken'), 'POST')
  renderPage(<App />, true, '/keys')
  await screen.findByRole('heading', { name: '登录控制台' })
  await user.type(screen.getByLabelText('用户名'), 'alice'); await user.type(screen.getByLabelText('密码'), 'testpassword')
  await user.click(screen.getByRole('button', { name: '登录', exact: true }))
  expect(await screen.findByRole('alert')).toHaveTextContent('密码错误')
  await user.click(screen.getByRole('button', { name: '没有账户？注册' }))
  await user.click(screen.getByRole('button', { name: '注册并登录' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('该用户名已被注册')
  respond('/admin/auth/register', account, 'POST')
  await user.click(screen.getByRole('button', { name: '注册并登录' }))
  await screen.findByRole('heading', { name: '运营仪表盘' })
  await user.click(screen.getByRole('link', { name: /我的资料/ }))
  await screen.findByRole('button', { name: '保存' })
  await user.click(screen.getByRole('button', { name: '退出登录' }))
  await screen.findByRole('heading', { name: '登录控制台' })
  await user.click(screen.getByRole('button', { name: '没有账户？注册' }))
  await user.click(screen.getByRole('button', { name: '已有账户？登录' }))
  respond('/admin/auth/login', account, 'POST')
  await user.type(screen.getByLabelText('用户名'), 'alice'); await user.type(screen.getByLabelText('密码'), 'testpassword')
  await user.click(screen.getByRole('button', { name: '登录', exact: true }))
  await screen.findByRole('heading', { name: '运营仪表盘' })
})

it('keeps loading/errors visible, retries profile, and saves nickname and password fields', async () => {
  const user = userEvent.setup()
  respond('/admin/profile', fail('profile offline'))
  renderPage(<ProfilePage />)
  expect(screen.getByText('加载中…')).toBeInTheDocument()
  await screen.findByText(/该面板暂时无法加载/)
  respond('/admin/profile', account)
  await user.click(screen.getByRole('button', { name: '重试' }))
  await screen.findByDisplayValue('alice')
  await user.type(screen.getByLabelText('昵称（留空不改）'), 'New Nick')
  await user.type(screen.getByLabelText('当前密码（改密码时必填）'), 'oldpassword')
  await user.type(screen.getByLabelText('新密码（留空不改）'), 'newpassword')
  await user.click(screen.getByRole('button', { name: '保存' }))
  await screen.findByText('资料已更新')
  expect(requests.find(r => r.method === 'PUT' && r.path === '/admin/profile')?.body).toEqual({ nickname: 'New Nick', current_password: 'oldpassword', new_password: 'newpassword' })
  respond('/admin/profile', fail('update failed'), 'PUT')
  await user.click(screen.getByRole('button', { name: '保存' }))
  await screen.findByText('update failed')
})

it('creates a key once, handles copy success/failure, cancels and deletes through real HTTP', async () => {
  const user = userEvent.setup()
  const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
  renderPage(<KeysPage />)
  await screen.findByText('production')
  await user.click(screen.getByRole('button', { name: '新建' }))
  await user.click(screen.getByRole('button', { name: '取消' }))
  await user.click(screen.getByRole('button', { name: '新建' }))
  respond('/admin/keys', fail('cannot create'), 'POST')
  await user.click(screen.getByRole('button', { name: '创建', exact: true }))
  expect(await screen.findByRole('alert')).toHaveTextContent('cannot create')
  respond('/admin/keys', { full_key: 'sk-test-only-secret' }, 'POST')
  await user.type(screen.getByLabelText('名称'), '  demo  ')
  await user.click(screen.getByRole('button', { name: '创建', exact: true }))
  const secret = await screen.findByLabelText('新创建的 API key')
  await user.click(secret)
  await user.click(screen.getByRole('button', { name: '复制', exact: true }))
  await screen.findByText('API key 已复制')
  expect(clipboard).toHaveBeenCalledWith('sk-test-only-secret')
  clipboard.mockRejectedValueOnce(new Error('clipboard denied'))
  await user.click(screen.getByRole('button', { name: '已复制', exact: true }))
  await screen.findByText('复制失败，请选中密钥后手动复制。')
  await user.click(screen.getByRole('button', { name: '关闭' }))
  expect(screen.queryByLabelText('新创建的 API key')).not.toBeInTheDocument()
  expect(requests.filter(r => r.method === 'POST' && r.path === '/admin/keys').at(-1)?.body).toEqual({ key_name: 'demo', prefix: 'sk-' })
  respond('/admin/keys/11', fail('cannot delete'), 'DELETE')
  await user.click(screen.getByRole('button', { name: '删除密钥 production' }))
  await screen.findByText('cannot delete')
  respond('/admin/keys/11', { deleted: true }, 'DELETE'); respond('/admin/keys', list([]))
  await user.click(screen.getByRole('button', { name: '删除密钥 production' }))
  await screen.findByText('暂无 Key')
})

it('retries key queries and renders nullable last usage dates', async () => {
  respond('/admin/keys', fail('offline'))
  renderPage(<KeysPage />)
  await screen.findByText(/该面板暂时无法加载/)
  respond('/admin/keys', list([{ ...key, last_used_at: null }]))
  await userEvent.click(screen.getByRole('button', { name: '重试' }))
  await screen.findByText('production'); expect(screen.getByText('—')).toBeInTheDocument()
})

it('ignores a duplicate key submit while the first request is pending', async () => {
  let finish!: () => void
  server.use(http.post('http://localhost/admin/keys', async () => {
    await new Promise<void>(resolve => { finish = resolve })
    return ok({ full_key: 'sk-test-pending' })
  }))
  renderPage(<KeysPage />)
  await screen.findByText('production')
  await userEvent.click(screen.getByRole('button', { name: '新建' }))
  const form = screen.getByRole('dialog')
  fireEvent.submit(form)
  await waitFor(() => expect(finish).toBeTypeOf('function'))
  expect(screen.getByRole('button', { name: '创建中…' })).toBeDisabled()
  fireEvent.submit(form)
  await act(async () => { finish() })
  await screen.findByLabelText('新创建的 API key')
})

it('traps modal focus, closes by escape/backdrop, submits once, and restores focus', async () => {
  const close = vi.fn(); const submit = vi.fn(); const user = userEvent.setup()
  const opener = document.createElement('button'); document.body.append(opener); opener.focus()
  const view = renderPage(<Modal title="Example" onClose={close} onSubmit={submit}><input aria-label="value" /><button type="button">last</button></Modal>)
  expect(screen.getByLabelText('value')).toHaveFocus()
  const buttons = within(screen.getByRole('dialog')).getAllByRole('button')
  buttons[0].focus(); await user.tab({ shift: true }); expect(buttons.at(-1)).toHaveFocus()
  await user.tab(); expect(buttons[0]).toHaveFocus()
  await user.click(screen.getByRole('button', { name: '保存' })); expect(submit).toHaveBeenCalledOnce()
  fireEvent.keyDown(document, { key: 'Escape' }); expect(close).toHaveBeenCalledOnce()
  fireEvent.mouseDown(screen.getByRole('dialog')); expect(close).toHaveBeenCalledOnce()
  fireEvent.mouseDown(screen.getByRole('dialog').parentElement!); expect(close).toHaveBeenCalledTimes(2)
  view.unmount(); expect(opener).toHaveFocus(); opener.remove()
})

it('distinguishes a stale panel error from initial load failure', () => {
  const view = renderPage(<AsyncState loading={false} error={new Error('offline')} hasData />)
  expect(screen.getByText(/数据更新失败，当前显示上次成功数据/)).toBeInTheDocument()
  view.rerender(<AsyncState loading={false} error={null} />)
  expect(screen.queryByText(/数据更新失败/)).not.toBeInTheDocument()
})
