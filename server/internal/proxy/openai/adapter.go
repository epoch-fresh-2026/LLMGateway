package openai

import (
	"encoding/json"
	"fmt"
	"io"

	"LLMGateway/server/internal/proxy"
)

// Adapter provides OpenAI wire conversion at the protocol boundary.
func Adapter() proxy.ProtocolAdapter {
	return proxy.ProtocolAdapter{
		RewriteRequest:  rewriteRequest,
		ParseUsage:      parseUsage,
		RewriteResponse: rewriteResponse,
		ParseStream: func(reader io.Reader, publicModel string, emit func(proxy.StreamEvent) error) error {
			return parseStream(reader, publicModel, func(event streamEvent) error {
				return emit(proxy.StreamEvent{Frame: event.Frame, Data: event.Data, Done: event.Done, Usage: event.Usage, Text: event.Text})
			})
		},
		StreamError:     streamError,
		EstimateUsage:   estimateRequestUsage,
		CountTextTokens: countTextTokens,
	}
}

func ParseRequest(body []byte) (proxy.ChatRequest, error) {
	var request ChatCompletionRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return proxy.ChatRequest{}, err
	}
	return proxy.ChatRequest{Model: request.Model, Stream: request.Stream, Body: body}, nil
}

func rewriteRequest(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	payload["model"] = upstreamModel
	if stream, _ := payload["stream"].(bool); stream {
		options, ok := payload["stream_options"]
		if !ok {
			options = map[string]any{}
		}
		streamOptions, ok := options.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("stream_options must be an object")
		}
		streamOptions["include_usage"] = true
		payload["stream_options"] = streamOptions
	}
	return json.Marshal(payload)
}

func parseUsage(body []byte) *proxy.Usage {
	var response ChatCompletionResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Usage == nil {
		return nil
	}
	usage := &proxy.Usage{
		PromptTokens:     response.Usage.PromptTokens,
		CompletionTokens: response.Usage.CompletionTokens,
		TotalTokens:      response.Usage.TotalTokens,
	}
	if response.Usage.PromptTokensDetails != nil {
		usage.CachedInputTokens = response.Usage.PromptTokensDetails.CachedTokens
	}
	return usage
}

func rewriteResponse(body []byte, publicModel string) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	payload["model"] = publicModel
	// JSON-decoded values and the replacement model string are always encodable.
	rewritten, _ := json.Marshal(payload)
	return rewritten
}
