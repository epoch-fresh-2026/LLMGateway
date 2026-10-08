import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { ReactNode } from 'react'
import { AuthProvider } from '../src/auth/AuthProvider'
export * from './http-support'

export function renderPage(node: ReactNode, session = false, route = '/') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 }, mutations: { retry: false } } })
  const view = render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[route]}>{session ? <AuthProvider>{node}</AuthProvider> : node}</MemoryRouter></QueryClientProvider>)
  return { ...view, client }
}
