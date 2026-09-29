import type { paths } from './generated/schema'

type AdminPath = Extract<keyof paths, `/admin/${string}`>
type DynamicPath = `/admin/channels/${number}` | `/admin/channels/${number}/status` | `/admin/channels/${number}/balance` | `/admin/channels/${number}/health` | `/admin/channels/${number}/breaker` | `/admin/channels/${number}/models` | `/admin/channels/${number}/models/${number}` | `/admin/channels/${number}/remote-models` | `/admin/channels/${number}/test` | `/admin/channels/${number}/health/reset` | `/admin/keys/${number}` | `/admin/keys/${number}/reset` | `/admin/usage-logs/${number}` | `/admin/rate-limits/${number}` | `/admin/quota-policies/${number}`
const admin = <P extends AdminPath | DynamicPath>(path: P) => path

export const apiPaths = {
  channels: () => admin('/admin/channels'),
  channel: (id: number) => admin(`/admin/channels/${id}` as `/admin/channels/${number}`),
  channelStatus: (id: number) => admin(`/admin/channels/${id}/status` as `/admin/channels/${number}/status`),
  channelBalance: (id: number) => admin(`/admin/channels/${id}/balance` as `/admin/channels/${number}/balance`),
  channelHealth: (id: number) => admin(`/admin/channels/${id}/health` as `/admin/channels/${number}/health`),
  channelHealthList: () => admin('/admin/channels/health'),
  channelBreaker: (id: number) => admin(`/admin/channels/${id}/breaker` as `/admin/channels/${number}/breaker`),
  channelModels: (id: number) => admin(`/admin/channels/${id}/models` as `/admin/channels/${number}/models`),
  channelModel: (channelId: number, modelId: number) => admin(`/admin/channels/${channelId}/models/${modelId}` as `/admin/channels/${number}/models/${number}`),
  channelRemoteModels: (id: number) => admin(`/admin/channels/${id}/remote-models` as `/admin/channels/${number}/remote-models`),
  channelTest: (id: number) => admin(`/admin/channels/${id}/test` as `/admin/channels/${number}/test`),
  channelHealthReset: (id: number) => admin(`/admin/channels/${id}/health/reset` as `/admin/channels/${number}/health/reset`),
  breakerConfig: () => admin('/admin/breaker-config'),
  models: () => admin('/admin/models'),
  pricing: () => admin('/admin/pricing'),
  profile: () => admin('/admin/profile'),
  keys: () => admin('/admin/keys'),
  key: (id: number) => admin(`/admin/keys/${id}` as `/admin/keys/${number}`),
  keyReset: (id: number) => admin(`/admin/keys/${id}/reset` as `/admin/keys/${number}/reset`),
  usageOverview: () => admin('/admin/stats/overview'),
  usageDaily: () => admin('/admin/stats/daily'),
  usageChannels: () => admin('/admin/stats/channels'),
  usageAggregate: () => admin('/admin/stats/usage'),
  usageTTFT: () => admin('/admin/stats/ttft'),
  usageLogs: () => admin('/admin/usage-logs'),
  usageLog: (id: number) => admin(`/admin/usage-logs/${id}` as `/admin/usage-logs/${number}`),
  rateLimits: () => admin('/admin/rate-limits'),
  rateLimit: (id: number) => admin(`/admin/rate-limits/${id}` as `/admin/rate-limits/${number}`),
  quotaPolicies: () => admin('/admin/quota-policies'),
  quotaPolicy: (id: number) => admin(`/admin/quota-policies/${id}` as `/admin/quota-policies/${number}`),
  quotaUsage: () => admin('/admin/quota-usage'),
  authRegister: () => admin('/admin/auth/register'),
  authLogin: () => admin('/admin/auth/login'),
  authLogout: () => admin('/admin/auth/logout'),
  authMe: () => admin('/admin/auth/me'),
} as const
