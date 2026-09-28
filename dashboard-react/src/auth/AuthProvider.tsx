import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { getMe, login as apiLogin, logout as apiLogout, register as apiRegister } from '../api/auth'
import { authErrorMessage } from '../api/errorMessages'
import type { Account, AuthCredentialsInput } from '../types/api'

type SessionValue = {
  account: Account | null
  loading: boolean
  login: (input: AuthCredentialsInput) => Promise<void>
  register: (input: AuthCredentialsInput) => Promise<void>
  logout: () => Promise<void>
}

const SessionContext = createContext<SessionValue | null>(null)

// AuthProvider resolves the session cookie once on mount and reacts to 401s
// dispatched by the API transport.
export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [account, setAccount] = useState<Account | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    getMe()
      .then((me) => { if (active) setAccount(me) })
      .catch(() => { if (active) setAccount(null) })
      .finally(() => { if (active) setLoading(false) })

    const onUnauthorized = () => {
      setAccount(null)
      queryClient.clear()
    }
    window.addEventListener('auth:unauthorized', onUnauthorized)
    return () => {
      active = false
      window.removeEventListener('auth:unauthorized', onUnauthorized)
    }
  }, [queryClient])

  const login = async (input: AuthCredentialsInput) => {
    try {
      setAccount(await apiLogin(input))
    } catch (error) {
      throw new Error(authErrorMessage(error, '登录失败'))
    }
  }

  const register = async (input: AuthCredentialsInput) => {
    try {
      setAccount(await apiRegister(input))
    } catch (error) {
      throw new Error(authErrorMessage(error, '注册失败'))
    }
  }

  const logout = async () => {
    try {
      await apiLogout()
    } finally {
      setAccount(null)
      queryClient.clear()
    }
  }

  return <SessionContext.Provider value={{ account, loading, login, register, logout }}>{children}</SessionContext.Provider>
}

export function useSession() {
  const value = useContext(SessionContext)
  if (!value) throw new Error('useSession must be used within AuthProvider')
  return value
}
