package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

func TestAdminBreakerConfigRoute(t *testing.T) {
	server := newCatalogServer(storefake.New(), &http.Client{})
	channel, err := server.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "c", BaseURL: "https://c.test", APIKey: "sk", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server.RegisterAdminRoutes(mux)
	path := "/admin/channels/" + strconv.Itoa(channel.ID) + "/breaker"

	body := bytes.NewBufferString(`{"window_seconds":120,"minimum_samples":20,"error_rate_percent":30,"timeout_rate_percent":40,"cooldown_seconds":15}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withOwner(httptest.NewRequest(http.MethodPut, path, body), 1))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, withOwner(httptest.NewRequest(http.MethodGet, path, nil), 1))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			ChannelID       int `json:"channel_id"`
			WindowSeconds   int `json:"window_seconds"`
			CooldownSeconds int `json:"cooldown_seconds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ChannelID != channel.ID || resp.Data.WindowSeconds != 120 || resp.Data.CooldownSeconds != 15 {
		t.Fatalf("data = %+v", resp.Data)
	}
}
