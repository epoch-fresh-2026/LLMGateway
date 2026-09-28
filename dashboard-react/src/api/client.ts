import type { AdminResponse } from '../types/api'
import { generatedClient } from './generated/client'
import type { paths } from './generated/schema'
import { z } from 'zod'

type AdminPath = Extract<keyof paths, `/admin/${string}`>
type ContractPath = AdminPath | `/admin/channels/${number}` | `/admin/channels/${number}/status` | `/admin/channels/${number}/balance` | `/admin/channels/${number}/health` | `/admin/channels/${number}/breaker` | `/admin/channels/${number}/models` | `/admin/channels/${number}/models/${number}` | `/admin/channels/${number}/remote-models` | `/admin/channels/${number}/test` | `/admin/channels/${number}/health/reset` | `/admin/keys/${number}` | `/admin/keys/${number}/reset` | `/admin/usage-logs/${number}` | `/admin/rate-limits/${number}` | `/admin/quota-policies/${number}`
export type QueryParams = Record<string, string | number | boolean | undefined>

export class ApiContractError extends Error {
  constructor(readonly path: string, readonly issues: z.ZodIssue[]) {
    super(`${path} returned an invalid API response`)
    this.name = 'ApiContractError'
  }
}

// ApiError carries the HTTP status and the backend's stable error_code so the
// presentation layer can localize failures without parsing message text.
export class ApiError extends Error {
  constructor(readonly status: number, readonly code: string | undefined, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

// AuthRequiredError marks a 401 so the session layer can redirect to login.
export class AuthRequiredError extends ApiError {
  constructor(status: number, code: string | undefined, message: string) {
    super(status, code, message)
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

type ErrorBody = Partial<AdminResponse<unknown>> & { error_code?: string }

function unwrap<T>(path: string, response: { response: Response; data?: unknown; error?: unknown }, schema?: z.ZodType<T>): T {
  const status = response.response.status
  const error = response.error as ErrorBody | undefined
  const json = response.data as ErrorBody | undefined
  const code = error?.error_code ?? json?.error_code
  if (status === 401) {
    if (typeof window !== 'undefined') window.dispatchEvent(new Event('auth:unauthorized'))
    throw new AuthRequiredError(status, code, error?.message || json?.message || 'authentication required')
  }
  const envelope = z.object({ code: z.literal(0), message: z.literal('ok'), data: z.unknown() }).safeParse(response.data)
  if (!response.response.ok || !envelope.success) {
    throw new ApiError(status, code, error?.message || json?.message || `${path} failed`)
  }
  if (!schema) return envelope.data.data as T
  const parsed = schema.safeParse(envelope.data.data)
  if (!parsed.success) throw new ApiContractError(path, parsed.error.issues)
  return parsed.data
}
