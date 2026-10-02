package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"LLMGateway/server/internal/catalog"
	"LLMGateway/server/internal/testutil/storefake"
)

func TestAdminUserBreakerConfigRoute(t *testing.T) {
	server := newCatalogServer(storefake.New(), &http.Client{})
	mux := http.NewServeMux()
	server.RegisterAdminRoutes(mux)
	path := "/admin/breaker-config"
	request := func(method, body string, owner int) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, withOwner(httptest.NewRequest(method, path, bytes.NewBufferString(body)), owner))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", method, rec.Code, rec.Body.String())
		}
		return rec
	}
	config := func(owner int) (int, int) {
		rec := request(http.MethodGet, "", owner)
		var resp struct {
			Data struct {
				WindowSeconds   int `json:"window_seconds"`
				CooldownSeconds int `json:"cooldown_seconds"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Data.WindowSeconds, resp.Data.CooldownSeconds
	}
	request(http.MethodPut, `{"window_seconds":120,"minimum_samples":20,"error_rate_percent":30,"timeout_rate_percent":40,"cooldown_seconds":15}`, 1)
	if window, cooldown := config(1); window != 120 || cooldown != 15 {
		t.Fatalf("user config = %d/%d", window, cooldown)
	}
	if window, cooldown := config(2); window != 60 || cooldown != 30 {
		t.Fatalf("other user config = %d/%d", window, cooldown)
	}
	request(http.MethodDelete, "", 1)
	if window, cooldown := config(1); window != 60 || cooldown != 30 {
		t.Fatalf("deleted config = %d/%d", window, cooldown)
	}
}

func TestAdminChannelBreakerRouteRemoved(t *testing.T) {
	server := newCatalogServer(storefake.New(), &http.Client{})
	mux := http.NewServeMux()
	server.RegisterAdminRoutes(mux)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			body := `{"window_seconds":120,"minimum_samples":20,"error_rate_percent":30,"timeout_rate_percent":40,"cooldown_seconds":15}`
			mux.ServeHTTP(rec, withOwner(httptest.NewRequest(method, "/admin/channels/1/breaker", bytes.NewBufferString(body)), 1))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, body = %s, want 404", rec.Code, rec.Body.String())
			}
		})
	}
}

type breakerConfigReadStore struct {
	*storefake.Store
	reads map[int]int
}

func (s *breakerConfigReadStore) GetUserBreakerConfigRow(ctx context.Context, owner int) (catalog.ChannelBreakerConfig, bool, error) {
	s.reads[owner]++
	return s.Store.GetUserBreakerConfigRow(ctx, owner)
}

func TestAdminUserBreakerConfigCacheInvalidationIsOwnerScoped(t *testing.T) {
	st := &breakerConfigReadStore{Store: storefake.New(), reads: map[int]int{}}
	server := catalog.New(catalog.Deps{
		Store: st, Health: st, Tx: st.CatalogTx(), Cipher: testCipher(), Client: &http.Client{},
		Now: func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) },
	})
	mux := http.NewServeMux()
	server.RegisterAdminRoutes(mux)
	get := func(owner, window, cooldown int) {
		t.Helper()
		got, err := server.GetUserBreakerConfig(context.Background(), owner)
		if err != nil {
			t.Fatal(err)
		}
		if got.WindowSeconds != window || got.CooldownSeconds != cooldown {
			t.Fatalf("owner %d config = %+v, want window %d cooldown %d", owner, got, window, cooldown)
		}
	}
	get(1, 60, 30)
	get(2, 60, 30)
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		body := `{"window_seconds":120,"minimum_samples":20,"error_rate_percent":30,"timeout_rate_percent":40,"cooldown_seconds":15}`
		mux.ServeHTTP(rec, withOwner(httptest.NewRequest(method, "/admin/breaker-config", bytes.NewBufferString(body)), 1))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", method, rec.Code, rec.Body.String())
		}
		if method == http.MethodPut {
			get(1, 120, 15)
		} else {
			get(1, 60, 30)
		}
		get(2, 60, 30)
		wantReads := 2
		if method == http.MethodDelete {
			wantReads = 3
		}
		if st.reads[1] != wantReads || st.reads[2] != 1 {
			t.Fatalf("after %s reads = %v, want owner 1: %d, owner 2: 1", method, st.reads, wantReads)
		}
	}
}

func TestUserBreakerWindowCountsAreChannelScoped(t *testing.T) {
	st := storefake.New()
	server := catalog.New(catalog.Deps{
		Store: st, Health: st, Tx: st.CatalogTx(), Cipher: testCipher(), Client: &http.Client{},
		Now: func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) },
	})
	ctx := context.Background()
	ids := make([]int, 2)
	for i := range ids {
		channel, err := server.CreateChannel(ctx, 1, catalog.ChannelInput{Name: "test", BaseURL: "https://example.test", APIKey: "placeholder", Status: 1})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = channel.ID
	}
	if _, err := server.UpdateUserBreakerConfig(ctx, 1, catalog.ChannelBreakerConfigInput{WindowSeconds: 60, MinimumSamples: 4, ErrorRatePercent: 50, TimeoutRatePercent: 50, CooldownSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		for i := 0; i < 3; i++ {
			health, err := server.RecordChannelFailure(ctx, id, catalog.FailureUpstreamTimeout)
			if err != nil {
				t.Fatal(err)
			}
			if health.State != catalog.HealthClosed {
				t.Fatalf("channel %d attempt %d state = %s, want closed below minimum samples", id, i+1, health.State)
			}
		}
	}
	health, err := server.RecordChannelFailure(ctx, ids[0], catalog.FailureUpstreamTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if health.State != catalog.HealthOpen || health.FailureCount != 4 {
		t.Fatalf("first channel health = %+v, want open with 4 failures", health)
	}
	health, err = server.GetChannelHealth(ctx, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if health.State != catalog.HealthClosed || health.FailureCount != 3 {
		t.Fatalf("second channel health = %+v, want closed with 3 failures", health)
	}
}
