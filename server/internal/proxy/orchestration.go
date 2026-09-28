package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	settlement "LLMGateway/server/internal/proxy/settlement"
	"LLMGateway/server/internal/ratelimit"
	usagecontracts "LLMGateway/server/internal/usage"
)

// Models models returns the OpenAI-style model list visible to the key, filtered by
// its permissions.
func (a *Service) Models(ctx context.Context, auth *accounts.AuthContext) (ModelList, error) {
	result, err := a.catalog.ListCatalogModels(ctx, auth.UserID, true)
	if err != nil {
		return ModelList{}, err
	}

	created := a.now().Unix()
	seen := map[string]bool{}
	data := []Model{}
	for _, item := range result.List {
		name := item.ModelName
		if name == "" || seen[name] || !allowModel(auth, name) {
			continue
		}
		seen[name] = true
		data = append(data, Model{ID: name, Created: created, OwnedBy: "llmgateway"})
	}
	return ModelList{Models: data}, nil
}

// ChatCompletions prepares a buffered or streaming chat completion. A non-nil
// error is a pre-flight/transport failure the handler maps to an OpenAI error.
func (a *Service) ChatCompletions(ctx context.Context, auth *accounts.AuthContext, req ChatRequest, clientIP string) (ChatResponse, error) {
	requestCtx, cancel := context.WithTimeout(ctx, a.requestTimeout)
	streamOwnsCancel := false
	defer func() {
		if !streamOwnsCancel {
			cancel()
		}
	}()
	ctx = requestCtx
	if req.Model == "" {
		return ChatResponse{}, ErrInvalidRequest
	}
	if !allowModel(auth, req.Model) {
		return ChatResponse{}, ErrForbidden
	}

	requestID := newRequestID()
	start := a.now()
	estimate, estimateErr := a.adapter.EstimateUsage(req.Body, a.defaultMaxTokens)
	var estimatedTokens *int64
	if estimateErr == nil {
		value := int64(estimate.TotalTokens)
		estimatedTokens = &value
	}
	if err := a.checkRateLimit(ctx, auth, req.Model, estimatedTokens); err != nil {
		if errors.Is(err, ErrRateLimited) {
			a.logUsage(ctx, requestID, auth, nil, "", req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "rate_limited")
		}
		return ChatResponse{}, err
	}
	if estimateErr != nil {
		return ChatResponse{}, ErrRateLimited
	}
	rateReservation, rateErr := a.ratelimit.ReserveRateLimit(ctx, ratelimit.RateLimitReservationInput{RequestID: requestID, UserID: auth.UserID, APIKeyID: auth.KeyID, Model: req.Model, EstimatedTokens: int64(estimate.TotalTokens), ExpiresAt: a.now().Add(a.requestTimeout)})
	if rateErr != nil {
		return ChatResponse{}, ErrRateLimited
	}
	rateReservationOpen := true
	defer func() {
		if rateReservationOpen {
			relCtx, cancel := detachedCtx(ctx, bestEffortTimeout)
			defer cancel()
			_ = a.ratelimit.ReleaseRateLimit(relCtx, rateReservation.ID)
		}
	}()

	candidates, probes, err := a.orderedCandidates(ctx, auth.UserID, req.Model, auth.KeyID)
	defer a.releaseProbes(ctx, probes)
	if err != nil {
		if errors.Is(err, ErrNoHealthyChannel) {
			// Degraded: every candidate is tripped open or there is no mapping.
			a.logUsage(ctx, requestID, auth, nil, "", req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "no_healthy_channel")
		}
		return ChatResponse{}, err
	}
	if len(candidates) == 0 {
		a.logUsage(ctx, requestID, auth, nil, "", req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "no_healthy_channel")
		return ChatResponse{}, ErrNoHealthyChannel
	}
	candidate := candidates[0]
	if a.maxAttempts < len(candidates) {
		candidates = candidates[:a.maxAttempts]
	}
	reservation, err := a.reserveQuota(ctx, requestID, auth, req, candidate.ChannelID)
	if err != nil {
		if errors.Is(err, ErrQuotaExceeded) {
			a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "quota_exceeded")
		}
		return ChatResponse{}, err
	}
	releaseReservation := reservation.ID != 0
	defer func() {
		if releaseReservation {
			relCtx, cancel := detachedCtx(ctx, bestEffortTimeout)
			defer cancel()
			_ = a.quota.ReleaseQuota(relCtx, reservation.ID)
		}
	}()

	if a.adapter.RewriteRequest == nil {
		return ChatResponse{}, ErrInvalidRequest
	}
	var resp *http.Response
	var responseBody []byte
	var readErr error
	healthRecorded := false
	for attempt, next := range candidates {
		if ctx.Err() != nil {
			return ChatResponse{}, ctx.Err()
		}
		candidate = next
		if err := a.checkChannelRateLimit(ctx, auth, req.Model, candidate.ChannelID, *estimatedTokens); err != nil {
			if errors.Is(err, ErrRateLimited) {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamUnreachable)
				if attempt+1 < len(candidates) {
					continue
				}
				a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "rate_limited")
			}
			return ChatResponse{}, err
		}
		secret, secretErr := a.catalog.GetChannelSecret(ctx, auth.UserID, candidate.ChannelID)
		if secretErr != nil {
			return ChatResponse{}, secretErr
		}
		upstreamBody, rewriteErr := a.adapter.RewriteRequest(req.Body, candidate.UpstreamModel)
		if rewriteErr != nil {
			return ChatResponse{}, ErrInvalidRequest
		}
		httpReq, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(secret.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(upstreamBody))
		if requestErr != nil {
			return ChatResponse{}, ErrUpstream
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if req.Stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		} else {
			httpReq.Header.Set("Accept", "application/json")
		}
		if secret.AuthType == "bearer" && secret.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+secret.APIKey)
		}
		resp, err = a.client.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return ChatResponse{}, ctx.Err()
			}
			if reason := catalog.ClassifyUpstreamResult(0, err); reason.CountsAsChannelFailure() {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
				if attempt+1 < len(candidates) {
					continue
				}
			}
			a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "upstream_unreachable")
			return ChatResponse{}, ErrUpstream
		}
		if req.Stream && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		responseBody, readErr = io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if ctx.Err() != nil {
				return ChatResponse{}, ctx.Err()
			}
			a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamUnreachable)
			if attempt+1 < len(candidates) {
				continue
			}
			a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", "upstream_stream_interrupted")
			return ChatResponse{}, ErrUpstream
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		if reason := catalog.ClassifyUpstreamResult(resp.StatusCode, nil); reason.CountsAsChannelFailure() {
			a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
			healthRecorded = true
			if attempt+1 < len(candidates) {
				continue
			}
		}
		break
	}
	if resp == nil {
		return ChatResponse{}, ErrUpstream
	}
	if catalog.ClassifyUpstreamResult(resp.StatusCode, nil).CountsAsChannelFailure() && len(candidates) > 1 {
		a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", elapsedMs(start, a.now()), clientIP, "error", fmt.Sprintf("upstream_%d", resp.StatusCode))
		return ChatResponse{}, ErrUpstream
	}
	if req.Stream && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if a.adapter.ParseStream == nil || a.adapter.StreamError == nil {
			resp.Body.Close()
			return ChatResponse{}, ErrInvalidRequest
		}
		releaseReservation = false
		streamOwnsCancel = true
		rateReservationOpen = false
		// The stream outlives this call, so it owns the probe lease until it
		// closes; remove it from the deferred release set.
		probeLeaseID := probes[candidate.ChannelID]
		delete(probes, candidate.ChannelID)
		return ChatResponse{Status: resp.StatusCode, Stream: &completionStream{
			service: a, body: resp.Body, ctx: ctx, requestID: requestID, auth: auth,
			candidate: candidate, publicModel: req.Model, clientIP: clientIP, start: start, reservationID: reservation.ID, rateReservationID: rateReservation.ID, estimatedPromptTokens: estimate.InputTokens, cancel: cancel, probeLeaseID: probeLeaseID,
		}}, nil
	}

	if responseBody == nil {
		defer resp.Body.Close()
		responseBody, readErr = io.ReadAll(resp.Body)
	}
	durationMs := elapsedMs(start, a.now())
	if readErr != nil {
		if !errors.Is(readErr, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
			a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamUnreachable)
		}
		a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", durationMs, clientIP, "error", "upstream_stream_interrupted")
		return ChatResponse{}, ErrUpstream
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if reason := catalog.ClassifyUpstreamResult(resp.StatusCode, nil); reason.CountsAsChannelFailure() && !healthRecorded {
			a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
		}
		a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", durationMs, clientIP, "error", fmt.Sprintf("upstream_%d", resp.StatusCode))
		return ChatResponse{Status: resp.StatusCode, Body: responseBody}, nil
	}

	if a.adapter.ParseUsage == nil || a.adapter.RewriteResponse == nil {
		return ChatResponse{}, ErrInvalidRequest
	}
	usage := a.adapter.ParseUsage(responseBody)
	if usage == nil {
		a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamProtocol)
		a.logUsage(ctx, requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, nil, "0.000000", "", "", durationMs, clientIP, "error", "upstream_usage_missing")
		return ChatResponse{}, ErrUpstream
	}
	a.recordChannelHealth(ctx, candidate.ChannelID, true, "")
	cost, inputPrice, outputPrice, err := a.priceFor(ctx, candidate.ChannelID, req.Model, usage)
	if err != nil {
		return ChatResponse{}, err
	}

	usageLog := a.usageLogInput(requestID, auth, &candidate.ChannelID, candidate.UpstreamModel, req.Model, usage, cost, inputPrice, outputPrice, durationMs, clientIP, "success", "")
	_, err = a.Settle(ctx, settlement.Input{
		ReservationID: reservation.ID,
		UserID:        auth.UserID,
		APIKeyID:      auth.KeyID,
		ChannelID:     &candidate.ChannelID,
		Cost:          cost,
		DebitChannel:  candidate.Balance != nil,
		UsageLog:      usageLog,
	})
	if err != nil {
		return ChatResponse{}, err
	}
	releaseReservation = false
	bestEffortCtx, bestEffortCancel := detachedCtx(ctx, bestEffortTimeout)
	_ = a.ratelimit.FinalizeRateLimit(bestEffortCtx, rateReservation.ID, int64(usage.TotalTokens))
	bestEffortCancel()
	rateReservationOpen = false

	// Best-effort: the request already succeeded and was charged, so a
	// last_used_at update failure must not turn it into an error response.
	lastUsedCtx, lastUsedCancel := detachedCtx(ctx, bestEffortTimeout)
	_ = a.store.UpdateKeyLastUsed(lastUsedCtx, auth.KeyID)
	lastUsedCancel()

	return ChatResponse{Status: http.StatusOK, Body: a.adapter.RewriteResponse(responseBody, req.Model), Usage: usage}, nil
}

