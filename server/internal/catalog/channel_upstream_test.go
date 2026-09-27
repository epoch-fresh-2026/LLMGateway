package catalog_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

// TestRemoteModelsCancellationReachesUpstream verifies that cancelling the
// admin request context aborts the upstream /v1/models call instead of waiting
// for the client timeout.
func TestRemoteModelsCancellationReachesUpstream(t *testing.T) {
	started := make(chan struct{})
	server := newCatalogServer(storefake.New(), &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	channel, err := server.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "remote", BaseURL: "https://upstream.test", APIKey: "sk-secret", AuthType: "bearer", Status: 1})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	server.RegisterAdminRoutes(mux)

	ctx, cancel := context.WithCancel(context.Background())
	req := withOwner(httptest.NewRequest(http.MethodPost, "/admin/channels/"+strconv.Itoa(channel.ID)+"/remote-models", nil).WithContext(ctx), 1)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rec, req)
		close(done)
	}()

	<-started
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("remote-models did not return after cancellation")
	}
	if !strings.Contains(rec.Body.String(), `"ok":false`) {
		t.Fatalf("expected canceled remote-models result, got %s", rec.Body.String())
	}
}
