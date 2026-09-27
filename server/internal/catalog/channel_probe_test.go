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
	"LLMGateway/server/internal/crypto"
	"LLMGateway/server/internal/httpcommon"
	"LLMGateway/server/internal/testutil/storefake"
	domain "LLMGateway/server/internal/testutil/testtypes"
)

// withOwner injects the authenticated identity the session middleware would
// normally set on admin requests.
func withOwner(req *http.Request, owner int) *http.Request {
	return req.WithContext(httpcommon.WithIdentity(req.Context(), httpcommon.Identity{UserID: owner}))
}

const testEncryptionKey = "0123456789abcdef0123456789abcdef"

func testCipher() *crypto.Cipher {
	cipher, err := crypto.NewCipher([]byte(testEncryptionKey))
	if err != nil {
		panic(err)
	}
	return cipher
}

func newCatalogServer(st *storefake.Store, client *http.Client) *catalog.Server {
	return catalog.New(catalog.Deps{
		Store:  st,
		Health: st,
		Tx:     st.CatalogTx(),
		Cipher: testCipher(),
		Client: client,
	})
}

func TestChannelTestTimeoutReturnsSafeError(t *testing.T) {
	st := storefake.New()
	server := newCatalogServer(st, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})})
	channel, err := server.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "slow", BaseURL: "https://upstream.test", APIKey: "sk-secret", AuthType: "bearer", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.CreateChannelModel(context.Background(), 1, channel.ID, domain.ChannelModel{ModelName: "public", UpstreamModel: "upstream", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server.ConfigureTestTimeout(10 * time.Millisecond)
	req := httptest.NewRequest(http.MethodPost, "/admin/channels/1/test", bytes.NewBufferString(`{}`))
	result := server.TestChannel(req, 1, channel.ID)
	item := result.Data.(domain.ChannelTestResultDTO).List[0]
	if item.OK || item.HTTPStatus != 0 || item.Error != "upstream request timed out" || item.LatencyMs <= 0 {
		t.Fatalf("timeout item = %+v", item)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChannelTestCheckAllFalseOnlyTestsFirstEnabledModel(t *testing.T) {
	var models []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		models = append(models, body.Model)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	st := storefake.New()
	server := newCatalogServer(st, &http.Client{})
	channel, err := server.CreateChannel(context.Background(), 1, domain.ChannelInput{Name: "test", BaseURL: upstream.URL, APIKey: "sk", AuthType: "bearer", Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []domain.ChannelModel{{ModelName: "first", UpstreamModel: "first-upstream", Enabled: true}, {ModelName: "second", UpstreamModel: "second-upstream", Enabled: true}} {
		if _, err := server.CreateChannelModel(context.Background(), 1, channel.ID, model); err != nil {
			t.Fatal(err)
		}
	}
	checkAll := false
	body, _ := json.Marshal(map[string]bool{"check_all": checkAll})
	req := httptest.NewRequest(http.MethodPost, "/admin/channels/1/test", bytes.NewReader(body))
	result := server.TestChannel(req, 1, channel.ID)
	items := result.Data.(domain.ChannelTestResultDTO).List
	if len(items) != 1 || len(models) != 1 || models[0] != "first-upstream" {
		t.Fatalf("items/models = %+v/%+v", items, models)
	}
}
