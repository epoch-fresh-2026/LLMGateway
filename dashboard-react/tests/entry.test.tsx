import { act, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { fail, respond } from './http-support'

it('mounts the actual browser entry under StrictMode with session and query providers', async () => {
  respond('/admin/auth/me', fail('missing', 401))
  const root = document.createElement('div'); root.id = 'root'; document.body.append(root)
  await act(async () => { await import('../src/main') })
  expect(await screen.findByRole('heading', { name: '登录控制台' })).toBeInTheDocument()
  root.remove()
})
