import { z } from 'zod'
import { listSchema, MoneySchema } from './common'

const Counts = { request_count: z.number().int(), success_count: z.number().int(), error_count: z.number().int(), total_tokens: z.number().int(), total_cost: MoneySchema }
export const StatsSchema = z.object({ ...Counts, active_key_count: z.number().int() })
export const DailyStatsSchema = z.object({ stat_date: z.string().date(), ...Counts })
export const DailyStatsListSchema = listSchema(DailyStatsSchema)
export const ChannelStatsSchema = z.object({ channel_id: z.number().int(), channel_name: z.string(), ...Counts })
export const ChannelStatsListSchema = listSchema(ChannelStatsSchema)
export const UsageAggregateSchema = z.object({ user_id: z.number().int().nullable().optional(), api_key_id: z.number().int().nullable().optional(), channel_id: z.number().int().nullable().optional(), model: z.string(), ...Counts, duration_ms: z.number().int() })
export const UsageAggregateListSchema = listSchema(UsageAggregateSchema)
export const UsageLogSchema = z.object({
  id: z.number().int(), request_id: z.string(), user_id: z.number().int().nullable().optional(), api_key_id: z.number().int().nullable().optional(), channel_id: z.number().int().nullable().optional(),
  channel_name: z.string(), model: z.string(), upstream_model: z.string(), input_tokens: z.number().int(), output_tokens: z.number().int(), cached_input_tokens: z.number().int(), total_tokens: z.number().int(),
  unit_price_input_per_1m: MoneySchema, unit_price_output_per_1m: MoneySchema, total_cost: MoneySchema, duration_ms: z.number().int(), ttft_ms: z.number().int().nullable().optional(), status: z.string(), error_code: z.string(), client_ip: z.string(), created_at: z.string().datetime(),
})
export const UsageLogListSchema = listSchema(UsageLogSchema)
