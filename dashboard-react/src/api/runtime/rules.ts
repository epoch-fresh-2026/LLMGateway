import { z } from 'zod'
import { listSchema, MoneySchema } from './common'

export const RateLimitSchema = z.object({ id: z.number().int(), rule_name: z.string(), target_type: z.string(), target_value: z.string(), metric: z.string(), limit_value: z.number().int(), action: z.literal('reject'), priority: z.number().int(), enabled: z.boolean(), extras: z.record(z.unknown()) })
export const RateLimitListSchema = listSchema(RateLimitSchema)
export const QuotaPolicySchema = z.object({ id: z.number().int(), policy_name: z.string(), scope_type: z.enum(['user', 'api_key']), scope_id: z.number().int(), period_type: z.enum(['day', 'month']), token_limit: z.number().int().nullable(), cost_limit: MoneySchema.nullable(), enabled: z.boolean() })
export const QuotaPolicyListSchema = listSchema(QuotaPolicySchema)
export const QuotaUsageSchema = z.object({ policy_id: z.number().int(), policy_name: z.string(), scope_type: z.string(), scope_id: z.number().int(), period_type: z.string(), period_start: z.string().datetime({ offset: true }), period_end: z.string().datetime({ offset: true }), token_limit: z.number().int().nullable().optional(), used_tokens: z.number().int(), reserved_tokens: z.number().int(), cost_limit: MoneySchema.nullable().optional(), used_cost: MoneySchema, reserved_cost: MoneySchema })
export const QuotaUsageListSchema = listSchema(QuotaUsageSchema)
