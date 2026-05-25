// Package provider defines the Provider abstraction the engine uses
// to talk to language models. Provider adapters live in subpackages
// (openrouter, zai, copilot, routing) and translate to/from the
// canonical Anthropic-shape message form used internally.
//
// See docs/harness-architecture.md § Providers.
package provider

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
)

// Provider is the abstraction every LLM transport implements.
//
// Concrete implementations live in subpackages:
//   - openrouter (v0.1)
//   - zai        (v0.1)
//   - copilot    (v0.2 placeholder)
//   - routing    (v0.3 RoutingProvider composition)
type Provider interface {
	// Name returns the provider's identifier (e.g., "openrouter", "zai").
	Name() string

	// Chat starts a streaming chat completion. The returned channel
	// is closed when the stream terminates (success or error).
	//
	// The engine consumes events from this channel and re-emits them
	// onto the per-session Bus.
	Chat(ctx context.Context, req *Request) (<-chan StreamEvent, error)

	// Models returns the model catalog this provider can serve. The
	// engine surfaces this through /v1/providers/{name}/models.
	Models(ctx context.Context) ([]Model, error)

	// TokenCount estimates how many tokens the given messages will
	// consume against the named model. Used by the compaction
	// trigger middleware.
	TokenCount(ctx context.Context, model string, msgs []Message) (int, error)
}

// Request is a model invocation. It is provider-agnostic: each
// adapter translates it to its wire format.
type Request struct {
	Model       string
	System      string                 // system prompt (already assembled by middleware)
	Messages    []Message              // canonical history
	Tools       []ToolSchema           // available tools for tool_use
	Temperature float64                // 0.0 = deterministic
	MaxTokens   int                    // 0 = provider default
	CacheHints  []CacheBreakpoint      // provider-aware cache placement
	Extra       map[string]interface{} // provider-specific knobs (rarely used)
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

// ToolSchema is the tool catalog entry surfaced to the model.
type ToolSchema struct {
	Name        string
	Description string
	InputSchema []byte // JSON Schema bytes
}

// CacheBreakpoint marks a position in the history where prompt cache
// should be anchored. Provider-aware: Anthropic uses content-block
// cache_control; OpenAI uses prompt_cache_key; others ignore.
type CacheBreakpoint struct {
	MessageIndex int    // 0-based index into Request.Messages
	Marker       string // optional opaque marker
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

// Pricing per million tokens, USD.
type Pricing struct {
	InputPerMTok       float64
	OutputPerMTok      float64
	CacheReadPerMTok   float64 // discounted cache hit rate
	CacheWritePerMTok  float64 // cache-write surcharge
}

// Capabilities describes optional features a model supports.
type Capabilities struct {
	Vision      bool
	Thinking    bool
	ToolUse     bool
	PromptCache bool
}

// StreamEvent is a single event from a provider's streaming
// response. Each adapter parses provider-specific wire format and
// emits these canonical events. The engine's stream parser
// (engine/stream.go) translates them to control.Event types.
type StreamEvent struct {
	Kind       StreamEventKind
	Text       string                 // for KindTextDelta
	Thinking   []byte                 // for KindThinkingDelta (opaque, provider-stamped)
	ToolUse    *control.ToolUse       // for KindToolUseStart (Input filled in on Stop)
	ToolUseID  string                 // for KindToolUseDelta / KindToolUseStop
	InputDelta string                 // for KindToolUseDelta (concatenates to ToolUse.Input on Stop)
	Usage      *Usage                 // for KindUsage
	FinishReason string               // for KindStop ("stop", "tool_use", "length", "yield")
	Err        error                  // for KindError
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
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheWriteTokens  int
}
