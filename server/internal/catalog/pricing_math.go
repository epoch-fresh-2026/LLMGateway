package catalog

import (
	"fmt"

	"LLMGateway/server/internal/money"
)

// ComputeCost converts token counts and 8-decimal per-1M unit prices into a
// 6-decimal cost string. The unit prices are scaled by 1e8 and the cost by 1e6,
// so price8 * tokens / 1e8 yields the 6-decimal cost. No float64 is used.
//
// inputTokens is the prompt token count, which (per the OpenAI usage object)
// already includes cachedTokens. Cached tokens are therefore billed at the
// cached price and only the remainder at the input price, so they are not
// charged twice.
func ComputeCost(inputPricePer1M, outputPricePer1M, cachedPricePer1M string, inputTokens, outputTokens, cachedTokens int) (string, error) {
	inputPrice, err := parsePrice8(inputPricePer1M)
	if err != nil {
		return "", err
	}
	outputPrice, err := parsePrice8(outputPricePer1M)
	if err != nil {
		return "", err
	}
	cachedPrice, err := parsePrice8(cachedPricePer1M)
	if err != nil {
		return "", err
	}

	billableInput := inputTokens - cachedTokens
	if billableInput < 0 {
		billableInput = 0
	}

	total := priceTokens(inputPrice, billableInput) + priceTokens(outputPrice, outputTokens) + priceTokens(cachedPrice, cachedTokens)
	return money.Format6(money.Amount(total)), nil
}

// EstimateReservationCost computes a conservative cost for a quota reservation
// before the actual usage is known. It charges every prompt token at the higher
// of the input and cached price so the reservation cannot under-count when the
// final cached-token split is unknown.
func EstimateReservationCost(inputPricePer1M, outputPricePer1M, cachedPricePer1M string, inputTokens, outputTokens int) (string, error) {
	inputPrice, err := parsePrice8(inputPricePer1M)
	if err != nil {
		return "", err
	}
	cachedPrice, err := parsePrice8(cachedPricePer1M)
	if err != nil {
		return "", err
	}
	if cachedPrice > inputPrice {
		inputPrice = cachedPrice
	}
	outputPrice, err := parsePrice8(outputPricePer1M)
	if err != nil {
		return "", err
	}
	total := priceTokens(inputPrice, inputTokens) + priceTokens(outputPrice, outputTokens)
	if total < 0 {
		return "", fmt.Errorf("%w: invalid estimated cost", ErrInvalid)
	}
	return money.Format6(money.Amount(total)), nil
}

func parsePrice8(value string) (money.Amount, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := money.Parse8(value)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid price", ErrInvalid)
	}
	return parsed, nil
}

// priceTokens computes price * tokens for an 8-decimal price and a token count,
// returning the cost in 1e-6 units.
func priceTokens(price money.Amount, tokens int) int64 {
	if price <= 0 || tokens <= 0 {
		return 0
	}
	return int64(price) * int64(tokens) / 100_000_000
}
