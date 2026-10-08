package catalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/httpcommon"
)

var errCatalogStorage = errors.New("catalog storage unavailable")

type catalogFailurePort struct {
	Port
	operation string
	found     bool
	record    ChannelRecord
}

func (p catalogFailurePort) failure(op string) error {
	if p.operation == op {
		return errCatalogStorage
	}
	return nil
}
func (p catalogFailurePort) GetChannelDTO(context.Context, int, int) (ChannelDTO, error) {
	return ChannelDTO{ID: 1}, p.failure("dto")
}
func (p catalogFailurePort) GetChannelOwner(context.Context, int) (int, error) {
	return 1, p.failure("owner")
}
func (p catalogFailurePort) GetChannelRecord(context.Context, int, int) (ChannelRecord, error) {
	return p.record, p.failure("record")
}
func (p catalogFailurePort) InsertChannel(context.Context, ChannelInsert) (int, error) {
	return 1, p.failure("insert")
}
func (p catalogFailurePort) UpdateChannelRecord(context.Context, int, int, ChannelUpdate) (bool, error) {
	return p.found, p.failure("update")
}
func (p catalogFailurePort) UpdateChannelStatusRecord(context.Context, int, int, int) (bool, error) {
	return p.found, p.failure("status")
}
func (p catalogFailurePort) DeleteChannel(context.Context, int, int) (bool, error) {
	return p.found, p.failure("delete")
}
func (p catalogFailurePort) GetChannelModelByID(context.Context, int, int) (ChannelModel, bool, error) {
	return ChannelModel{ID: 1, ModelName: "old", UpstreamModel: "real"}, p.found, p.failure("model")
}
func (p catalogFailurePort) ChannelModelExists(context.Context, int, int, string) (bool, error) {
	return p.operation == "duplicate", p.failure("exists")
}
func (p catalogFailurePort) ChannelUpstreamExists(context.Context, int, int, string) (bool, error) {
	return p.found, p.failure("upstream")
}
func (p catalogFailurePort) DeleteChannelModel(context.Context, int, int) (bool, error) {
	return p.found, p.failure("delete model")
}
func (p catalogFailurePort) ListChannelModels(context.Context, int, int) (ListResponse[ChannelModel], error) {
	return ListResponse[ChannelModel]{}, p.failure("list models")
}
func (p catalogFailurePort) UpsertPricingRecord(_ context.Context, in PricingRecord) (PricingDTO, error) {
	return PricingDTO{ChannelID: in.ChannelID, UpstreamModel: in.UpstreamModel, Currency: in.Currency}, nil
}

type catalogFailureTx struct {
	Tx
	operation string
	found     bool
	balance   string
	model     ChannelModel
}

func (x catalogFailureTx) failure(op string) error {
	if x.operation == op {
		return errCatalogStorage
	}
	return nil
}
func (x catalogFailureTx) LockChannel(int, int) error { return x.failure("lock") }
func (x catalogFailureTx) GetChannelBalanceText(int, int) (string, error) {
	return x.balance, x.failure("read balance")
}
func (x catalogFailureTx) UpdateChannelBalance(int, int, string) (bool, error) {
	return x.found, x.failure("write balance")
}
func (x catalogFailureTx) UpdateChannelModelRecord(int, int, string, bool) (ChannelModel, bool, error) {
	return x.model, x.found, x.failure("write model")
}
func (x catalogFailureTx) DeleteChannelProbe(int) error         { return x.failure("probe") }
func (x catalogFailureTx) DeleteChannelHealthBuckets(int) error { return x.failure("buckets") }
func (x catalogFailureTx) ResetChannelHealthState(int) error    { return x.failure("reset") }
func (x catalogFailureTx) UpsertUserBreakerConfig(int, ChannelBreakerConfig) error {
	return x.failure("config upsert")
}
func (x catalogFailureTx) DeleteUserBreakerConfig(int) error { return x.failure("config delete") }
func (x catalogFailureTx) EnsureChannelHealth(int) error     { return x.failure("ensure") }
func (x catalogFailureTx) GetChannelHealthForUpdate(int) (ChannelHealth, error) {
	return NewChannelHealth(1), x.failure("read health")
}
func (x catalogFailureTx) UpsertChannelHealthBucket(int, time.Time, int64, int64, int64) error {
	return x.failure("bucket")
}
func (x catalogFailureTx) GetChannelHealthWindow(int, time.Time) (ChannelHealthWindow, error) {
	return ChannelHealthWindow{}, x.failure("window")
}
func (x catalogFailureTx) UpdateChannelHealth(ChannelHealth) (bool, error) {
	return x.found, x.failure("write health")
}

