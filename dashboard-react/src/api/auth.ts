import { z } from 'zod'
import { adminGet, adminSend } from './client'
import { apiPaths } from './paths'
import { AccountSchema } from './runtime/accounts'
import type { Account, AuthCredentialsInput } from '../types/api'

export const getMe = () => adminGet(apiPaths.authMe(), AccountSchema)
export const login = (input: AuthCredentialsInput) => adminSend('POST', apiPaths.authLogin(), input, AccountSchema)
export const register = (input: AuthCredentialsInput) => adminSend('POST', apiPaths.authRegister(), input, AccountSchema)
export const logout = () => adminSend('POST', apiPaths.authLogout(), z.unknown())
