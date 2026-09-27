package catalog

import (
	"context"
	"fmt"
	"strings"

	"LLMGateway/server/internal/money"
)

func (a *Server) ListPricing(ctx context.Context, ownerUserID int) (ListResponse[PricingDTO], error) {
	return a.store.ListPricing(ctx, ownerUserID)
}

// UpsertPricing validates the target channel and model mapping, normalizes the
// 8-decimal prices and persists the row.
func (a *Server) UpsertPricing(ctx context.Context, ownerUserID int, in PricingInput) (PricingDTO, error) {
	if in.ChannelID <= 0 || strings.TrimSpace(in.ModelName) == "" {
		return PricingDTO{}, fmt.Errorf("%w: channel_id and model_name are required", ErrInvalid)
	}
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, in.ChannelID); err != nil {
		return PricingDTO{}, err
	}
	exists, err := a.store.ChannelModelExists(ctx, ownerUserID, in.ChannelID, in.ModelName)
	if err != nil {
		return PricingDTO{}, err
	}
	if !exists {
		return PricingDTO{}, fmt.Errorf("%w: model mapping not found", ErrInvalid)
	}

	inputPrice, err := normalizePrice(in.InputPricePer1M, "input_price_per_1m")
	if err != nil {
		return PricingDTO{}, err
	}
	outputPrice, err := normalizePrice(in.OutputPricePer1M, "output_price_per_1m")
	if err != nil {
		return PricingDTO{}, err
	}
	cachedPrice, err := normalizeOptionalPrice(in.CachedInputPricePer1M, "cached_input_price_per_1m")
	if err != nil {
		return PricingDTO{}, err
	}
	currency := in.Currency
	if currency == "" {
		currency = "USD"
	}

	return a.store.UpsertPricingRecord(ctx, PricingRecord{
		ChannelID:             in.ChannelID,
		ModelName:             in.ModelName,
		InputPricePer1M:       inputPrice,
		OutputPricePer1M:      outputPrice,
		CachedInputPricePer1M: cachedPrice,
		Currency:              currency,
	})
}

func (a *Server) DeletePricing(ctx context.Context, ownerUserID int, in DeletePricingInput) error {
	if _, err := a.store.GetChannelDTO(ctx, ownerUserID, in.ChannelID); err != nil {
		return err
	}
	return a.store.DeletePricing(ctx, in)
}

func (a *Server) GetPricing(ctx context.Context, channelID int, modelName string) (PricingDTO, error) {
	return a.store.GetPricing(ctx, channelID, modelName)
}

func normalizePrice(value string, field string) (string, error) {
	amount, err := money.Parse8(value)
	if err != nil {
		return "", fmt.Errorf("%w: invalid %s", ErrInvalid, field)
	}
	return money.Format8(amount), nil
}

func normalizeOptionalPrice(value string, field string) (string, error) {
	if value == "" {
		return "", nil
	}
	return normalizePrice(value, field)
}
