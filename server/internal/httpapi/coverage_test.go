package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/proxy"
	"LLMGateway/server/internal/testutil/storefake"
)

func TestAssemblyOptionsAndMaintenanceDelegation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	options := resolveOptions(WithQuotaConfig(123, time.Minute), WithUpstreamTimeout(time.Second), WithUpstreamTimeout(0), WithUpstreamMaxAttempts(2), WithUpstreamMaxAttempts(0), WithMinimumRouteBalance("0.123456"), WithChannelBreakerConfig(catalog.ChannelBreakerConfig{}), WithClock(nil), WithClock(func() time.Time { return now }))
	if options.quotaDefaultMaxTokens != 123 || options.upstreamTimeout != time.Second || options.upstreamMaxAttempts != 2 || options.minimumRouteBalance != "0.123456" || !options.now().Equal(now) {
		t.Fatalf("options=%+v", options)
	}
	s := NewServer(storefake.New(), WithCipher(testCipher()))
	if _, err := s.ReapChannelHealthBuckets(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReapExpiredSessions(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	s.Healthz(res, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if res.Code != 405 {
		t.Fatalf("health status=%d", res.Code)
	}
}

func TestProxyErrorWireMappingAndClientAddress(t *testing.T) {
	resUnknown := httptest.NewRecorder()
	newTestServer().OpenAI(resUnknown, httptest.NewRequest("GET", "/v1/unknown", nil))
	if resUnknown.Code != 404 {
		t.Fatalf("unknown endpoint=%d", resUnknown.Code)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{proxy.ErrUnauthorized, 401, "invalid_api_key"}, {proxy.ErrForbidden, 403, "permission_error"}, {proxy.ErrInvalidRequest, 400, "invalid_request_error"}, {proxy.ErrRateLimited, 429, "rate_limit_exceeded"}, {proxy.ErrQuotaExceeded, 429, "insufficient_quota"}, {proxy.ErrNoHealthyChannel, 503, "no_healthy_channel"}, {proxy.ErrUpstream, 502, "upstream_error"}, {errors.New("secret internal failure"), 500, "internal_error"}} {
		res := httptest.NewRecorder()
		writeProxyError(res, fmt.Errorf("wrapped: %w", tc.err))
		if res.Code != tc.status || !strings.Contains(res.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(res.Body.String(), "secret") {
			t.Fatalf("response=%d %s", res.Code, res.Body.String())
		}
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", " 203.0.113.7 , 10.0.0.1")
	if clientIP(req) != "203.0.113.7" {
		t.Fatal("forwarded address not selected")
	}
	req.Header.Del("X-Forwarded-For")
	req.RemoteAddr = "unix-peer"
	if clientIP(req) != "unix-peer" {
		t.Fatal("fallback address changed")
	}
}

type coverageHTTPPort struct {
	Port
	err error
}

func (p coverageHTTPPort) ListCatalogModels(context.Context, int, bool) (catalog.ListResponse[catalog.CatalogModelDTO], error) {
	return catalog.ListResponse[catalog.CatalogModelDTO]{}, p.err
}

type coverageReadError struct{}

func (coverageReadError) Read([]byte) (int, error) { return 0, errors.New("body disconnected") }
func (coverageReadError) Close() error             { return nil }

type coverageWriteError struct{ *httptest.ResponseRecorder }

func (coverageWriteError) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

func TestOpenAIMethodReadAndModelFailures(t *testing.T) {
	f := newProxyFixture(t, upstreamSuccess())
	for _, path := range []string{"/v1/models", "/v1/chat/completions"} {
		res := httptest.NewRecorder()
		f.server.OpenAI(res, httptest.NewRequest(http.MethodDelete, path, nil))
		if res.Code != 405 {
			t.Fatalf("method status=%d", res.Code)
		}
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Body = coverageReadError{}
	req.Header.Set("Authorization", "Bearer "+f.fullKey)
	res := httptest.NewRecorder()
	f.server.OpenAI(res, req)
	if res.Code != 400 || !strings.Contains(res.Body.String(), "unable to read request body") {
		t.Fatalf("read error=%d %s", res.Code, res.Body.String())
	}
	failPort := coverageHTTPPort{Port: f.store, err: errors.New("database unavailable")}
	s := NewServer(failPort, WithCipher(testCipher()))
	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+f.fullKey)
	res = httptest.NewRecorder()
	s.OpenAI(res, req)
	if res.Code != 500 {
		t.Fatalf("model lookup error=%d %s", res.Code, res.Body.String())
	}
}

func TestHTTPStreamWriteFailureReleasesReservation(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"model\":\"up-gpt\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n")
	}))
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+f.fullKey)
	res := coverageWriteError{httptest.NewRecorder()}
	f.server.OpenAI(res, req)
	if res.Code != 200 || !strings.HasPrefix(res.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream headers=%d %+v", res.Code, res.Header())
	}
	active, err := f.store.CountActiveRateLimitReservations(context.Background(), 1, nil, "gpt", nil)
	if err != nil || active != 0 {
		t.Fatalf("active reservations=%d err=%v", active, err)
	}
}
