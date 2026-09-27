// Package testtypes provides test-only aliases to the owning business modules.
package testtypes

import (
	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/quota"
	"LLMGateway/server/internal/ratelimit"
	"LLMGateway/server/internal/usage"
)

type Channel = catalog.Channel
type ChannelDTO = catalog.ChannelDTO
type ChannelModel = catalog.ChannelModel
type CatalogChannelDTO = catalog.CatalogChannelDTO
type CatalogModelDTO = catalog.CatalogModelDTO
type Pricing = catalog.Pricing
type PricingDTO = catalog.PricingDTO
type RouteCandidate = catalog.RouteCandidate
type ChannelTestItemDTO = catalog.ChannelTestItemDTO
type ChannelTestResultDTO = catalog.ChannelTestResultDTO
type ChannelInput = catalog.ChannelInput
type PricingInput = catalog.PricingInput
type DeletePricingInput = catalog.DeletePricingInput
type HealthState = catalog.HealthState
type ChannelHealth = catalog.ChannelHealth
type ChannelHealthDTO = catalog.ChannelHealthDTO
type ChannelBreakerConfig = catalog.ChannelBreakerConfig
type ChannelHealthWindow = catalog.ChannelHealthWindow
type FailureReason = catalog.FailureReason

const (
	HealthClosed               = catalog.HealthClosed
	HealthOpen                 = catalog.HealthOpen
	HealthHalfOpen             = catalog.HealthHalfOpen
	FailureUpstreamUnreachable = catalog.FailureUpstreamUnreachable
	FailureUpstream401         = catalog.FailureUpstream401
	FailureUpstream402         = catalog.FailureUpstream402
	FailureUpstream403         = catalog.FailureUpstream403
	FailureUpstream429         = catalog.FailureUpstream429
	FailureUpstream5xx         = catalog.FailureUpstream5xx
	FailureUpstreamProtocol    = catalog.FailureUpstreamProtocol
	FailureUpstreamTimeout     = catalog.FailureUpstreamTimeout
	MaxRateLimitWindowSeconds  = ratelimit.MaxRateLimitWindowSeconds
	QuotaScopeUser             = quota.QuotaScopeUser
	QuotaScopeAPIKey           = quota.QuotaScopeAPIKey
	QuotaPeriodDay             = quota.QuotaPeriodDay
	QuotaPeriodMonth           = quota.QuotaPeriodMonth
)

type User = accounts.User
type ProfileUpdateInput = accounts.ProfileUpdateInput
type ClientKey = accounts.ClientKey
type ClientKeyDTO = accounts.ClientKeyDTO
type KeySecretDTO = accounts.KeySecretDTO
type KeyInput = accounts.KeyInput
type KeyUpdateInput = accounts.KeyUpdateInput
type AuthContext = accounts.AuthContext
type Account = accounts.Account
type AuthConfig = accounts.AuthConfig
type LoginResult = accounts.LoginResult

type UsageLog = usage.UsageLog
type UsageLogDTO = usage.UsageLogDTO
type StatsOverviewDTO = usage.StatsOverviewDTO
type TTFTStatsFilter = usage.TTFTStatsFilter
type TTFTStatsDTO = usage.TTFTStatsDTO
type StatsDailyDTO = usage.StatsDailyDTO
type StatsChannelDTO = usage.StatsChannelDTO
type UsageAggregateFilter = usage.UsageAggregateFilter
type UsageAggregateDTO = usage.UsageAggregateDTO
type UsageLogFilter = usage.UsageLogFilter
type UsageCountFilter = usage.UsageCountFilter
type TokenCountFilter = usage.TokenCountFilter
type UsageLogInput = usage.UsageLogInput
type SettlementInput = settlement.Input
type RateLimitRule = ratelimit.RateLimitRule
type RateLimitRuleDTO = ratelimit.RateLimitRuleDTO
type RateLimitInput = ratelimit.RateLimitInput
type RateLimitReservationInput = ratelimit.RateLimitReservationInput
type RateLimitReservation = ratelimit.RateLimitReservation
type QuotaScopeType = quota.QuotaScopeType
type QuotaPeriodType = quota.QuotaPeriodType
type QuotaPolicy = quota.QuotaPolicy
type QuotaPolicyDTO = quota.QuotaPolicyDTO
type QuotaPolicyInput = quota.QuotaPolicyInput
type QuotaPolicyFilter = quota.QuotaPolicyFilter
type QuotaReserveInput = quota.QuotaReserveInput
type QuotaReservation = quota.QuotaReservation
type QuotaUsageDTO = quota.QuotaUsageDTO

var ValidateTimeRange = usage.ValidateTimeRange
var ValidateSince = usage.ValidateSince
var ValidateDateRange = usage.ValidateDateRange
var QuotaPeriodBounds = quota.QuotaPeriodBounds
var NormalizeQuotaPolicy = quota.NormalizeQuotaPolicy
var QuotaPolicyToDTO = quota.QuotaPolicyToDTO
