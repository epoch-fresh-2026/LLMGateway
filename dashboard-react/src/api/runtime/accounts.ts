import { z } from 'zod'
import { BalanceSchema, DeletedSchema, listSchema, MoneySchema } from './common'
export { BalanceSchema }

export const AccountSchema = z.object({ id: z.number().int(), username: z.string(), nickname: z.string() })
export const AuthCredentialsSchema = z.object({ username: z.string().min(3).max(64), password: z.string().min(8).max(72) })
export const UserSchema = z.object({ id: z.number().int(), nickname: z.string(), user_group: z.string(), status: z.enum(['active', 'suspended']), balance: BalanceSchema })
export const UserListSchema = listSchema(UserSchema)
export const BalanceTransactionSchema = z.object({ id: z.number().int(), tx_type: z.string(), amount: MoneySchema, balance_after: MoneySchema, created_at: z.string().datetime() })
export const BalanceTransactionListSchema = listSchema(BalanceTransactionSchema)
export const KeySchema = z.object({ id: z.number().int(), user_id: z.number().int(), key_name: z.string(), prefix: z.string(), is_active: z.boolean(), last_used_at: z.string().datetime().nullable(), expires_at: z.string().datetime().nullable() })
export const KeyListSchema = listSchema(KeySchema)
export const KeySecretSchema = z.object({ id: z.number().int().optional(), full_key: z.string().min(1) })
export const BalanceAfterSchema = z.object({ balance_after: MoneySchema })
export { DeletedSchema }
