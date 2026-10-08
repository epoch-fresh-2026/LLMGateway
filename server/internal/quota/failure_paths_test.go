package quota

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

var errQuotaStorage = errors.New("quota storage unavailable")

type quotaFailurePort struct {
	Port
	operation string
	err       error
	owned     bool
}

func (p quotaFailurePort) ListQuotaPolicies(_ context.Context, f QuotaPolicyFilter) (ListResponse[QuotaPolicyDTO], error) {
	if f.OwnerUserID != 1 || f.Enabled == nil || *f.Enabled {
		return ListResponse[QuotaPolicyDTO]{}, errQuotaStorage
	}
	return ListResponse[QuotaPolicyDTO]{List: []QuotaPolicyDTO{}}, nil
}
func (p quotaFailurePort) KeyBelongsToUser(context.Context, int, int) (bool, error) {
	return p.owned, p.err
}
func (p quotaFailurePort) InsertQuotaPolicy(context.Context, QuotaPolicy) (int, error) {
	if p.operation == "insert" {
		return 0, p.err
	}
	return 1, nil
}
func (p quotaFailurePort) GetQuotaPolicy(context.Context, int, int) (QuotaPolicy, error) {
	return QuotaPolicy{}, p.err
}
func (p quotaFailurePort) DeleteQuotaPolicy(context.Context, int, int) (bool, error) {
	return false, p.err
}

type quotaFailureTx struct {
	Tx
	operation string
	period    QuotaPeriodType
	empty     bool
}

func (x quotaFailureTx) failure(name string) error {
	if x.operation == name {
		return errQuotaStorage
	}
	return nil
}
func (x quotaFailureTx) ApplicablePolicies(int, int) ([]QuotaPolicy, error) {
	if x.empty {
		return nil, nil
	}
	return []QuotaPolicy{{ID: 1, PeriodType: x.period}}, x.failure("policies")
}
func (x quotaFailureTx) ReapExpired(time.Time, int, int, int) (int, error) {
	return 0, x.failure("reap")
}
func (x quotaFailureTx) InsertReservation(QuotaReservationInsert) (int64, error) {
	return 1, x.failure("reservation")
}
func (x quotaFailureTx) UpsertBucket(int, time.Time, time.Time) error { return x.failure("upsert") }
func (x quotaFailureTx) LockBucket(int, time.Time) error              { return x.failure("lock") }
func (x quotaFailureTx) ReserveBucket(int, time.Time, int64, string) (bool, error) {
	return x.operation != "limit", x.failure("reserve")
}
func (x quotaFailureTx) InsertReservationItem(int64, int, time.Time, int64, string) error {
	return x.failure("item")
}
func (x quotaFailureTx) ReleaseReservation(int64, string, time.Time) error {
	return x.failure("release")
}

type quotaTxManager struct{ tx Tx }

func (m quotaTxManager) InTx(_ context.Context, fn func(Tx) error) error { return fn(m.tx) }
func qp[T any](v T) *T                                                   { return &v }
func validQuotaInput() QuotaPolicyInput {
	return QuotaPolicyInput{PolicyName: qp("daily"), ScopeType: qp("user"), ScopeID: qp(1), PeriodType: qp("day"), TokenLimit: qp(int64(100))}
}

