import { adminGet, adminSend } from './client'
import { apiPaths } from './paths'
import { AccountSchema, DeletedSchema, KeyListSchema, KeySchema, KeySecretSchema } from './runtime/accounts'
import type { Account, ClientKey, Deleted, KeyCreateInput, KeySecret, KeyUpdateInput, ListResponse, ProfileUpdateInput } from '../types/api'

export type PaginationParams = { page?: number; page_size?: number }

export const getProfile = (): Promise<Account> => adminGet(apiPaths.profile(), AccountSchema)
export const updateProfile = (input: ProfileUpdateInput): Promise<Account> => adminSend('PUT', apiPaths.profile(), input, AccountSchema)

export const listKeys = (params: PaginationParams = {}): Promise<ListResponse<ClientKey>> => adminGet(apiPaths.keys(), { page: 1, page_size: 100, ...params }, KeyListSchema)
export const createKey = (input: KeyCreateInput): Promise<KeySecret> => adminSend('POST', apiPaths.keys(), input, KeySecretSchema)
export const updateKey = (id: number, input: KeyUpdateInput): Promise<ClientKey> => adminSend('PUT', apiPaths.key(id), input, KeySchema)
export const deleteKey = (id: number): Promise<Deleted> => adminSend('DELETE', apiPaths.key(id), DeletedSchema)
