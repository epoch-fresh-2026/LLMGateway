package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"LLMGateway/server/internal/proxy"
	openaiwire "LLMGateway/server/internal/proxy/openai"
)

func TestChatCompletionsLargeRequestBody(t *testing.T) {
	for _, size := range []int{(1 << 20) + 1, (8 << 20) + 1, 32 << 20} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("bytes_%d/stream_%t", size, stream), func(t *testing.T) {
				// Padding fixes the exact byte count independently of JSON fields.
				content := "history start " + strings.Repeat("word ", 200000) + " history end"
				if size == 32<<20 {
					content = "history at the exact byte limit"
				}
				payload := fmt.Sprintf(`{"model":"gpt","stream":%t,"messages":[{"role":"user","content":%q}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"extra":{"keep":"tail"}}`, stream, content)
				padding := size - len(payload)
				body := strings.Repeat(" ", padding) + payload
				var calls atomic.Int32
				f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					var got struct {
						Model    string `json:"model"`
						Stream   bool   `json:"stream"`
						Messages []struct {
							Content string `json:"content"`
						} `json:"messages"`
						Tools []struct {
							Function struct {
								Name string `json:"name"`
							} `json:"function"`
						} `json:"tools"`
						Extra struct {
							Keep string `json:"keep"`
						} `json:"extra"`
						StreamOptions struct {
							IncludeUsage bool `json:"include_usage"`
						} `json:"stream_options"`
					}
					if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
						t.Errorf("decode upstream request: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if got.Model != "up-gpt" || got.Stream != stream || len(got.Messages) != 1 || got.Messages[0].Content != content || len(got.Tools) != 1 || got.Tools[0].Function.Name != "lookup" || got.Extra.Keep != "tail" {
						t.Error("upstream request fields were changed or truncated")
					}
					if stream {
						if !got.StreamOptions.IncludeUsage {
							t.Error("missing stream usage option")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: {\"model\":\"up-gpt\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n")
						return
					}
					_, _ = fmt.Fprint(w, `{"model":"up-gpt","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
				}))
				// Byte-limit tests retain the wire adapter but avoid tokenizing
				// synthetic padding; tokenizer behavior is covered in openai tests.
				adapter := openaiwire.Adapter()
				adapter.EstimateUsage = func([]byte, int) (proxy.EstimatedUsage, error) {
					return proxy.EstimatedUsage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2}, nil
				}
				f.server.proxy = proxy.NewServiceWithConfig(proxy.Config{Store: f.store, Catalog: f.catalog, Quota: f.quota, RateLimit: f.ratelimit, Client: f.server.client, RandIntN: func(int) int { return 0 }, Now: time.Now, Adapter: adapter})
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
				if stream {
					req.ContentLength = -1
					req.TransferEncoding = []string{"chunked"}
				}
				req.Header.Set("Authorization", "Bearer "+f.fullKey)
				res := httptest.NewRecorder()
				f.server.OpenAI(res, req)
				if res.Code != http.StatusOK || calls.Load() != 1 {
					t.Fatalf("status=%d calls=%d body=%s", res.Code, calls.Load(), res.Body.String())
				}
				if stream && (!strings.HasPrefix(res.Header().Get("Content-Type"), "text/event-stream") || !strings.HasSuffix(res.Body.String(), "data: [DONE]\n\n")) {
					t.Fatal("stream response missing SSE content type or DONE")
				}
			})
		}
	}
}

func TestChatCompletionsRejectedRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name          string
		body          string
		unknownLength bool
		status        int
		code          string
	}{
		{name: "over_limit", body: strings.Repeat(" ", (32<<20)+1), status: http.StatusRequestEntityTooLarge, code: "request_body_too_large"},
		{name: "over_limit_unknown_length", body: strings.Repeat(" ", (32<<20)+1), unknownLength: true, status: http.StatusRequestEntityTooLarge, code: "request_body_too_large"},
		{name: "invalid_json", body: `{"model":`, status: http.StatusBadRequest, code: "invalid_request_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
			if tc.unknownLength {
				req.ContentLength = -1
				req.TransferEncoding = []string{"chunked"}
			}
			req.Header.Set("Authorization", "Bearer "+f.fullKey)
			res := httptest.NewRecorder()
			f.server.OpenAI(res, req)
			var response openaiwire.OpenAIError
			if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if res.Code != tc.status || response.Error.Code != tc.code || response.Error.Type != tc.code || calls.Load() != 0 {
				t.Fatalf("status=%d calls=%d body=%s", res.Code, calls.Load(), res.Body.String())
			}
			if tc.status == http.StatusRequestEntityTooLarge && !strings.Contains(response.Error.Message, "33554432 byte") {
				t.Fatalf("missing byte limit: %s", response.Error.Message)
			}
		})
	}
}
