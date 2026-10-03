package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	domain "LLMGateway/server/internal/testutil/testtypes"
)

func captureTiming(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	old := slog.Default()
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &output
}

func TestTimingDisabled(t *testing.T) {
	output := captureTiming(t, slog.LevelInfo)
	timing := newRequestTiming(context.Background(), "req_test", time.Now())
	if timing != nil {
		t.Fatal("timing enabled at INFO")
	}
	ctx, trace := timing.trace(context.Background(), 1, 1)
	trace.responseHeaders()
	trace.emit(ctx, "response_headers")
	if output.Len() != 0 {
		t.Fatal("unexpected log")
	}
}

func TestTimingFirstFrameAndTTFT(t *testing.T) {
	output := captureTiming(t, slog.LevelDebug)
	clock := time.Now()
	adapter := streamAdapter(func(_ io.Reader, _ string, emit func(StreamEvent) error) error {
		if err := emit(StreamEvent{Frame: []byte(": heartbeat\n\n")}); err != nil {
			return err
		}
		if output.Len() != 0 {
			t.Fatal("heartbeat logged as data")
		}
		clock = clock.Add(37 * time.Millisecond)
		if err := emit(StreamEvent{Data: true, Frame: []byte("data: body-secret\n\n"), Usage: &Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}); err != nil {
			return err
		}
		if output.Len() == 0 {
			t.Fatal("log delayed until stream end")
		}
		if err := emit(StreamEvent{Data: true, Frame: []byte("data: second\n\n")}); err != nil {
			return err
		}
		return emit(StreamEvent{Done: true})
	})
	service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
	service.now = func() time.Time { return clock }
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte("request-secret"), Stream: true}, "ip-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Stream.Close()
	if output.Len() != 0 {
		t.Fatal("stream logged before first data")
	}
	if err := response.Stream.Forward(func(frame []byte) error {
		if strings.Contains(string(frame), "body-secret") && output.Len() == 0 {
			t.Fatal("first frame log not available at emit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "proxy_latency") != 1 {
		t.Fatalf("unexpected logs: %s", output)
	}
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"request_id", "channel_id", "attempt", "preflight_estimate_ms", "preflight_rate_limit_ms", "preflight_route_ms", "preflight_quota_ms", "response_headers_ms", "first_data_frame_ms", "chat_to_first_data_frame_ms"} {
		if _, ok := record[field]; !ok {
			t.Errorf("missing %s", field)
		}
	}
	for _, secret := range []string{"body-secret", "request-secret", "ip-secret", "public-model", "upstream", "Authorization", "Bearer", "http://"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("leaked %s", secret)
		}
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil || logs.Total != 1 || logs.List[0].TTFTMs == nil || *logs.List[0].TTFTMs != 37 {
		t.Fatalf("TTFT changed: %+v, %v", logs, err)
	}
}

type timingCheckedBody struct {
	t      *testing.T
	output *bytes.Buffer
	io.Reader
}

func (b *timingCheckedBody) Read(p []byte) (int, error) {
	if !strings.Contains(b.output.String(), `"event":"response_headers"`) {
		b.t.Fatal("response headers log delayed until body read")
	}
	return b.Reader.Read(p)
}

func (b *timingCheckedBody) Close() error { return nil }

func TestTimingBufferedHeadersBeforeBody(t *testing.T) {
	output := captureTiming(t, slog.LevelDebug)
	service, st, auth := newProtocolSeamService(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: &timingCheckedBody{t: t, output: output, Reader: strings.NewReader(`{}`)}}, nil
	}), seamAdapter())
	if _, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Body: []byte(`{}`)}, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "first_data_frame_ms") {
		t.Fatal("buffered response has first frame metric")
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil || logs.Total != 1 || logs.List[0].TTFTMs != nil {
		t.Fatalf("nonstream TTFT changed: %+v, %v", logs, err)
	}
}

func TestTimingHTTPTraceConnectionReuse(t *testing.T) {
	output := captureTiming(t, slog.LevelDebug)
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()
	client := server.Client()
	defer client.CloseIdleConnections()
	for i := 0; i < 2; i++ {
		timing := newRequestTiming(context.Background(), "req_test", time.Now())
		ctx, trace := timing.trace(context.Background(), 7, i+1)
		if i == 0 {
			callbacks := httptrace.ContextClientTrace(ctx)
			callbacks.DNSStart(httptrace.DNSStartInfo{Host: "dns-secret"})
			callbacks.DNSDone(httptrace.DNSDoneInfo{Addrs: []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}})
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/url-secret", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer key-secret")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		trace.responseHeaders()
		trace.emit(ctx, "response_headers")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	for _, secret := range []string{server.URL, "url-secret", "key-secret", "Authorization", "127.0.0.1", "dns-secret", "192.0.2.1"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("trace leaked %s", secret)
		}
	}
	decoder := json.NewDecoder(output)
	for i := 0; i < 2; i++ {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record["connection_reused"] != (i == 1) {
			t.Fatalf("reuse record: %+v", record)
		}
		if i == 0 {
			for _, field := range []string{"dns_ms", "tcp_ms", "tls_ms", "response_headers_ms"} {
				if _, ok := record[field]; !ok {
					t.Errorf("missing %s", field)
				}
			}
		} else if _, ok := record["tcp_ms"]; ok {
			t.Fatal("reused connection reported TCP dial")
		}
	}
}
