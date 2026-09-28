package proxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	domain "LLMGateway/server/internal/testutil/testtypes"
)

func streamAdapter(parseStream func(io.Reader, string, func(StreamEvent) error) error) ProtocolAdapter {
	return ProtocolAdapter{
		RewriteRequest: func(body []byte, model string) ([]byte, error) { return body, nil },
		EstimateUsage: func([]byte, int) (EstimatedUsage, error) {
			return EstimatedUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}, nil
		},
		ParseStream: parseStream,
		StreamError: func(code, message string) []byte { return []byte("data: error\n\n") },
	}
}

func streamTransport() roundTripFunc {
	return func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("stream")), Header: make(http.Header)}, nil
	}
}

func TestCompletionStreamSettlesOnceWithTTFT(t *testing.T) {
	adapter := streamAdapter(func(reader io.Reader, publicModel string, emit func(StreamEvent) error) error {
		if err := emit(StreamEvent{Frame: []byte("data: {\"chunk\":1}\n\n"), Data: true, Text: "hi", Usage: &Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}}); err != nil {
			return err
		}
		return emit(StreamEvent{Done: true, Frame: []byte("data: [DONE]\n\n")})
	})
	service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	if response.Stream == nil {
		t.Fatal("expected a stream response")
	}
	defer response.Stream.Close()
	var frames []byte
	if err := response.Stream.Forward(func(frame []byte) error { frames = append(frames, frame...); return nil }); err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if !strings.Contains(string(frames), "[DONE]") {
		t.Fatalf("frames = %q, want terminal DONE", frames)
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].Status != "success" || logs.List[0].TTFTMs == nil {
		t.Fatalf("usage logs = %+v, want one success with TTFT", logs)
	}
}

func TestCompletionStreamInterruptedChargesForwardedText(t *testing.T) {
	adapter := streamAdapter(func(reader io.Reader, publicModel string, emit func(StreamEvent) error) error {
		if err := emit(StreamEvent{Frame: []byte("data: {\"chunk\":1}\n\n"), Data: true, Text: "hello world"}); err != nil {
			return err
		}
		return ErrInvalidStream
	})
	adapter.CountTextTokens = func(model, text string) (int, error) { return 3, nil }
	service, st, auth := newProtocolSeamService(t, streamTransport(), adapter)
	response, err := service.ChatCompletions(context.Background(), auth, ChatRequest{Model: "public-model", Stream: true, Body: []byte(`{"model":"public-model"}`)}, "127.0.0.1")
	if err != nil {
		t.Fatalf("ChatCompletions: %v", err)
	}
	defer response.Stream.Close()
	if err := response.Stream.Forward(func([]byte) error { return nil }); err == nil {
		t.Fatal("Forward should surface the stream interruption")
	}
	logs, err := st.ListUsageLogs(context.Background(), 1, domain.UsageLogFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 1 || logs.List[0].ErrorCode != "partial_estimated_upstream_stream_protocol_error" || logs.List[0].TotalTokens <= 0 {
		t.Fatalf("partial usage log = %+v", logs)
	}
}
