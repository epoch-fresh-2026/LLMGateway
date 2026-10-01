import { z } from 'zod'
import { DeletedSchema, listSchema } from './common'
export { DeletedSchema }

export const AccountSchema = z.object({ id: z.number().int(), username: z.string(), nickname: z.string() })
export const AuthCredentialsSchema = z.object({ username: z.string().min(3).max(64), password: z.string().min(8).max(72) })
export const KeySchema = z.object({ id: z.number().int(), key_name: z.string(), prefix: z.string(), key_suffix: z.string(), is_active: z.boolean(), created_at: z.string().datetime(), last_used_at: z.string().datetime().nullable(), expires_at: z.string().datetime().nullable() })
export const KeyListSchema = listSchema(KeySchema)
export const KeySecretSchema = z.object({ id: z.number().int().optional(), full_key: z.string().min(1) })