type catalogTxManager struct{ tx Tx }

func (m catalogTxManager) InTx(_ context.Context, fn func(Tx) error) error { return fn(m.tx) }

type catalogFailureHealth struct {
	HealthPort
	operation string
}

func (p catalogFailureHealth) GetUserBreakerConfigRow(context.Context, int) (ChannelBreakerConfig, bool, error) {
	if p.operation == "config" {
		return ChannelBreakerConfig{}, false, errCatalogStorage
	}
	return ChannelBreakerConfig{}, false, nil
}
func (p catalogFailureHealth) GetChannelHealthRow(context.Context, int) (ChannelHealth, bool, error) {
	return NewChannelHealth(1), true, errCatalogStorage
}
func (p catalogFailureHealth) ListChannelHealthRows(context.Context, int) ([]ChannelHealth, error) {
	return nil, errCatalogStorage
}
func (p catalogFailureHealth) DeleteStaleChannelHealthBuckets(context.Context, time.Time) (int, error) {
	return 1, nil
}
func cp[T any](v T) *T { return &v }
func catalogServer(p catalogFailurePort, x catalogFailureTx) *Server {
	return New(Deps{Store: p, Health: catalogFailureHealth{}, Tx: catalogTxManager{x}})
}

func TestCatalogChannelFailureContracts(t *testing.T) {
	ctx := context.Background()
	cipher, e := crypto.NewCipher([]byte("0123456789abcdef"))
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, op string
		found    bool
		fn       func(*Server) error
		want     error
	}{
		{"insert", "insert", true, func(a *Server) error { _, e := a.CreateChannel(ctx, 1, ChannelInput{APIKey: "secret"}); return e }, errCatalogStorage},
		{"create balance", "", true, func(a *Server) error {
			_, e := a.CreateChannel(ctx, 1, ChannelInput{APIKey: "secret", Balance: cp("bad")})
			return e
		}, ErrInvalid},
		{"update balance", "", true, func(a *Server) error { _, e := a.UpdateChannel(ctx, 1, 1, ChannelInput{Balance: cp("bad")}); return e }, ErrInvalid},
		{"update", "update", true, func(a *Server) error { _, e := a.UpdateChannel(ctx, 1, 1, ChannelInput{}); return e }, errCatalogStorage},
		{"status", "status", true, func(a *Server) error { _, e := a.UpdateChannelStatus(ctx, 1, 1, 1); return e }, errCatalogStorage},
		{"status missing", "", false, func(a *Server) error { _, e := a.UpdateChannelStatus(ctx, 1, 1, 1); return e }, ErrNotFound},
		{"delete", "delete", true, func(a *Server) error { return a.DeleteChannel(ctx, 1, 1) }, errCatalogStorage},
		{"balance empty", "", true, func(a *Server) error { _, e := a.UpdateChannelBalance(ctx, 1, 1, "", ""); return e }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{operation: tc.op, found: tc.found}, catalogFailureTx{})
			a.cipher = cipher
			if e := tc.fn(a); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
	for _, method := range []string{"create", "update"} {
		t.Run("cipher missing "+method, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{}, catalogFailureTx{})
			in := ChannelInput{APIKey: "secret"}
			var e error
			if method == "create" {
				_, e = a.CreateChannel(ctx, 1, in)
			} else {
				_, e = a.UpdateChannel(ctx, 1, 1, in)
			}
			if e == nil {
				t.Fatal("missing cipher accepted")
			}
		})
	}
	a := catalogServer(catalogFailurePort{found: true}, catalogFailureTx{})
	a.cipher = cipher
	if _, e := a.UpdateChannel(ctx, 1, 1, ChannelInput{APIKey: "secret"}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, op, balance, set, delta string
		found                         bool
		want                          error
	}{
		{"lock", "lock", "1", "1", "", true, errCatalogStorage}, {"read", "read balance", "1", "1", "", true, errCatalogStorage}, {"stored malformed", "", "bad", "1", "", true, ErrInvalid}, {"set malformed", "", "1", "bad", "", true, ErrInvalid}, {"delta malformed", "", "1", "", "bad", true, ErrInvalid}, {"write", "write balance", "1", "1", "", true, errCatalogStorage}, {"missing", "", "1", "1", "", false, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{}, catalogFailureTx{operation: tc.op, found: tc.found, balance: tc.balance})
			if _, e := a.UpdateChannelBalance(ctx, 1, 1, tc.set, tc.delta); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
	for _, tc := range []struct {
		name, ciphertext string
		cipher           bool
	}{{"cipher absent", "encoded", false}, {"ciphertext invalid", "invalid", true}} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{record: ChannelRecord{APIKeyCiphertext: tc.ciphertext}}, catalogFailureTx{})
			if tc.cipher {
				a.cipher = cipher
			}
			if value, e := a.GetChannelSecret(ctx, 1, 1); e == nil || value != nil {
				t.Fatalf("channel=%v err=%v", value, e)
			}
		})
	}
}

