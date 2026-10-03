package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

func TestEstimateRequestUsageUsesTokenCountForReservation(t *testing.T) {
	encoding, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content any
		media   int
	}{
		{"unicode", strings.Repeat("你好世界，测试令牌估算。", 1024), 0},
		{"large JSON", strings.Repeat(`{"name":"repeated value","enabled":true}`, 4096), 0},
		{"multimodal", []any{
			map[string]any{"type": "text", "text": "describe the media"},
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": "https://example.com/image.png"}},
			map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": "AAAA", "format": "wav"}},
		}, 4 * 8192},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"model":    "unknown-model",
				"messages": []any{map[string]any{"role": "user", "content": tt.content}},
			})
			if err != nil {
				t.Fatal(err)
			}
			count, err := encoding.Count(string(body))
			if err != nil {
				t.Fatal(err)
			}
			estimate, err := estimateRequestUsage(body, 40)
			if err != nil {
				t.Fatal(err)
			}
			if estimate.PromptTokens != count+16+tt.media {
				t.Fatalf("prompt tokens = %d, want %d", estimate.PromptTokens, count+16+tt.media)
			}
			if estimate.InputTokens != count+16+tt.media {
				t.Fatalf("estimate = %+v, want input %d", estimate, count+16+tt.media)
			}
			if estimate.TotalTokens != estimate.InputTokens+estimate.OutputTokens {
				t.Fatalf("total does not preserve reservation: %+v", estimate)
			}
		})
	}
}

func TestEstimateRequestUsageUsesTokenizerAndMaxTokenPriority(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		defaultMax int
		wantOutput int
	}{
		{"max completion tokens", `{"model":"gpt","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"max_completion_tokens":30}`, 40, 30},
		{"legacy max tokens", `{"model":"gpt","messages":[{"role":"user","content":"hello"}],"max_tokens":20}`, 40, 20},
		{"configured default", `{"model":"gpt","messages":[{"role":"user","content":"hello"}]}`, 40, 40},
		{"multiple choices", `{"model":"gpt","messages":[{"role":"user","content":"hello"}],"max_tokens":20,"n":3}`, 40, 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			estimate, err := estimateRequestUsage([]byte(tt.body), tt.defaultMax)
			if err != nil {
				t.Fatal(err)
			}
			if estimate.InputTokens <= 0 {
				t.Fatalf("input tokens = %d, want tokenizer estimate > 0", estimate.InputTokens)
			}
			if estimate.OutputTokens != tt.wantOutput || estimate.TotalTokens != estimate.InputTokens+tt.wantOutput {
				t.Fatalf("estimate = %+v, want output %d", estimate, tt.wantOutput)
			}
		})
	}
}
