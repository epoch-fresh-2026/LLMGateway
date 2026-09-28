package proxy

import (
	"context"
	"errors"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/quota"
)

func (a *Service) reserveQuota(ctx context.Context, requestID string, auth *accounts.AuthContext, req ChatRequest, channelID int) (quota.QuotaReservation, error) {
	if a.adapter.EstimateUsage == nil {
		return quota.QuotaReservation{}, ErrInvalidRequest
	}
	estimate, err := a.adapter.EstimateUsage(req.Body, a.defaultMaxTokens)
	if err != nil {
		return quota.QuotaReservation{}, ErrInvalidRequest
	}
	estimatedCost, err := a.estimatedCost(ctx, channelID, req.Model, estimate)
	if err != nil {
		return quota.QuotaReservation{}, err
	}
	reservation, err := a.quota.ReserveQuota(ctx, quota.QuotaReserveInput{
		RequestID: requestID, UserID: auth.UserID, APIKeyID: auth.KeyID, Model: req.Model,
		EstimatedTokens: int64(estimate.TotalTokens), EstimatedCost: estimatedCost,
		ExpiresAt: a.now().Add(a.reservationTTL),
	})
	if errors.Is(err, apperrors.ErrQuotaExceeded) {
		return quota.QuotaReservation{}, ErrQuotaExceeded
	}
	return reservation, err
}

func (a *Service) estimatedCost(ctx context.Context, channelID int, model string, estimate EstimatedUsage) (string, error) {
	pricing, err := a.catalog.GetPricing(ctx, channelID, model)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "0.000000", nil
		}
		return "", err
	}
	return catalog.EstimateReservationCost(pricing.InputPricePer1M, pricing.OutputPricePer1M, pricing.CachedInputPricePer1M, estimate.InputTokens, estimate.OutputTokens)
}