func TestCatalogModelFailureContracts(t *testing.T) {
	ctx := context.Background()
	valid := ChannelModel{ModelName: "alias", UpstreamModel: "real"}
	for _, tc := range []struct {
		name, op string
		found    bool
		method   string
		want     error
	}{{"create owner", "dto", true, "create", errCatalogStorage}, {"create lookup", "exists", true, "create", errCatalogStorage}, {"create duplicate", "duplicate", true, "create", ErrInvalid}, {"update owner", "dto", true, "update", errCatalogStorage}, {"update model lookup", "model", true, "update", errCatalogStorage}, {"update missing", "", false, "update", ErrNotFound}, {"update rename lookup", "exists", true, "update", errCatalogStorage}, {"update rename duplicate", "duplicate", true, "update", ErrInvalid}, {"delete owner", "dto", true, "delete", errCatalogStorage}, {"delete write", "delete model", true, "delete", errCatalogStorage}, {"delete missing", "", false, "delete", ErrNotFound}} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{operation: tc.op, found: tc.found}, catalogFailureTx{})
			var e error
			switch tc.method {
			case "create":
				_, e = a.CreateChannelModel(ctx, 1, 1, valid)
			case "update":
				_, e = a.UpdateChannelModel(ctx, 1, 1, 1, "new", true)
			case "delete":
				e = a.DeleteChannelModel(ctx, 1, 1, 1)
			}
			if !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
	for _, model := range []ChannelModel{{UpstreamModel: "real"}, {ModelName: "alias"}} {
		if _, e := catalogServer(catalogFailurePort{}, catalogFailureTx{}).CreateChannelModel(ctx, 1, 1, model); !errors.Is(e, ErrInvalid) {
			t.Fatal(e)
		}
	}
	for _, tc := range []struct {
		name, op string
		found    bool
		want     error
	}{{"write", "write model", true, errCatalogStorage}, {"missing", "", false, ErrNotFound}, {"success", "", true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{found: true}, catalogFailureTx{operation: tc.op, found: tc.found, model: valid})
			m, e := a.UpdateChannelModel(ctx, 1, 1, 1, "", true)
			if !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
			if e == nil && m.ModelName != "alias" {
				t.Fatalf("model=%+v", m)
			}
		})
	}
	for _, op := range []string{"dto", "upstream"} {
		a := catalogServer(catalogFailurePort{operation: op, found: true}, catalogFailureTx{})
		if _, e := a.UpsertPricing(ctx, 1, PricingInput{ChannelID: 1, UpstreamModel: "real"}); !errors.Is(e, errCatalogStorage) {
			t.Fatal(e)
		}
	}
	a := catalogServer(catalogFailurePort{operation: "dto"}, catalogFailureTx{})
	if e := a.DeletePricing(ctx, 1, DeletePricingInput{ChannelID: 1}); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	for _, tc := range []struct{ name, input, output, cached string }{{"output", "1", "bad", "1"}, {"cached", "1", "1", "bad"}} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{found: true}, catalogFailureTx{})
			if _, e := a.UpsertPricing(ctx, 1, PricingInput{ChannelID: 1, UpstreamModel: "real", InputPricePer1M: tc.input, OutputPricePer1M: tc.output, CachedInputPricePer1M: tc.cached}); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	a = catalogServer(catalogFailurePort{found: true}, catalogFailureTx{})
	if e := a.DeleteChannelModel(ctx, 1, 1, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := a.UpsertPricing(ctx, 1, PricingInput{}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	if _, e := a.UpsertPricing(ctx, 1, PricingInput{ChannelID: 1, UpstreamModel: "real", InputPricePer1M: "bad"}); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	price, e := a.UpsertPricing(ctx, 1, PricingInput{ChannelID: 1, UpstreamModel: "real", InputPricePer1M: "1", OutputPricePer1M: "2"})
	if e != nil || price.Currency != "USD" {
		t.Fatalf("price=%+v err=%v", price, e)
	}
}

func TestCatalogHealthFailureContracts(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"ensure", "read health", "bucket", "window", "write health", "missing"} {
		t.Run(op, func(t *testing.T) {
			x := catalogFailureTx{operation: op, found: op != "missing"}
			a := catalogServer(catalogFailurePort{}, x)
			value, e := a.RecordChannelFailure(ctx, 1, FailureUpstreamTimeout)
			want := errCatalogStorage
			if op == "missing" {
				want = ErrNotFound
			}
			if !errors.Is(e, want) || value.ChannelID != 0 {
				t.Fatalf("health=%+v err=%v", value, e)
			}
		})
	}
	a := catalogServer(catalogFailurePort{}, catalogFailureTx{operation: "bucket"})
	if _, e := a.RecordChannelSuccess(ctx, 1); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	for _, op := range []string{"probe", "buckets"} {
		a := catalogServer(catalogFailurePort{}, catalogFailureTx{operation: op})
		if e := a.ResetChannelHealth(ctx, 1); !errors.Is(e, errCatalogStorage) {
			t.Fatal(e)
		}
	}
	a = catalogServer(catalogFailurePort{}, catalogFailureTx{})
	if _, e := a.GetChannelHealth(ctx, 1); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	if _, e := a.ListChannelHealth(ctx, 1); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	if _, e := a.RecordChannelFailure(ctx, 1, ""); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	cfg := ChannelBreakerConfigInput{WindowSeconds: 60, MinimumSamples: 10, CooldownSeconds: 30, ErrorRatePercent: 50, TimeoutRatePercent: 50}
	a = catalogServer(catalogFailurePort{}, catalogFailureTx{operation: "config upsert"})
	if _, e := a.UpdateUserBreakerConfig(ctx, 1, cfg); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	a = catalogServer(catalogFailurePort{}, catalogFailureTx{operation: "config delete"})
	if e := a.DeleteUserBreakerConfig(ctx, 1); !errors.Is(e, errCatalogStorage) {
		t.Fatal(e)
	}
	cfg.WindowSeconds = 0
	if _, e := a.UpdateUserBreakerConfig(ctx, 1, cfg); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	cfg.WindowSeconds = 60
	cfg.ErrorRatePercent = 101
	if _, e := a.UpdateUserBreakerConfig(ctx, 1, cfg); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	a = catalogServer(catalogFailurePort{operation: "owner"}, catalogFailureTx{})
	if got := a.breakerFor(ctx, 1); got != a.breaker {
		t.Fatal(got)
	}
	a.health = catalogFailureHealth{operation: "config"}
	if got := a.baseBreakerFor(ctx, 1); got != a.breaker {
		t.Fatal(got)
	}
	now := time.Now()
	a.breakerCache[1] = breakerConfigCacheEntry{config: ChannelBreakerConfig{WindowSeconds: 99}, expires: now.Add(time.Hour)}
	if got := a.loadBaseBreaker(ctx, 1, now); got.WindowSeconds != 99 {
		t.Fatal(got)
	}
	if n, e := a.ReapChannelHealthBuckets(ctx, 0); n != 1 || e != nil {
		t.Fatalf("n=%d err=%v", n, e)
	}
	if got := ResolveChannelBreakerConfig(a.breaker, nil); got != a.breaker {
		t.Fatal(got)
	}
	if got := ResolveChannelBreakerConfig(a.breaker, &ChannelBreakerConfig{FailureThreshold: 9}); got.FailureThreshold != 9 {
		t.Fatal(got)
	}
	opened := "malformed"
	h := ChannelHealth{State: HealthOpen, OpenedAt: &opened}
	if got := EvaluateChannelHealth(h, now, a.breaker); got.State != HealthOpen {
		t.Fatal(got)
	}
}

