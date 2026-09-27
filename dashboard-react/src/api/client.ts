import type { AdminResponse } from '../types/api'
import { generatedClient } from './generated/client'
import type { paths } from './generated/schema'
import { z } from 'zod'

type AdminPath = Extract<keyof paths, `/admin/${string}`>
type ContractPath = AdminPath | `/admin/channels/${number}` | `/admin/channels/${number}/status` | `/admin/channels/${number}/balance` | `/admin/channels/${number}/health` | `/admin/channels/${number}/breaker` | `/admin/channels/${number}/models` | `/admin/channels/${number}/models/${number}` | `/admin/channels/${number}/remote-models` | `/admin/channels/${number}/test` | `/admin/channels/${number}/health/reset` | `/admin/users/${number}` | `/admin/users/${number}/status` | `/admin/users/${number}/balance` | `/admin/users/${number}/recharge` | `/admin/users/${number}/balance-transactions` | `/admin/users/${number}/keys` | `/admin/users/${number}/keys/${number}` | `/admin/users/${number}/keys/${number}/reset` | `/admin/usage-logs/${number}` | `/admin/rate-limits/${number}` | `/admin/quota-policies/${number}`
export type QueryParams = Record<string, string | number | boolean | undefined>

export class ApiContractError extends Error {
  constructor(readonly path: string, readonly issues: z.ZodIssue[]) {
    super(`${path} returned an invalid API response`)
    this.name = 'ApiContractError'
  }
}

// AuthRequiredError marks a 401 so the session layer can redirect to login.
export class AuthRequiredError extends Error {
  constructor() {
    super('authentication required')
    this.name = 'AuthRequiredError'
  }
}

export async function adminGet<T>(path: ContractPath, params: QueryParams, schema: z.ZodType<T>): Promise<T>
export async function adminGet<T>(path: ContractPath, schema: z.ZodType<T>): Promise<T>
export async function adminGet<T>(path: ContractPath, paramsOrSchema: QueryParams | z.ZodType<T>, maybeSchema?: z.ZodType<T>): Promise<T> {
  const params = paramsOrSchema instanceof z.ZodType ? {} : paramsOrSchema
  const schema = paramsOrSchema instanceof z.ZodType ? paramsOrSchema : maybeSchema
  // Domain API modules use concrete URLs; openapi-fetch normally receives path templates.
  const response = await generatedClient.request('get' as never, path as never, { params: { query: params }, credentials: 'include' } as never)
  return unwrap<T>(path, response, schema)
}
export async function adminSend<T, B extends object = object>(method: string, path: ContractPath, body: B, schema: z.ZodType<T>): Promise<T>
export async function adminSend<T>(method: string, path: ContractPath, schema: z.ZodType<T>): Promise<T>
export async function adminSend<T>(method: string, path: ContractPath, bodyOrSchema: object | z.ZodType<T>, maybeSchema?: z.ZodType<T>): Promise<T> {
  const body = bodyOrSchema instanceof z.ZodType ? undefined : bodyOrSchema
  const schema = bodyOrSchema instanceof z.ZodType ? bodyOrSchema : maybeSchema
  const response = await generatedClient.request(method.toLowerCase() as never, path as never, { body, credentials: 'include' } as never)
  return unwrap<T>(path, response, schema)
}

function unwrap<T>(path: string, response: { response: Response; data?: unknown; error?: unknown }, schema?: z.ZodType<T>): T {
  if (response.response.status === 401) {
    if (typeof window !== 'undefined') window.dispatchEvent(new Event('auth:unauthorized'))
    throw new AuthRequiredError()
  }
  const envelope = z.object({ code: z.literal(0), message: z.literal('ok'), data: z.unknown() }).safeParse(response.data)
  if (!response.response.ok || !envelope.success) {
    const error = response.error as AdminResponse<unknown> | undefined
    const json = response.data as Partial<AdminResponse<unknown>> | undefined
    throw new Error(error?.message || json?.message || `${path} failed`)
  }
  if (!schema) return envelope.data.data as T
  const parsed = schema.safeParse(envelope.data.data)
  if (!parsed.success) throw new ApiContractError(path, parsed.error.issues)
  return parsed.data
}