func TestQuotaReservationFailures(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	for _, op := range []string{"policies", "reap", "reservation", "upsert", "lock", "reserve", "item", "limit", "period", "success", "empty"} {
		t.Run(op, func(t *testing.T) {
			x := quotaFailureTx{operation: op, period: QuotaPeriodDay, empty: op == "empty"}
			if op == "period" {
				x.period = "unknown"
			}
			a := New(nil, quotaTxManager{x}, func() time.Time { return now })
			result, err := a.ReserveQuota(ctx, QuotaReserveInput{RequestID: "request", UserID: 1, APIKeyID: 2, EstimatedTokens: 10, EstimatedCost: "1", ExpiresAt: now.Add(time.Hour)})
			want := errQuotaStorage
			if op == "limit" {
				want = ErrQuotaExceeded
			}
			if op == "period" {
				want = ErrInvalid
			}
			if op == "success" || op == "empty" {
				if err != nil {
					t.Fatal(err)
				}
				if op == "success" && (result.ID != 1 || result.EstimatedCost != "1.000000") {
					t.Fatalf("reservation=%+v", result)
				}
				if op == "empty" && result.ID != 0 {
					t.Fatalf("reservation=%+v", result)
				}
				return
			}
			if !errors.Is(err, want) || result.ID != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	a := New(nil, quotaTxManager{quotaFailureTx{operation: "reap"}}, func() time.Time { return now })
	if _, e := a.ReserveQuota(ctx, QuotaReserveInput{}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	in := QuotaReserveInput{RequestID: "request", UserID: 1, APIKeyID: 1, ExpiresAt: now.Add(time.Hour), EstimatedCost: "-1"}
	if _, e := a.ReserveQuota(ctx, in); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := a.ReapExpiredQuotaReservations(ctx, 0); !errors.Is(e, errQuotaStorage) {
		t.Fatal(e)
	}
	if e := a.ReleaseQuota(ctx, 0); e != nil {
		t.Fatal(e)
	}
	a = New(nil, quotaTxManager{quotaFailureTx{operation: "release"}}, nil)
	if e := a.ReleaseQuota(ctx, 1); !errors.Is(e, errQuotaStorage) {
		t.Fatal(e)
	}
	a = New(nil, quotaTxManager{quotaFailureTx{}}, nil)
	if n, e := a.ReapExpiredQuotaReservations(ctx, 1); e != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, e)
	}
}

func TestQuotaPolicyFailures(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		port quotaFailurePort
		key  bool
		want error
	}{{"insert", quotaFailurePort{operation: "insert", err: errQuotaStorage}, false, errQuotaStorage}, {"read", quotaFailurePort{err: errQuotaStorage}, false, errQuotaStorage}, {"key lookup", quotaFailurePort{err: errQuotaStorage}, true, errQuotaStorage}, {"foreign key", quotaFailurePort{}, true, ErrNotFound}} {
		t.Run(tc.name, func(t *testing.T) {
			in := validQuotaInput()
			if tc.key {
				in.ScopeType = qp("api_key")
			}
			a := New(tc.port, nil, nil)
			if _, e := a.CreateQuotaPolicy(ctx, 1, in); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
	a := New(quotaFailurePort{err: errQuotaStorage}, nil, nil)
	if e := a.DeleteQuotaPolicy(ctx, 1, 1); !errors.Is(e, errQuotaStorage) {
		t.Fatal(e)
	}
	a = New(quotaFailurePort{}, nil, nil)
	if e := a.DeleteQuotaPolicy(ctx, 1, 1); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	in := validQuotaInput()
	in.ScopeID = qp(2)
	if _, e := a.CreateQuotaPolicy(ctx, 1, in); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := a.CreateQuotaPolicy(ctx, 1, QuotaPolicyInput{}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*QuotaPolicyInput)
	}{{"name", func(in *QuotaPolicyInput) { in.PolicyName = nil }}, {"scope", func(in *QuotaPolicyInput) { in.ScopeType = qp("other") }}, {"scope id", func(in *QuotaPolicyInput) { in.ScopeID = qp(0) }}, {"period", func(in *QuotaPolicyInput) { in.PeriodType = qp("other") }}, {"limits", func(in *QuotaPolicyInput) { in.TokenLimit = nil }}, {"tokens", func(in *QuotaPolicyInput) { in.TokenLimit = qp(int64(0)) }}, {"cost", func(in *QuotaPolicyInput) { in.CostLimit = qp("bad") }}} {
		t.Run(tc.name, func(t *testing.T) {
			in := validQuotaInput()
			tc.mutate(&in)
			if _, e := NormalizeQuotaPolicy(in); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	in = validQuotaInput()
	in.CostLimit = qp("1.25")
	in.Enabled = qp(false)
	p, e := NormalizeQuotaPolicy(in)
	if e != nil || p.Enabled || *p.CostLimit != "1.250000" {
		t.Fatalf("policy=%+v err=%v", p, e)
	}
	if _, _, e := QuotaPeriodBounds(time.Now(), "other"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}

func TestQuotaHTTPValidation(t *testing.T) {
	a := New(nil, nil, nil)
	mux := http.NewServeMux()
	a.RegisterAdminRoutes(mux)
	for _, tc := range []struct {
		name, method, path, body string
		identity                 bool
		status                   int
	}{
		{"usage method", "POST", "/admin/quota-usage", "", false, 405}, {"policies method", "PUT", "/admin/quota-policies", "", false, 405}, {"policy anonymous", "DELETE", "/admin/quota-policies/1", "", false, 401}, {"policy invalid id", "DELETE", "/admin/quota-policies/nope", "", true, 400}, {"policy method", "GET", "/admin/quota-policies/1", "", true, 405}, {"list anonymous", "GET", "/admin/quota-policies", "", false, 401}, {"usage anonymous", "GET", "/admin/quota-usage", "", false, 401}, {"create anonymous", "POST", "/admin/quota-policies", "{}", false, 401}, {"create json", "POST", "/admin/quota-policies", "{", true, 400},
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
	a = New(quotaFailurePort{}, nil, nil)
	r := httptest.NewRequest("GET", "/admin/quota-policies?enabled=false", nil)
	r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 1}))
	if result := a.listPolicies(r); result.Status != 0 {
		t.Fatal(result)
	}
}
