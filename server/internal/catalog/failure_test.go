package catalog

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestClassifyUpstreamResult(t *testing.T) {
	tests := []struct {
		name   string
		status int
		err    error
		reason FailureReason
	}{
		{"transport error", 0, errors.New("dial tcp: refused"), FailureUpstreamUnreachable},
		{"timeout", 0, context.DeadlineExceeded, FailureUpstreamTimeout},
		{"429", http.StatusTooManyRequests, nil, FailureUpstream429},
		{"401", http.StatusUnauthorized, nil, FailureUpstream401},
		{"403", http.StatusForbidden, nil, FailureUpstream403},
		{"402", http.StatusPaymentRequired, nil, FailureUpstream402},
		{"500", http.StatusInternalServerError, nil, FailureUpstream5xx},
		{"503", http.StatusServiceUnavailable, nil, FailureUpstream5xx},
		{"400", http.StatusBadRequest, nil, ""},
		{"404", http.StatusNotFound, nil, ""},
		{"422", http.StatusUnprocessableEntity, nil, ""},
		{"200", http.StatusOK, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := ClassifyUpstreamResult(tt.status, tt.err)
			if reason != tt.reason {
				t.Fatalf("ClassifyUpstreamResult(%d, %v) = %q, want %q", tt.status, tt.err, reason, tt.reason)
			}
			if counts := reason.CountsAsChannelFailure(); counts != (tt.reason != "") {
				t.Fatalf("CountsAsChannelFailure(%q) = %v, want %v", reason, counts, tt.reason != "")
			}
		})
	}
}
