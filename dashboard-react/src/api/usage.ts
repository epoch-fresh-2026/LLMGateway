import { adminGet } from './client'
import { apiPaths } from './paths'
import { ChannelStatsListSchema, DailyStatsListSchema, StatsSchema, TTFTStatsSchema, UsageAggregateListSchema, UsageLogListSchema, UsageLogSchema } from './runtime/usage'
import type { DailyStats, ListResponse, Stats, UsageAggregate, UsageLog } from '../types/api'

export type DateRange = { date_from?: string; date_to?: string }
export type TimeRange = { start_time?: string; end_time?: string }
export type Pagination = { page?: number; page_size?: number }
export type UsageLogParams = Pagination & { user_id?: number; channel_id?: number; api_key_id?: number; model?: string; status?: string } & TimeRange
export type DailyStatsParams = DateRange & Pagination
export type UsageAggregateParams = Pagination & TimeRange & DateRange & { group_by: 'user' | 'api_key' | 'model' | 'channel'; user_id?: number; api_key_id?: number; channel_id?: number; model?: string; status?: string }

export const overview = (params: TimeRange = {}) => adminGet(apiPaths.usageOverview(), { start_time: new Date(Date.now() - 6 * 86400000).toISOString(), end_time: new Date().toISOString(), ...params }, StatsSchema)
export const logs = (params: UsageLogParams = {}) => adminGet(apiPaths.usageLogs(), { page: 1, page_size: 50, ...params }, UsageLogListSchema)
export const daily = (params: DailyStatsParams = {}) => adminGet(apiPaths.usageDaily(), { page: 1, page_size: 100, ...params }, DailyStatsListSchema)
export const usageStats = (params: UsageAggregateParams) => adminGet(apiPaths.usageAggregate(), params, UsageAggregateListSchema)
export const listUsageLogs = (params: UsageLogParams = {}) => adminGet(apiPaths.usageLogs(), { page: 1, page_size: 20, ...params }, UsageLogListSchema)
export const getUsageLog = (id: number) => adminGet(apiPaths.usageLog(id), UsageLogSchema)
export const channelStats = (params: TimeRange & DateRange = {}) => adminGet(apiPaths.usageChannels(), params, ChannelStatsListSchema)
export const ttft = (params: TimeRange = {}) => adminGet(apiPaths.usageTTFT(), params, TTFTStatsSchema)