type catalogTransport func(*http.Request) (*http.Response, error)

func (f catalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCatalogProbeFailureContracts(t *testing.T) {
	a := catalogServer(catalogFailurePort{operation: "record"}, catalogFailureTx{})
	r := httptest.NewRequest("POST", "/", strings.NewReader("{"))
	if result := a.TestChannel(r, 1, 1); result.Status != 400 {
		t.Fatal(result)
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	if result := a.TestChannel(r, 1, 1); result.Status != 500 {
		t.Fatal(result)
	}
	a = catalogServer(catalogFailurePort{operation: "list models"}, catalogFailureTx{})
	r = httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	if result := a.TestChannel(r, 1, 1); result.Status != 500 {
		t.Fatal(result)
	}
	a.ConfigureTestTimeout(0)
	if item := a.testModel(context.Background(), &Channel{BaseURL: "://invalid"}, ChannelModel{}); item.Error != "unable to create test request" {
		t.Fatal(item)
	}
	a.client = &http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) { return nil, errCatalogStorage })}
	if item := a.testModel(context.Background(), &Channel{BaseURL: "http://upstream"}, ChannelModel{}); item.Error != "upstream request failed" {
		t.Fatal(item)
	}
	for _, tc := range []struct {
		name, url, body string
		status          int
		operation       string
		wantStatus      int
		wantOK          bool
	}{
		{"missing record", "http://upstream", "{}", 200, "record", 500, false}, {"bad request", "://invalid", "{}", 200, "", 0, false}, {"non success", "http://upstream", "{}", 500, "", 0, false}, {"bad json", "http://upstream", "{", 200, "", 0, false}, {"models shape", "http://upstream", "{\"models\":[{\"id\":\"real\"}]}", 200, "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := catalogServer(catalogFailurePort{operation: tc.operation, record: ChannelRecord{BaseURL: tc.url}}, catalogFailureTx{})
			a.client = &http.Client{Transport: catalogTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			result := a.remoteModels(context.Background(), 1, 1)
			if result.Status != tc.wantStatus {
				t.Fatal(result)
			}
			if result.Status == 0 && result.Data.(map[string]any)["ok"] != tc.wantOK {
				t.Fatal(result)
			}
		})
	}
}

