package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
	settlement "LLMGateway/server/internal/proxy/settlement"
)

var errDownstreamWrite = errors.New("downstream stream write failed")

type completionStream struct {
	service               *Service
	body                  io.ReadCloser
	ctx                   context.Context
	requestID             string
	auth                  *accounts.AuthContext
	candidate             catalog.RouteCandidate
	publicModel           string
	clientIP              string
	start                 time.Time
	reservationID         int64
	rateReservationID     int64
	estimatedPromptTokens int
	cancel                context.CancelFunc
	probeLeaseID          string

	mu        sync.Mutex
	forwarded bool
}

func (s *completionStream) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.rateReservationID != 0 {
		relCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
		_ = s.service.ratelimit.ReleaseRateLimit(relCtx, s.rateReservationID)
		cancel()
	}
	if s.probeLeaseID != "" {
		relCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
		_, _ = s.service.catalog.ReleaseChannelProbe(relCtx, s.candidate.ChannelID, s.probeLeaseID)
		cancel()
	}
	return s.body.Close()
}

func (s *completionStream) Forward(emit func([]byte) error) error {
	if s.cancel != nil {
		defer s.cancel()
	}
	s.mu.Lock()
	if s.forwarded {
		s.mu.Unlock()
		return errors.New("chat stream already forwarded")
	}
	s.forwarded = true
	s.mu.Unlock()
	defer s.body.Close()
	settled := false
	defer func() {
		if !settled && s.reservationID != 0 {
			relCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
			_ = s.service.quota.ReleaseQuota(relCtx, s.reservationID)
			cancel()
		}
	}()

	var usage *Usage
	var ttft *int
	done := false
	var forwardedText strings.Builder
	err := s.service.adapter.ParseStream(s.body, s.publicModel, func(event StreamEvent) error {
		if event.Done {
			done = true
			return nil
		}
		if event.Data && ttft == nil {
			value := elapsedMs(s.start, s.service.now())
			ttft = &value
		}
		if event.Usage != nil {
			usage = event.Usage
		}
		if err := emit(event.Frame); err != nil {
			return fmt.Errorf("%w: %v", errDownstreamWrite, err)
		}
		forwardedText.WriteString(event.Text)
		return nil
	})
	if err != nil {
		if errors.Is(err, errDownstreamWrite) || errors.Is(s.ctx.Err(), context.Canceled) {
			if s.settlePartial(forwardedText.String(), ttft, "partial_estimated_client_canceled") {
				settled = true
			}
			return err
		}
		reason := catalog.FailureUpstreamUnreachable
		code := "upstream_stream_interrupted"
		message := "upstream stream was interrupted"
		if errors.Is(err, ErrInvalidStream) {
			reason = catalog.FailureUpstreamProtocol
			code = "upstream_stream_protocol_error"
			message = "upstream stream contained invalid data"
		}
		s.service.recordChannelHealth(s.ctx, s.candidate.ChannelID, false, reason)
		if s.settlePartial(forwardedText.String(), ttft, "partial_estimated_"+code) {
			settled = true
		}
		s.emitError(emit, code, message)
		return err
	}
	if !done {
		if errors.Is(s.ctx.Err(), context.Canceled) {
			if s.settlePartial(forwardedText.String(), ttft, "partial_estimated_client_canceled") {
				settled = true
			}
			return s.ctx.Err()
		}
		s.service.recordChannelHealth(s.ctx, s.candidate.ChannelID, false, catalog.FailureUpstreamProtocol)
		if s.settlePartial(forwardedText.String(), ttft, "partial_estimated_upstream_stream_interrupted") {
			settled = true
		}
		s.emitError(emit, "upstream_stream_interrupted", "upstream stream ended before [DONE]")
		return ErrUpstream
	}
	if usage == nil {
		s.service.recordChannelHealth(s.ctx, s.candidate.ChannelID, false, catalog.FailureUpstreamProtocol)
		s.logError(nil, ttft, "upstream_usage_missing")
		s.emitError(emit, "upstream_usage_missing", "upstream stream did not include usage")
		return ErrUpstream
	}

	durationMs := elapsedMs(s.start, s.service.now())
	cost, inputPrice, outputPrice, err := s.service.priceFor(s.ctx, s.candidate.ChannelID, s.publicModel, usage)
	if err != nil {
		s.logError(usage, ttft, "pricing_error")
		s.emitError(emit, "pricing_error", "unable to price completion")
		return err
	}
	s.service.recordChannelHealth(s.ctx, s.candidate.ChannelID, true, "")
	usageLog := s.service.usageLogInput(s.requestID, s.auth, &s.candidate.ChannelID, s.candidate.UpstreamModel, s.publicModel, usage, cost, inputPrice, outputPrice, durationMs, s.clientIP, "success", "")
	usageLog.TTFTMs = ttft
	_, err = s.service.Settle(s.ctx, settlement.Input{
		ReservationID: s.reservationID,
		UserID:        s.auth.UserID, APIKeyID: s.auth.KeyID, ChannelID: &s.candidate.ChannelID, Cost: cost,
		DebitChannel: s.candidate.Balance != nil, UsageLog: usageLog,
	})
	if err != nil {
		s.logError(usage, ttft, "settlement_failed")
		s.emitError(emit, "settlement_failed", "unable to settle completion")
		return err
	}
	settled = true
	if s.rateReservationID != 0 {
		finCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
		_ = s.service.ratelimit.FinalizeRateLimit(finCtx, s.rateReservationID, int64(usage.TotalTokens))
		cancel()
		s.rateReservationID = 0
	}
	lastUsedCtx, lastUsedCancel := detachedCtx(s.ctx, bestEffortTimeout)
	_ = s.service.store.UpdateKeyLastUsed(lastUsedCtx, s.auth.KeyID)
	lastUsedCancel()
	if err := emit([]byte("data: [DONE]\n\n")); err != nil {
		return fmt.Errorf("%w: %v", errDownstreamWrite, err)
	}
	return nil
}

