import { adminGet, adminSend } from './client'
import { apiPaths } from './paths'
import { DeletedSchema } from './runtime/common'
import { QuotaPolicyListSchema, QuotaPolicySchema, QuotaUsageListSchema } from './runtime/rules'
import type { QuotaPolicyCreateInput } from '../types/api'

export type ListQuotaParams = { page?: number; page_size?: number; scope_type?: 'user' | 'api_key'; scope_id?: number; enabled?: boolean }

export const listQuotaPolicies = (params: ListQuotaParams = {}) => adminGet(apiPaths.quotaPolicies(), { page: 1, page_size: 100, ...params }, QuotaPolicyListSchema)
export const listQuotaUsage = (params: ListQuotaParams = {}) => adminGet(apiPaths.quotaUsage(), { page: 1, page_size: 100, ...params }, QuotaUsageListSchema)
export const createQuotaPolicy = (input: QuotaPolicyCreateInput) => adminSend('POST', apiPaths.quotaPolicies(), input, QuotaPolicySchema)
export const deleteQuotaPolicy = (id: number) => adminSend('DELETE', apiPaths.quotaPolicy(id), DeletedSchema)