func TestCatalogPricingFailureContracts(t *testing.T) {
	for _, tc := range []struct{ name, input, output, cached string }{{"output", "1", "bad", "1"}, {"cached", "1", "1", "bad"}} {
		t.Run("actual "+tc.name, func(t *testing.T) {
			if _, e := ComputeCost(tc.input, tc.output, tc.cached, 1, 1, 1); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
	for _, tc := range []struct{ name, input, output, cached string }{{"input", "bad", "1", "1"}, {"output", "1", "bad", "1"}, {"cached", "1", "1", "bad"}, {"overflow", "40000000000", "0", "0"}} {
		t.Run("estimate "+tc.name, func(t *testing.T) {
			if _, e := EstimateReservationCost(tc.input, tc.output, tc.cached, 3, 1); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
		})
	}
}

func TestCatalogHTTPBoundaryValidation(t *testing.T) {
	a := catalogServer(catalogFailurePort{operation: "record"}, catalogFailureTx{})
	mux := http.NewServeMux()
	a.RegisterAdminRoutes(mux)
	for _, tc := range []struct {
		path, method, other string
		json                bool
	}{
		{"/admin/channels", "GET", "DELETE", true}, {"/admin/channels/1", "PUT", "GET", true}, {"/admin/channels/1/status", "PUT", "GET", true}, {"/admin/channels/1/balance", "PUT", "GET", true}, {"/admin/channels/1/health", "GET", "POST", false}, {"/admin/channels/1/health/reset", "POST", "GET", false}, {"/admin/channels/health", "GET", "POST", false}, {"/admin/models", "GET", "POST", false}, {"/admin/channels/1/test", "POST", "GET", true}, {"/admin/channels/1/remote-models", "POST", "GET", false}, {"/admin/channels/1/models", "POST", "DELETE", true}, {"/admin/channels/1/models/1", "PUT", "GET", true}, {"/admin/breaker-config", "PUT", "POST", true}, {"/admin/pricing", "POST", "PUT", true},
	} {
		t.Run(tc.path, func(t *testing.T) {
			for _, c := range []struct {
				name, method, body string
				identity           bool
				status             int
			}{{"anonymous", tc.method, "{}", false, 401}, {"method", tc.other, "{}", true, 405}} {
				t.Run(c.name, func(t *testing.T) { catalogHTTPStatus(t, mux, c.method, tc.path, c.body, c.identity, c.status) })
			}
			if tc.json {
				method := tc.method
				if tc.path == "/admin/channels" {
					method = "POST"
				}
				catalogHTTPStatus(t, mux, method, tc.path, "{", true, 400)
			}
			if strings.Contains(tc.path, "/1") {
				path := strings.Replace(tc.path, "/1", "/bad", 1)
				catalogHTTPStatus(t, mux, tc.method, path, "{}", true, 400)
			}
		})
	}
	catalogHTTPStatus(t, mux, "PUT", "/admin/channels/1/models/bad", "{}", true, 400)
	catalogHTTPStatus(t, mux, "DELETE", "/admin/pricing", "{", true, 400)
	catalogHTTPStatus(t, mux, "GET", "/admin/channels/1/health", "", true, 500)
	catalogHTTPStatus(t, mux, "POST", "/admin/channels/1/health/reset", "", true, 500)
	a = catalogServer(catalogFailurePort{}, catalogFailureTx{})
	r := httptest.NewRequest("GET", "/admin/channels/1/health", nil)
	r.SetPathValue("id", "1")
	r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 1}))
	if result := a.channelHealth(r); result.Status != 500 {
		t.Fatal(result)
	}
}
func catalogHTTPStatus(t *testing.T, mux *http.ServeMux, method, path, body string, identity bool, status int) {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if identity {
		r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 1}))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s status=%d body=%s", method, path, w.Code, w.Body.String())
	}
}
