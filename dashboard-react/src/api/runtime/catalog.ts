import { z } from 'zod'
import { DeletedSchema, listSchema, MoneySchema } from './common'

export const ChannelSchema = z.object({
  id: z.number().int(), name: z.string(), base_url: z.string(), auth_type: z.string(),
  status: z.number().int(), weight: z.number().int(), priority: z.number().int(),
  balance: MoneySchema.nullable(), model_count: z.number().int(),
})
export const ChannelListSchema = listSchema(ChannelSchema)
export const HealthSchema = z.object({
  channel_id: z.number().int(), state: z.enum(['closed', 'open', 'half-open']),
  consecutive_failures: z.number().int(), success_count: z.number().int(), failure_count: z.number().int(),
  opened_at: z.string().datetime().nullable(), updated_at: z.string().datetime(),
})
export const HealthListSchema = listSchema(HealthSchema)
export const ChannelBreakerConfigSchema = z.object({
  channel_id: z.number().int(), window_seconds: z.number().int(), minimum_samples: z.number().int(),
  error_rate_percent: z.number().int(), timeout_rate_percent: z.number().int(), cooldown_seconds: z.number().int(),
})
export const ChannelModelSchema = z.object({ id: z.number().int(), model_name: z.string(), upstream_model: z.string(), enabled: z.boolean() })
export const ChannelModelListSchema = listSchema(ChannelModelSchema)
export const PricingSchema = z.object({
  id: z.number().int(), channel_id: z.number().int(), channel_name: z.string(), model_name: z.string(), upstream_model: z.string(),
  input_price_per_1m: MoneySchema, output_price_per_1m: MoneySchema, cached_input_price_per_1m: MoneySchema, currency: z.string(),
})
export const PricingListSchema = listSchema(PricingSchema)
export const CatalogModelSchema = z.object({
  model_name: z.string(), status: z.number().int(),
  channels: z.array(z.object({ channel_id: z.number().int(), channel_name: z.string(), upstream_model: z.string(), enabled: z.boolean() })),
})
export const CatalogModelListSchema = listSchema(CatalogModelSchema)
export const RemoteModelsSchema = z.object({ ok: z.boolean(), models: z.array(z.object({ id: z.string(), object: z.string().optional(), created: z.number().optional(), owned_by: z.string().optional() })).optional(), error: z.string().optional() })
export const ChannelTestSchema = z.object({ list: z.array(z.object({ model_alias: z.string(), upstream_model: z.string(), http_status: z.number().int(), latency_ms: z.number().int(), ok: z.boolean(), error: z.string() })) })
export { DeletedSchema }
