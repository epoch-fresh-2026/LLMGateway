import type { components } from '../api/generated/schema'

export type AdminResponse<T> = { code: number; message: string; data: T }
export type ListResponse<T> = { list: T[]; total: number }

export type Account = components['schemas']['Account']
export type AuthCredentialsInput = components['schemas']['AuthCredentialsInput']
export type ProfileUpdateInput = components['schemas']['ProfileUpdateInput']

export type ClientKey = components['schemas']['Key']
export type KeyCreateInput = components['schemas']['KeyCreateInput']
export type KeyUpdateInput = components['schemas']['KeyUpdateInput']
export type KeySecret = components['schemas']['KeySecret']

export type Channel = components['schemas']['Channel']
export type ChannelCreateInput = components['schemas']['ChannelCreateInput']
export type ChannelUpdateInput = components['schemas']['ChannelUpdateInput']
type ChannelBalanceFields = { balance?: string; delta?: string; description?: string }
export type ChannelBalanceInput = ChannelBalanceFields & ({ balance: string } | { delta: string })
export type ChannelStatusInput = components['schemas']['ChannelStatusInput']
export type ChannelModel = components['schemas']['ChannelModel']
export type ChannelModelInput = components['schemas']['ChannelModelInput']
export type ChannelModelUpdate = components['schemas']['ChannelModelUpdate']
export type ChannelTestItem = components['schemas']['ChannelTestItem']
export type RemoteModel = components['schemas']['RemoteModel']
export type RemoteModelsResult = { ok: boolean; models?: RemoteModel[]; error?: string }
export type Health = components['schemas']['Health']
export type BreakerConfig = components['schemas']['BreakerConfig']
export type BreakerConfigInput = BreakerConfig

export type Pricing = components['schemas']['Pricing']
export type PricingCreateInput = components['schemas']['PricingCreateInput']
export type DeletePricingInput = components['schemas']['DeletePricingInput']

export type UsageLog = components['schemas']['UsageLog']
export type Stats = components['schemas']['Stats']
export type DailyStats = components['schemas']['DailyStats']
export type ChannelStats = components['schemas']['ChannelStats']
export type UsageAggregate = components['schemas']['UsageAggregate']

export type RateLimit = components['schemas']['RateLimit']
export type RateLimitCreateInput = components['schemas']['RateLimitCreateInput']
export type RateLimitUpdateInput = components['schemas']['RateLimitUpdateInput']
export type QuotaPolicy = components['schemas']['QuotaPolicy']
type QuotaPolicyCreateFields = { policy_name: string; scope_type: 'user' | 'api_key'; scope_id: number; period_type: 'day' | 'month'; enabled?: boolean }
export type QuotaPolicyCreateInput = QuotaPolicyCreateFields & ({ token_limit: number; cost_limit?: string } | { token_limit?: number; cost_limit: string })
export type Deleted = { deleted: true }