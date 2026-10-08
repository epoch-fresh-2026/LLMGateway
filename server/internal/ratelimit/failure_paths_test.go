package ratelimit

import (
	"LLMGateway/server/internal/httpcommon"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var errRateStorage = errors.New("rate storage unavailable")

type rateFailurePort struct {
	Port
	operation string
	err       error
	found     bool
}

func (p rateFailurePort) ListRateLimits(_ context.Context, owner int, enabled *bool, page, size int) (ListResponse[RateLimitRuleDTO], error) {
	if owner != 1 || enabled == nil || *enabled {
		return ListResponse[RateLimitRuleDTO]{}, errRateStorage
	}
	return ListResponse[RateLimitRuleDTO]{List: []RateLimitRuleDTO{}}, nil
}
func (p rateFailurePort) failure(op string) error {
	if p.operation == op {
		return p.err
	}
	return nil
}
func (p rateFailurePort) TargetOwnedByUser(context.Context, int, string, string) (bool, error) {
	return p.found, p.failure("target")
}
func (p rateFailurePort) InsertRateLimit(context.Context, int, RateLimitRule) (int, error) {
	return 1, p.failure("insert")
}
func (p rateFailurePort) UpdateRateLimitEnabled(context.Context, int, int, bool) (bool, error) {
	return p.found, p.failure("update")
}
func (p rateFailurePort) GetRateLimit(context.Context, int, int) (RateLimitRule, error) {
	return RateLimitRule{ID: 1}, p.failure("get")
}
func (p rateFailurePort) DeleteRateLimit(context.Context, int, int) (bool, error) {
	return p.found, p.failure("delete")
}
func (p rateFailurePort) InsertRateLimitReservation(context.Context, RateLimitReservationInput) (int64, error) {
	return 1, p.failure("reserve")
}
func (p rateFailurePort) FinalizeRateLimitReservation(context.Context, int64) (bool, error) {
	return p.found, p.failure("finalize")
}
func (p rateFailurePort) ReleaseRateLimitReservation(context.Context, int64) (bool, error) {
	return p.found, p.failure("release")
}
func (p rateFailurePort) ReapRateLimitReservations(context.Context, int) (int, error) { return 2, nil }
func (p rateFailurePort) CountActiveRateLimitReservations(context.Context, int, *int, string, *int) (int64, error) {
	return 3, nil
}
func rp[T any](v T) *T { return &v }
func validRateInput() RateLimitInput {
	return RateLimitInput{RuleName: rp("per minute"), TargetType: rp("user"), Metric: rp("rpm"), LimitValue: rp(int64(10)), Action: rp("reject")}
}

func TestRateLimitStorageFailures(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	for _, tc := range []struct {
		name, operation string
		found           bool
		fn              func(*Server) error
		want            error
	}{
		{"target lookup", "target", true, func(a *Server) error { _, e := a.CreateRateLimit(ctx, 1, validRateInput()); return e }, errRateStorage},
		{"target foreign", "", false, func(a *Server) error { _, e := a.CreateRateLimit(ctx, 1, validRateInput()); return e }, ErrInvalid},
		{"insert", "insert", true, func(a *Server) error { _, e := a.CreateRateLimit(ctx, 1, validRateInput()); return e }, errRateStorage},
		{"update", "update", true, func(a *Server) error {
			_, e := a.UpdateRateLimit(ctx, 1, 1, RateLimitUpdateInput{Enabled: rp(true)})
			return e
		}, errRateStorage},
		{"update missing", "", false, func(a *Server) error {
			_, e := a.UpdateRateLimit(ctx, 1, 1, RateLimitUpdateInput{Enabled: rp(true)})
			return e
		}, ErrNotFound},
		{"get", "get", true, func(a *Server) error {
			_, e := a.UpdateRateLimit(ctx, 1, 1, RateLimitUpdateInput{Enabled: rp(true)})
			return e
		}, errRateStorage},
		{"delete", "delete", true, func(a *Server) error { return a.DeleteRateLimit(ctx, 1, 1) }, errRateStorage},
		{"delete missing", "", false, func(a *Server) error { return a.DeleteRateLimit(ctx, 1, 1) }, ErrNotFound},
		{"reserve", "reserve", true, func(a *Server) error {
			_, e := a.ReserveRateLimit(ctx, RateLimitReservationInput{RequestID: "r", UserID: 1, APIKeyID: 2, ExpiresAt: now.Add(time.Hour)})
			return e
		}, errRateStorage},
		{"finalize", "finalize", true, func(a *Server) error { return a.FinalizeRateLimit(ctx, 1, 1) }, errRateStorage},
		{"finalize missing", "", false, func(a *Server) error { return a.FinalizeRateLimit(ctx, 1, 1) }, ErrNotFound},
		{"release", "release", true, func(a *Server) error { return a.ReleaseRateLimit(ctx, 1) }, errRateStorage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New(rateFailurePort{operation: tc.operation, err: errRateStorage, found: tc.found}, func() time.Time { return now })
			if e := tc.fn(a); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
	a := New(rateFailurePort{}, nil)
	if _, e := a.ReserveRateLimit(ctx, RateLimitReservationInput{}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if e := a.ReleaseRateLimit(ctx, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if n, e := a.CountActiveRateLimitReservations(ctx, 1, nil, "model", nil); e != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, e)
	}
	if n, e := a.ReapRateLimitReservations(ctx, 1); e != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, e)
	}
}

func TestRateLimitNormalizationEdges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*RateLimitInput)
	}{{"name", func(in *RateLimitInput) { in.RuleName = rp("") }}, {"target", func(in *RateLimitInput) { in.TargetType = rp("other") }}, {"metric", func(in *RateLimitInput) { in.Metric = rp("other") }}, {"action", func(in *RateLimitInput) { in.Action = rp("other") }}, {"limit", func(in *RateLimitInput) { in.LimitValue = rp(int64(0)) }}} {
		t.Run(tc.name, func(t *testing.T) {
			in := validRateInput()
			tc.mutate(&in)
			if _, e := NormalizeRateLimit(in); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	in := validRateInput()
	in.TargetValue = rp("")
	in.Priority = rp(1)
	in.Enabled = rp(false)
	in.Extras = []byte(`{"burst":2}`)
	rule, e := NormalizeRateLimit(in)
	if e != nil || rule.TargetValue != "*" || rule.Enabled || rule.Priority != 1 || string(rule.Extras) != string(in.Extras) {
		t.Fatalf("rule=%+v err=%v", rule, e)
	}
	rule, e = NormalizeRateLimit(validRateInput())
	if e != nil || string(rule.Extras) != "{}" {
		t.Fatalf("rule=%+v err=%v", rule, e)
	}
	for _, tc := range []struct {
		metric string
		want   int64
	}{{"rpm", 1}, {"tpm", 2}, {"concurrency", 3}, {"other", 0}} {
		t.Run(tc.metric, func(t *testing.T) {
			v, ok := (Overrides{RPM: 1, TPM: 2, Concurrency: 3}).ForRule(RateLimitRuleDTO{TargetType: "api_key", Metric: tc.metric})
			if v != tc.want || ok != (tc.want > 0) {
				t.Fatalf("override=%d/%v", v, ok)
			}
		})
	}
	now := time.Unix(100, 0)
	for _, tc := range []struct {
		name   string
		window int64
		start  time.Time
		want   int64
	}{{"nonpositive", 0, now, 4}, {"future", 60, now.Add(time.Second), 14}, {"expired", 60, now.Add(-time.Hour), 4}} {
		t.Run(tc.name, func(t *testing.T) {
			if n := SlidingWindowCount(10, 4, tc.window, now, tc.start); n != tc.want {
				t.Fatalf("count=%d", n)
			}
		})
	}
}

func TestRateLimitHTTPValidation(t *testing.T) {
	a := New(nil, nil)
	mux := http.NewServeMux()
	a.RegisterAdminRoutes(mux)
	for _, tc := range []struct {
		name, method, path, body string
		identity                 bool
		status                   int
	}{
		{"list anonymous", "GET", "/admin/rate-limits", "", false, 401}, {"create anonymous", "POST", "/admin/rate-limits", "{}", false, 401}, {"item anonymous", "PUT", "/admin/rate-limits/1", "{}", false, 401}, {"invalid id", "PUT", "/admin/rate-limits/nope", "{}", true, 400}, {"list method", "DELETE", "/admin/rate-limits", "", true, 405}, {"item method", "GET", "/admin/rate-limits/1", "", true, 405}, {"create json", "POST", "/admin/rate-limits", "{", true, 400}, {"update json", "PUT", "/admin/rate-limits/1", "{", true, 400}, {"update unknown", "PUT", "/admin/rate-limits/1", "{\"enabled\":true,\"other\":1}", true, 400}, {"update trailing", "PUT", "/admin/rate-limits/1", "{\"enabled\":true} {}", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.identity {
				r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 1}))
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	a = New(rateFailurePort{}, nil)
	r := httptest.NewRequest("GET", "/admin/rate-limits?enabled=false", nil)
	r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 1}))
	if result := a.listRateLimits(r); result.Status != 0 {
		t.Fatal(result)
	}
}