// recordChannelHealth drives the circuit breaker state machine. It is
// best-effort: a recording failure must never change the response.
func (a *Service) recordChannelHealth(ctx context.Context, channelID int, success bool, reason catalog.FailureReason) {
	detached, cancel := detachedCtx(ctx, bestEffortTimeout)
	defer cancel()
	_, _ = a.catalog.RecordChannelAttempt(detached, channelID, success, reason)
}

func (a *Service) priceFor(ctx context.Context, channelID int, model string, usage *Usage) (string, string, string, error) {
	pricing, err := a.catalog.GetPricing(ctx, channelID, model)
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			// Known behaviour: a channel+model without pricing is served for
			// free (cost 0). Documented in docs/backend-structure.md; operators
			// should configure pricing for every routable model.
			return "0.000000", "", "", nil
		}
		return "", "", "", err
	}

	inputPrice := pricing.InputPricePer1M
	outputPrice := pricing.OutputPricePer1M
	inputTokens, outputTokens, cachedTokens := 0, 0, 0
	if usage != nil {
		inputTokens = usage.PromptTokens
		outputTokens = usage.CompletionTokens
		cachedTokens = cachedTokenCount(usage)
	}
	cost, err := catalog.ComputeCost(inputPrice, outputPrice, pricing.CachedInputPricePer1M, inputTokens, outputTokens, cachedTokens)
	if err != nil {
		return "", "", "", err
	}
	return cost, inputPrice, outputPrice, nil
}

