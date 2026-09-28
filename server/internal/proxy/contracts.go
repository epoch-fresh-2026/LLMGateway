package proxy

import "io"

// ChatRequest is the protocol-neutral input needed by proxy orchestration.
type ChatRequest struct {
	Model  string
	Stream bool
	Body   []byte
}

// Usage contains the token counts needed for settlement and audit logging.
type Usage struct {
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	CachedInputTokens int
}

type EstimatedUsage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

// Model is a public model visible to an authenticated caller.
type Model struct {
	ID      string
	Created int64
	OwnedBy string
}

// ModelList is the protocol-neutral result of model discovery.
type ModelList struct {
	Models []Model
}

// ChatResponse carries an upstream response without protocol-specific DTOs.
type ChatResponse struct {
	Status int
	Body   []byte
	Usage  *Usage
	Stream ChatStream
}

// ChatStream forwards a successful upstream stream while proxy orchestration
// retains ownership of settlement, audit logging and channel health.
type ChatStream interface {
	Forward(func([]byte) error) error
	Close() error
}

// StreamEvent is a protocol-neutral event emitted by a wire adapter.
type StreamEvent struct {
	Frame []byte
	Data  bool
	Done  bool
	Usage *Usage
	// Text is content successfully forwarded to the downstream caller. It is
	// accumulated for local token estimation only when upstream final usage is
	// unavailable.
	Text string
}

// ProtocolAdapter contains the small protocol seam needed by proxy business
// orchestration. The proxy package owns the contract; adapters own wire rules.
type ProtocolAdapter struct {
	RewriteRequest  func([]byte, string) ([]byte, error)
	ParseUsage      func([]byte) *Usage
	RewriteResponse func([]byte, string) []byte
	ParseStream     func(io.Reader, string, func(StreamEvent) error) error
	StreamError     func(string, string) []byte
	EstimateUsage   func([]byte, int) (EstimatedUsage, error)
	CountTextTokens func(string, string) (int, error)
}
