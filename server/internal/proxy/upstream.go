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

// upstreamAttemptInput bundles the request-scoped context needed by the
// candidate failover loop. candidates is already truncated to maxAttempts.
type upstreamAttemptInput struct {
	requestID       string
	auth            *accounts.AuthContext
	req             ChatRequest
	candidates      []catalog.RouteCandidate
	estimatedTokens int64
	start           time.Time
	clientIP        string
}

// upstreamAttempt is the outcome of the candidate failover loop.
type upstreamAttempt struct {
	resp           *http.Response
	body           []byte
	candidate      catalog.RouteCandidate
	healthRecorded bool
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
	)
	for attempt, next := range in.candidates {
		if ctx.Err() != nil {
			return upstreamAttempt{}, ctx.Err()
		}
		candidate = next
		if err := a.checkChannelRateLimit(ctx, in.auth, in.req.Model, candidate.ChannelID, in.estimatedTokens); err != nil {
			if errors.Is(err, ErrRateLimited) {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamUnreachable)
				if attempt+1 < len(in.candidates) {
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
		resp, err = a.client.Do(httpReq)
		if err != nil {
			if ctx.Err() != nil {
				return upstreamAttempt{}, ctx.Err()
			}
			if reason := catalog.ClassifyUpstreamResult(0, err); reason.CountsAsChannelFailure() {
				a.recordChannelHealth(ctx, candidate.ChannelID, false, reason)
				if attempt+1 < len(in.candidates) {
					continue
				}
			}
			a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", "upstream_unreachable")
			return upstreamAttempt{}, ErrUpstream
		}
		if in.req.Stream && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}
		responseBody, readErr = io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if ctx.Err() != nil {
				return upstreamAttempt{}, ctx.Err()
			}
			a.recordChannelHealth(ctx, candidate.ChannelID, false, catalog.FailureUpstreamUnreachable)
			if attempt+1 < len(in.candidates) {
				continue
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
			if attempt+1 < len(in.candidates) {
				continue
			}
		}
		break
	}
	if resp == nil {
		return upstreamAttempt{}, ErrUpstream
	}
	if catalog.ClassifyUpstreamResult(resp.StatusCode, nil).CountsAsChannelFailure() && len(in.candidates) > 1 {
		a.logUsage(ctx, in.requestID, in.auth, &candidate.ChannelID, candidate.UpstreamModel, in.req.Model, nil, "0.000000", "", "", elapsedMs(in.start, a.now()), in.clientIP, "error", fmt.Sprintf("upstream_%d", resp.StatusCode))
		return upstreamAttempt{candidate: candidate}, ErrUpstream
	}
	return upstreamAttempt{resp: resp, body: responseBody, candidate: candidate, healthRecorded: healthRecorded}, nil
}