func (s *completionStream) logError(usage *Usage, ttft *int, code string) {
	durationMs := elapsedMs(s.start, s.service.now())
	input := s.service.usageLogInput(s.requestID, s.auth, &s.candidate.ChannelID, s.candidate.UpstreamModel, s.publicModel, usage, "0.000000", "", "", durationMs, s.clientIP, "error", code)
	input.TTFTMs = ttft
	logCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
	_, _ = s.service.store.InsertUsageLog(logCtx, input)
	cancel()
}

// settlePartial charges only text frames confirmed written to the downstream
// caller when the upstream final usage frame is unavailable. A missing or failed
// local token estimate remains uncharged rather than inventing a token count.
func (s *completionStream) settlePartial(text string, ttft *int, code string) bool {
	if text == "" || s.service.adapter.CountTextTokens == nil {
		s.logError(nil, ttft, code)
		return false
	}
	completionTokens, err := s.service.adapter.CountTextTokens(s.publicModel, text)
	if err != nil || completionTokens <= 0 {
		s.logError(nil, ttft, code)
		return false
	}
	usage := &Usage{
		PromptTokens:     s.estimatedPromptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      s.estimatedPromptTokens + completionTokens,
	}
	cost, inputPrice, outputPrice, err := s.service.priceFor(s.ctx, s.candidate.ChannelID, s.publicModel, usage)
	if err != nil {
		s.logError(usage, ttft, code)
		return false
	}
	input := s.service.usageLogInput(s.requestID, s.auth, &s.candidate.ChannelID, s.candidate.UpstreamModel, s.publicModel, usage, cost, inputPrice, outputPrice, elapsedMs(s.start, s.service.now()), s.clientIP, "error", code)
	input.TTFTMs = ttft
	if _, err := s.service.Settle(s.ctx, settlement.Input{
		ReservationID: s.reservationID,
		UserID:        s.auth.UserID,
		APIKeyID:      s.auth.KeyID,
		ChannelID:     &s.candidate.ChannelID,
		Cost:          cost,
		DebitChannel:  s.candidate.Balance != nil,
		UsageLog:      input,
	}); err != nil {
		// The atomic settlement did not create a usage log, so record the failed
		// partial charge with a distinct request id instead of colliding with it.
		input.RequestID += "_settlement_failed"
		input.TotalCost = "0.000000"
		input.UnitPriceInputPer1M = ""
		input.UnitPriceOutputPer1M = ""
		input.ErrorCode = "partial_estimated_settlement_failed"
		logCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
		_, _ = s.service.store.InsertUsageLog(logCtx, input)
		cancel()
		return false
	}
	if s.rateReservationID != 0 {
		finCtx, cancel := detachedCtx(s.ctx, bestEffortTimeout)
		_ = s.service.ratelimit.FinalizeRateLimit(finCtx, s.rateReservationID, int64(usage.TotalTokens))
		cancel()
		s.rateReservationID = 0
	}
	lastUsedCtx, lastUsedCancel := detachedCtx(s.ctx, bestEffortTimeout)
	_ = s.service.store.UpdateKeyLastUsed(lastUsedCtx, s.auth.KeyID)
	lastUsedCancel()
	return true
}

func (s *completionStream) emitError(emit func([]byte) error, code, message string) {
	_ = emit(s.service.adapter.StreamError(code, message))
	_ = emit([]byte("data: [DONE]\n\n"))
}
