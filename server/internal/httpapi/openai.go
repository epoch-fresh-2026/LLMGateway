package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"LLMGateway/server/internal/proxy"
	openaiwire "LLMGateway/server/internal/proxy/openai"
)

const maxChatRequestBodyBytes = 32 << 20

// OpenAI dispatches the OpenAI-compatible downstream endpoints.
func (a *Server) OpenAI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch path {
	case "/v1/models":
		a.handleModels(w, r)
	case "/v1/chat/completions":
		a.handleChatCompletions(w, r)
	default:
		writeOpenAIError(w, http.StatusNotFound, "not_found", "unknown endpoint")
	}
}

func (a *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	auth, err := a.proxy.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeProxyError(w, err)
		return
	}
	models, err := a.proxy.Models(r.Context(), auth)
	if err != nil {
		writeProxyError(w, err)
		return
	}
	data := make([]openaiwire.OpenAIModel, 0, len(models.Models))
	for _, model := range models.Models {
		data = append(data, openaiwire.OpenAIModel{ID: model.ID, Object: "model", Created: model.Created, OwnedBy: model.OwnedBy})
	}
	writeJSON(w, http.StatusOK, openaiwire.OpenAIModelList{Object: "list", Data: data})
}

func (a *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	auth, err := a.proxy.Authenticate(r.Context(), r.Header.Get("Authorization"))
	if err != nil {
		writeProxyError(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChatRequestBodyBytes))
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request_body_too_large", fmt.Sprintf("request body exceeds the %d byte (32 MiB) limit", maxChatRequestBodyBytes))
			return
		}
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "unable to read request body")
		return
	}

	req, err := openaiwire.ParseRequest(body)
	if err != nil {
		writeProxyError(w, proxy.ErrInvalidRequest)
		return
	}
	response, err := a.proxy.ChatCompletions(r.Context(), auth, req, clientIP(r))
	if err != nil {
		writeProxyError(w, err)
		return
	}
	if response.Stream != nil {
		defer response.Stream.Close()
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(response.Status)
		controller := http.NewResponseController(w)
		_ = response.Stream.Forward(func(frame []byte) error {
			if _, err := w.Write(frame); err != nil {
				return err
			}
			return controller.Flush()
		})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}

// writeProxyError maps proxy errors to OpenAI-compatible HTTP responses.
func writeProxyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, proxy.ErrUnauthorized):
		writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "invalid or missing API key")
	case errors.Is(err, proxy.ErrForbidden):
		writeOpenAIError(w, http.StatusForbidden, "permission_error", "user or key is not allowed to perform this request")
	case errors.Is(err, proxy.ErrInvalidRequest):
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid request")
	case errors.Is(err, proxy.ErrRateLimited):
		writeOpenAIError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded")
	case errors.Is(err, proxy.ErrQuotaExceeded):
		writeOpenAIError(w, http.StatusTooManyRequests, "insufficient_quota", "period quota exceeded")
	case errors.Is(err, proxy.ErrNoHealthyChannel):
		writeOpenAIError(w, http.StatusServiceUnavailable, "no_healthy_channel", "no healthy channel available for the requested model")
	case errors.Is(err, proxy.ErrUpstream):
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "upstream request failed")
	default:
		writeOpenAIError(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}

func writeOpenAIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, openaiwire.OpenAIError{Error: openaiwire.OpenAIErrorBody{Message: message, Type: code, Code: code}})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
