package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"LLMGateway/server/internal/proxy"

	"github.com/tiktoken-go/tokenizer"
)

// estimateRequestUsage tokenizes the full OpenAI request payload so messages,
// tool definitions and multimodal metadata all contribute to the reservation.
func estimateRequestUsage(body []byte, defaultMaxTokens int) (proxy.EstimatedUsage, error) {
	var request ChatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return proxy.EstimatedUsage{}, err
	}
	output := request.MaxCompletionTokens
	if output <= 0 {
		output = request.MaxTokens
	}
	if output <= 0 {
		output = defaultMaxTokens
	}
	if output <= 0 {
		return proxy.EstimatedUsage{}, fmt.Errorf("default max tokens must be positive")
	}
	choices := request.N
	if choices <= 0 {
		choices = 1
	}
	output *= choices
	encoding, err := tokenizer.ForModel(tokenizer.Model(request.Model))
	if err != nil {
		encoding, err = tokenizer.Get(tokenizer.Cl100kBase)
		if err != nil {
			return proxy.EstimatedUsage{}, fmt.Errorf("load tokenizer: %w", err)
		}
	}
	input, err := encoding.Count(string(body))
	if err != nil {
		return proxy.EstimatedUsage{}, fmt.Errorf("tokenize request: %w", err)
	}
	// Chat framing and provider-side normalization add tokens that are not
	// represented verbatim in JSON. Keep a fixed conservative safety margin.
	input += 16
	prompt := input
	if byteUpperBound := len(body) + 16; byteUpperBound > input {
		input = byteUpperBound
	}
	// Remote image/audio payloads are represented by short URLs but billed from
	// media content. Reserve a conservative budget for each multimodal part.
	media := (strings.Count(string(body), `"image_url"`) + strings.Count(string(body), `"input_audio"`)) * 8192
	input += media
	prompt += media
	return proxy.EstimatedUsage{PromptTokens: prompt, InputTokens: input, OutputTokens: output, TotalTokens: input + output}, nil
}

func countTextTokens(model, text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	encoding, err := tokenizer.ForModel(tokenizer.Model(model))
	if err != nil {
		encoding, err = tokenizer.Get(tokenizer.Cl100kBase)
		if err != nil {
			return 0, fmt.Errorf("load tokenizer: %w", err)
		}
	}
	return encoding.Count(text)
}
