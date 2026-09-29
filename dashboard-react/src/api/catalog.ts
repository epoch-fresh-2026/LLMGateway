import { adminGet, adminSend } from './client'
import { apiPaths } from './paths'
import { ChannelBreakerConfigSchema, ChannelListSchema, ChannelModelListSchema, ChannelModelSchema, ChannelSchema, CatalogModelListSchema, ChannelTestSchema, HealthListSchema, PricingListSchema, PricingSchema, RemoteModelsSchema, DeletedSchema } from './runtime/catalog'
import type { Channel, ChannelBalanceInput, ChannelBreakerConfigInput, ChannelCreateInput, ChannelModel, ChannelModelInput, ChannelModelUpdate, ChannelStatusInput, ChannelTestItem, Deleted, Health, ListResponse, Pricing, PricingCreateInput, RemoteModelsResult, ChannelUpdateInput, DeletePricingInput } from '../types/api'

export type ListParams = { page?: number; page_size?: number }
export type ListModelsParams = ListParams & { status?: number }

export const listChannels = (params: ListParams = {}) => adminGet(apiPaths.channels(), { page: 1, page_size: 100, ...params }, ChannelListSchema)
export const listChannelHealth = () => adminGet(apiPaths.channelHealthList(), HealthListSchema)
export const createChannel = (input: ChannelCreateInput) => adminSend('POST', apiPaths.channels(), input, ChannelSchema)
export const updateChannel = (id: number, input: ChannelUpdateInput) => adminSend('PUT', apiPaths.channel(id), input, ChannelSchema)
export const deleteChannel = (id: number) => adminSend('DELETE', apiPaths.channel(id), DeletedSchema)
export const updateChannelStatus = (id: number, input: ChannelStatusInput) => adminSend('PUT', apiPaths.channelStatus(id), input, ChannelSchema)
export const updateChannelBalance = (id: number, input: ChannelBalanceInput) => adminSend('PUT', apiPaths.channelBalance(id), input, ChannelSchema)
export const resetChannelHealth = (id: number) => adminSend('POST', apiPaths.channelHealthReset(id), DeletedSchema)
export const getChannelBreakerConfig = (id: number) => adminGet(apiPaths.channelBreaker(id), ChannelBreakerConfigSchema)
export const updateChannelBreakerConfig = (id: number, input: ChannelBreakerConfigInput) => adminSend('PUT', apiPaths.channelBreaker(id), input, ChannelBreakerConfigSchema)
export const deleteChannelBreakerConfig = (id: number) => adminSend('DELETE', apiPaths.channelBreaker(id), DeletedSchema)
export const listChannelModels = (channelId: number) => adminGet(apiPaths.channelModels(channelId), ChannelModelListSchema)
export const createChannelModel = (channelId: number, input: ChannelModelInput) => adminSend('POST', apiPaths.channelModels(channelId), input, ChannelModelSchema)
export const updateChannelModel = (channelId: number, modelId: number, input: ChannelModelUpdate) => adminSend('PUT', apiPaths.channelModel(channelId, modelId), input, ChannelModelSchema)
export const deleteChannelModel = (channelId: number, modelId: number) => adminSend('DELETE', apiPaths.channelModel(channelId, modelId), DeletedSchema)
export const loadRemoteModels = (channelId: number) => adminSend('POST', apiPaths.channelRemoteModels(channelId), RemoteModelsSchema)
export const testChannel = (channelId: number, checkAll: boolean) => adminSend('POST', apiPaths.channelTest(channelId), { check_all: checkAll }, ChannelTestSchema)
export const listModels = (params: ListModelsParams = {}) => adminGet(apiPaths.models(), { status: 1, ...params }, CatalogModelListSchema)
export const listPricing = (params: ListParams = {}) => adminGet(apiPaths.pricing(), { page: 1, page_size: 100, ...params }, PricingListSchema)
export const createPricing = (input: PricingCreateInput) => adminSend('POST', apiPaths.pricing(), input, PricingSchema)
export const deletePricing = (input: DeletePricingInput) => adminSend('DELETE', apiPaths.pricing(), input, DeletedSchema)
