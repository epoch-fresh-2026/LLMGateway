package openai

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"LLMGateway/server/internal/proxy"
	"github.com/tiktoken-go/tokenizer"
)

type failingCodec struct {
	tokenizer.Codec
	err error
}

func (c failingCodec) Count(string) (int, error) { return 0, c.err }

func TestTokenizerFailuresAndInvalidInputs(t *testing.T) {
	sentinel := errors.New("tokenizer unavailable")
	for _, tc := range []struct {
		name string
		body string
		max  int
	}{
		{"invalid JSON", "{", 10}, {"invalid default", `{"model":"gpt"}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := estimateRequestUsage([]byte(tc.body), tc.max); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	lookup := func(tokenizer.Model) (tokenizer.Codec, error) { return nil, sentinel }
	fallback := func(tokenizer.Encoding) (tokenizer.Codec, error) { return nil, sentinel }
	if _, err := estimateRequestUsageWithTokenizer([]byte(`{"model":"gpt"}`), 10, lookup, fallback); !errors.Is(err, sentinel) {
		t.Fatalf("fallback error=%v", err)
	}
	if _, err := countTextTokensWithTokenizer("gpt", "hello", lookup, fallback); !errors.Is(err, sentinel) {
		t.Fatalf("text error=%v", err)
	}
	badCount := func(tokenizer.Model) (tokenizer.Codec, error) { return failingCodec{err: sentinel}, nil }
	if _, err := estimateRequestUsageWithTokenizer([]byte(`{"model":"gpt"}`), 10, badCount, fallback); !errors.Is(err, sentinel) {
		t.Fatalf("count error=%v", err)
	}
	if n, err := countTextTokens("unknown", "hello"); err != nil || n <= 0 {
		t.Fatalf("fallback count=%d,%v", n, err)
	}
	if n, err := countTextTokens("unknown", ""); err != nil || n != 0 {
		t.Fatalf("empty count=%d,%v", n, err)
	}
}

func TestRequestAndResponseRoundTripBoundaries(t *testing.T) {
	for _, body := range []string{"{", `{"stream":true,"stream_options":null}`, `{"stream":true,"stream_options":[]}`} {
		if _, err := rewriteRequest([]byte(body), "up"); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	invalid := []byte("not JSON")
	if got := rewriteResponse(invalid, "public"); !bytes.Equal(got, invalid) {
		t.Fatalf("invalid response changed: %s", got)
	}
	body := []byte(`{"model":"up","nested":[null,true,"你好",{"x":1.25}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	got := rewriteResponse(body, "public")
	if !strings.Contains(string(got), `"model":"public"`) || parseUsage(got).TotalTokens != 5 || !strings.Contains(string(got), `"nested":[null,true,"你好",{"x":1.25}]`) {
		t.Fatalf("roundtrip lost payload: %s", got)
	}
	event, err := parseEvent(append(append([]byte("data: "), body...), '\n', '\n'), "public")
	if err != nil || !event.Data || event.Usage.TotalTokens != 5 {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}

func TestStreamEOFAndEmissionBoundaries(t *testing.T) {
	sentinel := errors.New("downstream closed")
	for _, tc := range []struct {
		name, input string
		emitError   bool
		want        error
		done        bool
	}{
		{"delimited emit", "data: {}\n\n", true, sentinel, false},
		{"tail malformed", "data: {", false, proxy.ErrInvalidStream, false},
		{"tail emit", "data: {}", true, sentinel, false},
		{"tail done", "data: [DONE]", false, nil, true},
		{"empty data", "data\n\n", false, nil, false},
		{"tail data", "data: {}", false, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []streamEvent
			err := parseStream(strings.NewReader(tc.input), "public", func(e streamEvent) error {
				events = append(events, e)
				if tc.emitError {
					return sentinel
				}
				return nil
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
			if tc.want == nil && (len(events) != 1 || events[0].Done != tc.done) {
				t.Fatalf("events=%+v", events)
			}
		})
	}
	if err := parseStream(strings.NewReader(strings.Repeat("x", maxStreamEventBytes+1)), "p", func(streamEvent) error { return nil }); !errors.Is(err, proxy.ErrInvalidStream) {
		t.Fatalf("oversized event error=%v", err)
	}
	if err := parseStream(&errorReader{sentinel}, "p", func(streamEvent) error { return nil }); !errors.Is(err, sentinel) {
		t.Fatalf("reader error=%v", err)
	}
}

type errorReader struct{ err error }

func (r *errorReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = (*errorReader)(nil)
