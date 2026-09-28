package catalog

import (
	"context"
	"errors"
	"net/http"
)

// FailureReason describes why an upstream channel attempt failed. The values
// are shared by the proxy (which classifies upstream results) and the circuit
// breaker (which decides whether to open), so a rename is a compile error
// instead of a silent behaviour change. The zero value means the outcome must
// not be attributed to the channel (e.g. caller errors and client cancellation).
type FailureReason string

const (
	FailureUpstreamUnreachable FailureReason = "upstream_unreachable"
	FailureUpstream401         FailureReason = "upstream_401"
	FailureUpstream402         FailureReason = "upstream_402"
	FailureUpstream403         FailureReason = "upstream_403"
	FailureUpstream429         FailureReason = "upstream_429"
	FailureUpstream5xx         FailureReason = "upstream_5xx"
	FailureUpstreamProtocol    FailureReason = "upstream_protocol_error"
	FailureUpstreamTimeout     FailureReason = "upstream_timeout"
)

// CountsAsChannelFailure reports whether the outcome is attributable to the
// channel. It is the single source of truth for the breaker decision; the zero
// value is the explicit "not a channel failure" sentinel.
func (r FailureReason) CountsAsChannelFailure() bool {
	return r != ""
}

// ClassifyUpstreamResult maps an upstream outcome to a channel failure reason,
// or the zero value when the outcome must not count against the channel. Only
// transport errors, upstream 429/401/403/402 and 5xx are penalised; other client
// errors (400/404/...) pass through without tripping the breaker, so a bad
// caller cannot open a healthy channel. Callers use
// FailureReason.CountsAsChannelFailure as the single decision point.
func ClassifyUpstreamResult(statusCode int, err error) FailureReason {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return FailureUpstreamTimeout
		}
		return FailureUpstreamUnreachable
	}
	switch statusCode {
	case http.StatusTooManyRequests:
		return FailureUpstream429
	case http.StatusUnauthorized:
		return FailureUpstream401
	case http.StatusForbidden:
		return FailureUpstream403
	case http.StatusPaymentRequired:
		return FailureUpstream402
	}
	if statusCode >= 500 && statusCode <= 599 {
		return FailureUpstream5xx
	}
	return ""
}

// IsDeterministic reports whether the failure will not recover on retry, so the
// breaker should open immediately instead of counting toward the threshold.
func (r FailureReason) IsDeterministic() bool {
	switch r {
	case FailureUpstream401, FailureUpstream402, FailureUpstream403:
		return true
	default:
		return false
	}
}