func (a *Service) logUsage(ctx context.Context, requestID string, auth *accounts.AuthContext, channelID *int, upstreamModel, model string, usage *Usage, cost, inputPrice, outputPrice string, durationMs int, clientIP, status, errorCode string) {
	detached, cancel := detachedCtx(ctx, bestEffortTimeout)
	defer cancel()
	_, _ = a.store.InsertUsageLog(detached, a.usageLogInput(requestID, auth, channelID, upstreamModel, model, usage, cost, inputPrice, outputPrice, durationMs, clientIP, status, errorCode))
}

func (a *Service) usageLogInput(requestID string, auth *accounts.AuthContext, channelID *int, upstreamModel, model string, usage *Usage, cost, inputPrice, outputPrice string, durationMs int, clientIP, status, errorCode string) usagecontracts.UsageLogInput {
	userID := auth.UserID
	keyID := auth.KeyID

	input := usagecontracts.UsageLogInput{
		RequestID:            requestID,
		UserID:               &userID,
		APIKeyID:             &keyID,
		ChannelID:            channelID,
		Model:                model,
		UpstreamModel:        upstreamModel,
		UnitPriceInputPer1M:  inputPrice,
		UnitPriceOutputPer1M: outputPrice,
		TotalCost:            cost,
		DurationMs:           durationMs,
		Status:               status,
		ErrorCode:            errorCode,
		ClientIP:             clientIP,
	}
	if usage != nil {
		input.InputTokens = usage.PromptTokens
		input.OutputTokens = usage.CompletionTokens
		input.CachedInputTokens = cachedTokenCount(usage)
		input.TotalTokens = usage.TotalTokens
	}
	return input
}

func cachedTokenCount(usage *Usage) int {
	if usage == nil {
		return 0
	}
	return usage.CachedInputTokens
}

func elapsedMs(start, end time.Time) int {
	return int(end.Sub(start).Milliseconds())
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(buf)
}
