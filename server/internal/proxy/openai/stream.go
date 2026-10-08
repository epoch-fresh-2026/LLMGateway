package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"LLMGateway/server/internal/proxy"
)

const maxStreamEventBytes = 8 << 20

type streamEvent struct {
	Frame []byte
	Data  bool
	Done  bool
	Usage *proxy.Usage
	Text  string
}

func parseStream(reader io.Reader, publicModel string, emit func(streamEvent) error) error {
	buffered := bufio.NewReader(reader)
	var event bytes.Buffer
	for {
		line, err := buffered.ReadString('\n')
		if len(line) > 0 {
			if event.Len()+len(line) > maxStreamEventBytes {
				return fmt.Errorf("%w: SSE event exceeds %d bytes", proxy.ErrInvalidStream, maxStreamEventBytes)
			}
			event.WriteString(line)
			if line == "\n" || line == "\r\n" {
				parsed, parseErr := parseEvent(event.Bytes(), publicModel)
				if parseErr != nil {
					return parseErr
				}
				if err := emit(parsed); err != nil {
					return err
				}
				if parsed.Done {
					return nil
				}
				event.Reset()
			}
		}
		if err != nil {
			if err == io.EOF {
				if event.Len() > 0 {
					parsed, parseErr := parseEvent(event.Bytes(), publicModel)
					if parseErr != nil {
						return parseErr
					}
					if err := emit(parsed); err != nil {
						return err
					}
					if parsed.Done {
						return nil
					}
				}
				return nil
			}
			return err
		}
	}
}

func parseEvent(frame []byte, publicModel string) (streamEvent, error) {
	normalized := normalizeEvent(frame)
	data := eventData(normalized)
	if data == "" {
		return streamEvent{Frame: normalized}, nil
	}
	if strings.TrimSpace(data) == "[DONE]" {
		return streamEvent{Frame: []byte("data: [DONE]\n\n"), Done: true}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return streamEvent{}, fmt.Errorf("%w: invalid JSON data frame", proxy.ErrInvalidStream)
	}
	payload["model"] = publicModel
	// The payload contains only decoded JSON values and the public model string.
	rewritten, _ := json.Marshal(payload)
	usage := parseUsage(rewritten)
	return streamEvent{Frame: append(append([]byte("data: "), rewritten...), '\n', '\n'), Data: true, Usage: usage, Text: deltaText(payload)}, nil
}

func deltaText(payload map[string]any) string {
	choices, _ := payload["choices"].([]any)
	var text strings.Builder
	for _, choice := range choices {
		item, _ := choice.(map[string]any)
		delta, _ := item["delta"].(map[string]any)
		content, _ := delta["content"].(string)
		text.WriteString(content)
	}
	return text.String()
}

func normalizeEvent(frame []byte) []byte {
	text := strings.ReplaceAll(string(frame), "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n\n") + "\n\n"
	return []byte(text)
}

func eventData(frame []byte) string {
	var values []string
	for _, line := range strings.Split(string(frame), "\n") {
		if line == "data" {
			values = append(values, "")
			continue
		}
		if strings.HasPrefix(line, "data:") {
			values = append(values, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	return strings.Join(values, "\n")
}

func streamError(code, message string) []byte {
	payload, _ := json.Marshal(OpenAIError{Error: OpenAIErrorBody{Message: message, Type: code, Code: code}})
	return append(append([]byte("data: "), payload...), '\n', '\n')
}
