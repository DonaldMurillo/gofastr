package provider

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
)

// Provider is the interface every LLM transport implements: Chat streams a completion on a channel
// that closes when the stream ends, Models lists the catalog, and TokenCount estimates msgs' tokens.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *Request) (<-chan StreamEvent, error)
	Models(ctx context.Context) ([]Model, error)
	TokenCount(ctx context.Context, model string, msgs []Message) (int, error)
}

// Request is a provider-agnostic model invocation; a zero Temperature or MaxTokens means the provider default.
type Request struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []ToolSchema
	Temperature float64
	MaxTokens   int
	CacheHints  []CacheBreakpoint
	Extra       map[string]any
}

// Message is a canonical Anthropic-shape message (role + content blocks).
type Message struct {
	Role    Role
	Content []control.ContentBlock
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ToolSchema is the tool catalog entry surfaced to the model; InputSchema is JSON Schema.
type ToolSchema struct {
	Name        string
	Description string
	InputSchema []byte
}

// CacheBreakpoint marks the Request.Messages index where a prompt cache should be anchored.
type CacheBreakpoint struct {
	MessageIndex int
	Marker       string
}

// Model describes a model the provider exposes.
type Model struct {
	ID            string
	Name          string
	ContextWindow int
	MaxOutput     int
	Pricing       Pricing
	Capabilities  Capabilities
}

// Pricing is USD per million tokens.
type Pricing struct {
	InputPerMTok      float64
	OutputPerMTok     float64
	CacheReadPerMTok  float64
	CacheWritePerMTok float64
}

// Capabilities describes optional features a model supports.
type Capabilities struct {
	Vision      bool
	Thinking    bool
	ToolUse     bool
	PromptCache bool
}

// StreamEvent is one canonical event of a streaming response; which fields are set depends on Kind.
type StreamEvent struct {
	Kind         StreamEventKind
	Text         string
	Thinking     []byte
	ToolUse      *control.ToolUse
	ToolUseID    string
	InputDelta   string
	Usage        *Usage
	FinishReason string
	Err          error
}

type StreamEventKind int

const (
	KindTextDelta StreamEventKind = iota
	KindThinkingDelta
	KindToolUseStart
	KindToolUseDelta
	KindToolUseStop
	KindUsage
	KindStop
	KindError
)

// Usage is the token accounting from a provider's response.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}
