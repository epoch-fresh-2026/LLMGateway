package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"LLMGateway/server/internal/accounts"
	"LLMGateway/server/internal/catalog"
)

type upstreamAttemptInput struct {
	requestID       string
	auth            *accounts.AuthContext
	req             ChatRequest
	candidates      []catalog.RouteCandidate
	estimatedTokens int64
	rateLimits      *rateLimitSnapshot
	start           time.Time
	clientIP        string
	timing          *requestTiming
	stickyKey       stickyKey
	stickyBinding   stickyBinding
	lazyFallback    bool
	probes          map[int]string
}

// upstreamAttempt is the outcome of the candidate failover loop.
type upstreamAttempt struct {
	resp           *http.Response
	body           []byte
	candidate      catalog.RouteCandidate
	healthRecorded bool
	timing         *upstreamTiming
}

// attemptUpstreams tries candidates in order until one yields a usable
// response. Retryable failures record channel health and move on; when attempts
// are exhausted the terminal usage log is written and the mapped error
// returned. A response that is not a channel failure (for example a caller
// error) is returned without logging so the caller can pass it through.
func (a *Service) attemptUpstreams(ctx context.Context, in upstreamAttemptInput) (upstreamAttempt, error) {
	var (
		resp           *http.Response
		err            error
		responseBody   []byte
		readErr        error
		candidate      = in.candidates[0]
		healthRecorded bool
		timing         *upstreamTiming
	)
	if in.rateLimits == nil {
		in.rateLimits = &rateLimitSnapshot{}
	}
	tried := map[int]bool{}
	retry := func(attempt int) bool {
		a.sticky.invalidate(in.stickyKey, in.stickyBinding)
		if ctx.Err() != nil || attempt+1 >= a.maxAttempts {
			return false
		}
		if in.lazyFallback {
			in.lazyFallback = false
			fallback, leases, lookupErr := a.orderedCandidatesExcluding(ctx, in.auth.UserID, in.req.Model, tried, in.auth.KeyID)
			for id, lease := range leases {
				in.probes[id] = lease
			}
			if lookupErr != nil {
				return false
			}
			remaining := a.maxAttempts - len(in.candidates)
			if len(fallback) > remaining {
				fallback = fallback[:remaining]
			}
			in.candidates = append(in.candidates, fallback...)
		}
		return attempt+1 < len(in.candidates)
	}
	for attempt := 0; attempt < len(in.candidates); attempt++ {
		next := in.candidates[attempt]
		tried[next.ChannelID] = true
		healthRecorded = false
		if ctx.Err() != nil {
			a.logAttemptContextError(ctx, in, candidate, false)
			return upstreamAttempt{}, ctx.Err()
		}
		candidate = next
		stageStart := in.timing.begin()
		rateErr := a.checkChannelRateLimitWithSnapshot(ctx, in.auth, in.req.Model, candidate.ChannelID, in.estimatedTokens, in.rateLimits)
		in.timing.finish("rate_limit", stageStart)
		if err := rateErr; err != nil {
			if errors.Is(err, ErrRateLimited) {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamUnreachable)
				if retry(attempt) {
					continue
				}
				a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", "rate_limited")
			}
			return upstreamAttempt{}, err
		}
		secret, secretErr := a.catalog.GetChannelSecret(ctx, in.auth.UserID, candidate.ChannelID)
		if secretErr != nil {
			return upstreamAttempt{}, secretErr
		}
		upstreamBody, rewriteErr := a.adapter.RewriteRequest(in.req.Body, candidate.UpstreamModel)
		if rewriteErr != nil {
			return upstreamAttempt{}, ErrInvalidRequest
		}
		httpReq, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(secret.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(upstreamBody))
		if requestErr != nil {
			return upstreamAttempt{}, ErrUpstream
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if in.req.Stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		} else {
			httpReq.Header.Set("Accept", "application/json")
		}
		if secret.AuthType == "bearer" && secret.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+secret.APIKey)
		}
		traceCtx, attemptTiming := in.timing.trace(httpReq.Context(), candidate.ChannelID, attempt+1)
		timing = attemptTiming
		if timing != nil {
			httpReq = httpReq.WithContext(traceCtx)
		}
		resp, err = a.client.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				a.logAttemptContextError(ctx, in, candidate, true)
				return upstreamAttempt{}, ctx.Err()
			}
			if reason := catalog.ClassifyUpstreamResult(0, err); reason.CountsAsChannelFailure() {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
				if retry(attempt) {
					continue
				}
			}
			a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", "upstream_unreachable")
			return upstreamAttempt{}, ErrUpstream
		}
		timing.responseHeaders()
		if !in.req.Stream || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			timing.emit(ctx, "response_headers")
		}
		if in.req.Stream && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		responseBody, readErr = io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if ctx.Err() != nil {
				a.logAttemptContextError(ctx, in, candidate, true)
				return upstreamAttempt{}, ctx.Err()
			}
			reason := catalog.ClassifyUpstreamResult(0, readErr)
			if reason.CountsAsChannelFailure() {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
				if retry(attempt) {
					continue
				}
			}
			a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", "upstream_stream_interrupted")
			return upstreamAttempt{}, ErrUpstream
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		if reason := catalog.ClassifyUpstreamResult(resp.StatusCode, nil); reason.CountsAsChannelFailure() {
			a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
			healthRecorded = true
			if retry(attempt) {
				continue
			}
		}
		break
	}
	if catalog.ClassifyUpstreamResult(resp.StatusCode, nil).CountsAsChannelFailure() && len(in.candidates) > 1 {
		a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", fmt.Sprintf("upstream_%d", resp.StatusCode))
		return upstreamAttempt{candidate: candidate}, ErrUpstream
	}
	return upstreamAttempt{resp: resp, body: responseBody, candidate: candidate, healthRecorded: healthRecorded, timing: timing}, nil
}

func (a *Service) logAttemptContextError(ctx context.Context, in upstreamAttemptInput, candidate catalog.RouteCandidate, attempted bool) {
	code := "client_canceled"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = "upstream_timeout"
		if attempted {
			a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamTimeout)
		}
	}
	a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", code)
}
