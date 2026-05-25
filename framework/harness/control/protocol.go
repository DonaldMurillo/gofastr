// Package control hosts the engine-as-a-service control plane.
//
// This file defines the transport-agnostic wire protocol: the
// canonical event envelope, the Command and Event sealed unions, the
// handshake, and the JSON codec. Every transport carries the same
// canonical event JSON verbatim; envelopes are framing only.
//
// See docs/harness-architecture.md § Control plane + § Protocol
// versioning & evolution.
package control

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// ProtocolVersion is the SemVer string identifying the wire protocol.
// Minor bumps are additive-only; major bumps require client
// re-implementation. See docs § Protocol versioning → Handshake.
const ProtocolVersion = "0.1.0"

// CanonicalFormVersion is the version of the internal Anthropic-shape
// canonical message form. Provider adapters target an explicit
// version; the engine asserts they match.
const CanonicalFormVersion = 1

// Schema versions of independently-evolving sub-schemas, surfaced in
// the handshake response.
const (
	SchemaVersionTokenClaim = 1
	SchemaVersionProfile    = 1
	SchemaVersionSessionLog = 1
)

// ResourceURIScheme is the URI scheme for MCP-server-exposed
// resources. Pinned to v1 so a v2 scheme can coexist later.
const ResourceURIScheme = "harness/v1"

// IdentityClass distinguishes human-driven clients from agent-driven
// ones. See hard rule 11 in the architecture doc.
type IdentityClass uint8

const (
	IdentityHuman IdentityClass = iota
	IdentityAgent
)

// String returns the wire-format string for the identity class.
func (c IdentityClass) String() string {
	switch c {
	case IdentityHuman:
		return "human"
	case IdentityAgent:
		return "agent"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the IdentityClass as its lowercase wire string.
func (c IdentityClass) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

// UnmarshalJSON decodes the IdentityClass from its lowercase wire string.
func (c *IdentityClass) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "human":
		*c = IdentityHuman
	case "agent":
		*c = IdentityAgent
	default:
		return fmt.Errorf("control: unknown identity_class %q", s)
	}
	return nil
}

// ---------- Content blocks (Anthropic-shape canonical form) ----------

// ContentBlock is a typed content item in a message. Blocks have a
// "type" discriminator on the wire.
type ContentBlock struct {
	Type string `json:"type"`

	// Type-specific fields. Only the field matching Type is meaningful.
	Text       string          `json:"text,omitempty"`
	ToolUse    *ToolUse        `json:"tool_use,omitempty"`
	ToolResult *ToolResultBlk  `json:"tool_result,omitempty"`
	Thinking   json.RawMessage `json:"thinking,omitempty"` // opaque, provider-stamped
	Image      *ImageBlock     `json:"image,omitempty"`
	Yield      *YieldBlock     `json:"yield,omitempty"` // explicit end-turn signal
}

// ToolUse is a model-emitted tool invocation.
type ToolUse struct {
	ID    string          `json:"id"`    // matches a tool_use id later in ToolResultBlk
	Name  string          `json:"name"`  // tool name
	Input json.RawMessage `json:"input"` // tool-call arguments
}

// ToolResultBlk is the result of a tool invocation, fed back to the
// model on the next turn.
type ToolResultBlk struct {
	ToolUseID string         `json:"tool_use_id"`
	Content   []ContentBlock `json:"content"`
	IsError   bool           `json:"is_error,omitempty"`
}

// ImageBlock is a base64-encoded image content block.
type ImageBlock struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// YieldBlock signals an explicit end-of-turn from the provider.
type YieldBlock struct {
	Reason string `json:"reason,omitempty"`
}

// ---------- Commands (client → engine) ----------

// Command is a sealed union of wire-level verbs sent from clients to
// the engine. Per hard rule 14, this set is closed in `control/`;
// plugins extend via CustomCommand.
type Command interface {
	isCommand()
	// CommandKind returns the kind discriminator used on the wire
	// (matches handshake.command_kinds entries).
	CommandKind() string
}

type SendInput struct {
	SessionID ids.SessionID  `json:"sessionId"`
	Content   []ContentBlock `json:"content"`
	// Wait controls REST/MCP synchronous vs streaming behavior.
	// "" / "none" → returns immediately; "turn" → blocks until TurnEnded.
	Wait string `json:"wait,omitempty"`
}

type CancelTurn struct {
	SessionID ids.SessionID `json:"sessionId"`
}

type AnswerPermission struct {
	SessionID ids.SessionID `json:"sessionId"`
	CallID    ids.CallID    `json:"callId"`
	Decision  Decision      `json:"decision"`
	Scope     PermitScope   `json:"scope,omitempty"` // for argv-glob/tool/session allow buttons
}

// Decision is the user's answer to a permission prompt.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// PermitScope describes how broadly an "allow" decision applies.
type PermitScope string

const (
	ScopeOnce        PermitScope = "once"
	ScopeArgvGlob    PermitScope = "argv_glob"
	ScopeTool        PermitScope = "tool"
	ScopeSessionWide PermitScope = "session"
	// ScopeAlways persists the rule to disk so subsequent harness
	// runs honor it without re-prompting. The persistence layer
	// (XDG_CONFIG_HOME/gofastr/harness/permissions.json) is loaded
	// on Engine boot.
	ScopeAlways PermitScope = "always"
)

type CreateSession struct {
	Profile string `json:"profile"`
	// Resume, when non-nil, attaches the new EngineRun to an existing LogID.
	Resume *ids.LogID `json:"resume,omitempty"`
}

type AttachSession struct {
	SessionID ids.SessionID `json:"sessionId"`
}

type DetachSession struct {
	SessionID ids.SessionID `json:"sessionId"`
}

type SetModel struct {
	SessionID ids.SessionID `json:"sessionId"`
	Model     string        `json:"model"`
}

type EnterPlanMode struct {
	SessionID ids.SessionID `json:"sessionId"`
}

type ExitPlanMode struct {
	SessionID ids.SessionID `json:"sessionId"`
	Approve   bool          `json:"approve"`
}

// CustomCommand is the open extension verb for plugin-defined wire
// commands. The engine routes by Namespace to the registered plugin
// handler.
type CustomCommand struct {
	SessionID ids.SessionID   `json:"sessionId"`
	Namespace string          `json:"namespace"` // matches a claimed slash-command namespace
	Verb      string          `json:"verb"`
	Payload   json.RawMessage `json:"payload"`
}

// Mark each command type as implementing the Command interface.
func (SendInput) isCommand()        {}
func (CancelTurn) isCommand()       {}
func (AnswerPermission) isCommand() {}
func (CreateSession) isCommand()    {}
func (AttachSession) isCommand()    {}
func (DetachSession) isCommand()    {}
func (SetModel) isCommand()         {}
func (EnterPlanMode) isCommand()    {}
func (ExitPlanMode) isCommand()     {}
func (CustomCommand) isCommand()    {}

func (SendInput) CommandKind() string        { return "SendInput" }
func (CancelTurn) CommandKind() string       { return "CancelTurn" }
func (AnswerPermission) CommandKind() string { return "AnswerPermission" }
func (CreateSession) CommandKind() string    { return "CreateSession" }
func (AttachSession) CommandKind() string    { return "AttachSession" }
func (DetachSession) CommandKind() string    { return "DetachSession" }
func (SetModel) CommandKind() string         { return "SetModel" }
func (EnterPlanMode) CommandKind() string    { return "EnterPlanMode" }
func (ExitPlanMode) CommandKind() string     { return "ExitPlanMode" }
func (CustomCommand) CommandKind() string    { return "CustomCommand" }

// AllCommandKinds returns the closed set of built-in command kinds, in
// the order they appear in the handshake response.
func AllCommandKinds() []string {
	return []string{
		"SendInput", "CancelTurn", "AnswerPermission",
		"CreateSession", "AttachSession", "DetachSession",
		"SetModel", "EnterPlanMode", "ExitPlanMode",
		"CustomCommand",
	}
}

// ---------- Events (engine → client) ----------

// Event is a sealed union of engine-emitted events. Per hard rule 14,
// this set is closed in `control/`; plugins extend via CustomEvent.
type Event interface {
	isEvent()
	EventKind() string
}

// EventEnvelope is the canonical JSON wrapper used by every transport.
// Transports may add their own framing on top (SSE `id:`/`event:`/`data:`,
// WS `{"frame":"event","body":...}`, MCP `notifications/resources/updated`),
// but the body is always this envelope.
type EventEnvelope struct {
	ID         uint64        `json:"id"`         // monotonic per SessionID
	Kind       string        `json:"kind"`       // matches handshake.event_kinds
	Session    ids.SessionID `json:"session"`
	Originator ids.ClientID  `json:"originator,omitempty"`
	TS         time.Time     `json:"ts"`
	Payload    json.RawMessage `json:"payload"`
}

// --- Event payload types ---

type TextDelta struct {
	Text string `json:"text"`
}

type ThinkingDelta struct {
	Block json.RawMessage `json:"block"` // opaque, provider-stamped
}

type ToolCallStarted struct {
	CallID   ids.CallID      `json:"callId"`
	Tool     string          `json:"tool"`
	Args     json.RawMessage `json:"args"`
	Mutating bool            `json:"mutating"`
}

type ToolCallProgress struct {
	CallID  ids.CallID `json:"callId"`
	Partial string     `json:"partial"`
}

type ToolResult struct {
	CallID  ids.CallID    `json:"callId"`
	Content []ContentBlock `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type TurnStarted struct {
	Turn       int          `json:"turn"`
	Originator ids.ClientID `json:"originator"`
	// Content is the input that started this turn. Lets non-originator
	// clients (browser sidecar attached to the same session as the TUI,
	// or vice-versa) render the user message without round-tripping
	// through a separate "echo" event. Optional for backwards
	// compatibility — older publishers may omit it.
	Content []ContentBlock `json:"content,omitempty"`
}

type TurnEnded struct {
	Turn   int    `json:"turn"`
	Reason string `json:"reason"` // "complete" | "cancelled" | "error" | "yield"
}

// TurnTiming is emitted at TurnEnded with per-component duration
// data. Operators answer "where was the time?" from a single event.
type TurnTiming struct {
	Turn       int                       `json:"turn"`
	Components map[string]time.Duration  `json:"components"`
	ToolCalls  []ToolTimingEntry         `json:"toolCalls,omitempty"`
}

type ToolTimingEntry struct {
	CallID   ids.CallID    `json:"callId"`
	Tool     string        `json:"tool"`
	Duration time.Duration `json:"duration"`
}

type CompactionTriggered struct {
	BeforeTokens int `json:"beforeTokens"`
	AfterTokens  int `json:"afterTokens"`
}

type CostIncremented struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CacheTokens  int     `json:"cacheTokens,omitempty"`
	USD          float64 `json:"usd"`
}

type PermissionRequested struct {
	CallID     ids.CallID    `json:"callId"`
	Tool       string        `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Originator ids.ClientID  `json:"originator"`
	Reason     string        `json:"reason,omitempty"` // e.g. why this is being asked
}

type Cancelled struct {
	Turn   int    `json:"turn"`
	By     ids.ClientID `json:"by"`
	Reason string `json:"reason"`
}

type Error struct {
	Reason  string          `json:"reason"`  // stable error code
	Message string          `json:"message"` // human-friendly text
	Details json.RawMessage `json:"details,omitempty"`
}

type StreamGap struct {
	From   uint64 `json:"from"`
	To     uint64 `json:"to"`
	Reason string `json:"reason"` // "ttl" | "compaction"
}

type TokenExpiring struct {
	JTI   ids.JTI   `json:"jti"`
	ExpAt time.Time `json:"expAt"`
}

type HookTimeout struct {
	Event   string `json:"event"`
	Command string `json:"command"`
	After   time.Duration `json:"after"`
}

type HookError struct {
	Event    string `json:"event"`
	Command  string `json:"command"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output,omitempty"`
}

type MCPServerDown struct {
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Attempts int    `json:"attempts"`
}

type SessionEnded struct {
	Reason string `json:"reason"` // "idle" | "user" | "error" | "binary-shutdown"
}

// CustomEvent is the open extension event for plugin-defined wire events.
type CustomEvent struct {
	Namespace string          `json:"namespace"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}

// Mark each event type as implementing the Event interface.
func (TextDelta) isEvent()          {}
func (ThinkingDelta) isEvent()      {}
func (ToolCallStarted) isEvent()    {}
func (ToolCallProgress) isEvent()   {}
func (ToolResult) isEvent()         {}
func (TurnStarted) isEvent()        {}
func (TurnEnded) isEvent()          {}
func (TurnTiming) isEvent()         {}
func (CompactionTriggered) isEvent() {}
func (CostIncremented) isEvent()    {}
func (PermissionRequested) isEvent() {}
func (Cancelled) isEvent()          {}
func (Error) isEvent()              {}
func (StreamGap) isEvent()          {}
func (TokenExpiring) isEvent()      {}
func (HookTimeout) isEvent()        {}
func (HookError) isEvent()          {}
func (MCPServerDown) isEvent()      {}
func (SessionEnded) isEvent()       {}
func (CustomEvent) isEvent()        {}

func (TextDelta) EventKind() string          { return "TextDelta" }
func (ThinkingDelta) EventKind() string      { return "ThinkingDelta" }
func (ToolCallStarted) EventKind() string    { return "ToolCallStarted" }
func (ToolCallProgress) EventKind() string   { return "ToolCallProgress" }
func (ToolResult) EventKind() string         { return "ToolResult" }
func (TurnStarted) EventKind() string        { return "TurnStarted" }
func (TurnEnded) EventKind() string          { return "TurnEnded" }
func (TurnTiming) EventKind() string         { return "TurnTiming" }
func (CompactionTriggered) EventKind() string { return "CompactionTriggered" }
func (CostIncremented) EventKind() string    { return "CostIncremented" }
func (PermissionRequested) EventKind() string { return "PermissionRequested" }
func (Cancelled) EventKind() string          { return "Cancelled" }
func (Error) EventKind() string              { return "Error" }
func (StreamGap) EventKind() string          { return "StreamGap" }
func (TokenExpiring) EventKind() string      { return "TokenExpiring" }
func (HookTimeout) EventKind() string        { return "HookTimeout" }
func (HookError) EventKind() string          { return "HookError" }
func (MCPServerDown) EventKind() string      { return "MCPServerDown" }
func (SessionEnded) EventKind() string       { return "SessionEnded" }
func (CustomEvent) EventKind() string        { return "CustomEvent" }

// AllEventKinds returns the closed set of built-in event kinds.
func AllEventKinds() []string {
	return []string{
		"TextDelta", "ThinkingDelta", "ToolCallStarted", "ToolCallProgress",
		"ToolResult", "TurnStarted", "TurnEnded", "TurnTiming",
		"CompactionTriggered", "CostIncremented", "PermissionRequested",
		"Cancelled", "Error", "StreamGap", "TokenExpiring",
		"HookTimeout", "HookError", "MCPServerDown", "SessionEnded",
		"CustomEvent",
	}
}

// ---------- Handshake ----------

// Handshake is the response to GET /v1/handshake (or the first frame
// on a WS/MCP session). Required before any other command.
type Handshake struct {
	ProtocolVersion        string   `json:"protocol_version"`
	CanonicalFormVersion   int      `json:"canonical_form_version"`
	SchemaVersionTokenClaim int     `json:"schema_version_token_claim"`
	SchemaVersionProfile   int      `json:"schema_version_profile"`
	SchemaVersionSessionLog int     `json:"schema_version_session_log"`
	CommandKinds           []string `json:"command_kinds"`
	EventKinds             []string `json:"event_kinds"`
	Features               []string `json:"features"`
	ResourceURIScheme      string   `json:"resource_uri_scheme"`
}

// CurrentHandshake returns the handshake advertised by this binary.
// The Features list is provided by the caller so different builds
// (e.g. v0.1 vs v0.2) can advertise different optional capabilities.
func CurrentHandshake(features []string) Handshake {
	return Handshake{
		ProtocolVersion:         ProtocolVersion,
		CanonicalFormVersion:    CanonicalFormVersion,
		SchemaVersionTokenClaim: SchemaVersionTokenClaim,
		SchemaVersionProfile:    SchemaVersionProfile,
		SchemaVersionSessionLog: SchemaVersionSessionLog,
		CommandKinds:            AllCommandKinds(),
		EventKinds:              AllEventKinds(),
		Features:                features,
		ResourceURIScheme:       ResourceURIScheme,
	}
}

// FeaturesV01 is the feature flag set advertised by v0.1 builds.
//
// Per the build-order section of the architecture doc:
//   - inproc + rest transports are first-class in v0.1
//   - ws + mcpserver_stdio land in v0.2
//   - mcpserver_http lands in v0.3
//   - delegate ships with v0.3
var FeaturesV01 = []string{
	"rest",
	"branching",
	"auto_approve",
	"plan_mode",
}

// ---------- JSON codec for Commands ----------

// commandEnvelope is the wire shape used to round-trip a typed
// Command through JSON: a "kind" discriminator plus the type-specific
// body.
type commandEnvelope struct {
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"`
}

// MarshalCommand encodes a Command to JSON with a "kind" discriminator.
func MarshalCommand(c Command) ([]byte, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return json.Marshal(commandEnvelope{Kind: c.CommandKind(), Body: body})
}

// UnmarshalCommand decodes a wire JSON envelope back into a typed Command.
// Unknown command kinds return an error (closed-union enforcement).
func UnmarshalCommand(data []byte) (Command, error) {
	var env commandEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	switch env.Kind {
	case "SendInput":
		var c SendInput
		return c, json.Unmarshal(env.Body, &c)
	case "CancelTurn":
		var c CancelTurn
		return c, json.Unmarshal(env.Body, &c)
	case "AnswerPermission":
		var c AnswerPermission
		return c, json.Unmarshal(env.Body, &c)
	case "CreateSession":
		var c CreateSession
		return c, json.Unmarshal(env.Body, &c)
	case "AttachSession":
		var c AttachSession
		return c, json.Unmarshal(env.Body, &c)
	case "DetachSession":
		var c DetachSession
		return c, json.Unmarshal(env.Body, &c)
	case "SetModel":
		var c SetModel
		return c, json.Unmarshal(env.Body, &c)
	case "EnterPlanMode":
		var c EnterPlanMode
		return c, json.Unmarshal(env.Body, &c)
	case "ExitPlanMode":
		var c ExitPlanMode
		return c, json.Unmarshal(env.Body, &c)
	case "CustomCommand":
		var c CustomCommand
		return c, json.Unmarshal(env.Body, &c)
	default:
		return nil, fmt.Errorf("control: unknown command kind %q", env.Kind)
	}
}

// EncodeEvent wraps a typed event payload in a canonical EventEnvelope.
func EncodeEvent(id uint64, e Event, session ids.SessionID, originator ids.ClientID, ts time.Time) (EventEnvelope, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return EventEnvelope{}, err
	}
	return EventEnvelope{
		ID:         id,
		Kind:       e.EventKind(),
		Session:    session,
		Originator: originator,
		TS:         ts,
		Payload:    payload,
	}, nil
}

// DecodeEvent extracts the typed event payload from a canonical
// envelope. Unknown event kinds return an error.
func DecodeEvent(env EventEnvelope) (Event, error) {
	switch env.Kind {
	case "TextDelta":
		var e TextDelta
		return e, json.Unmarshal(env.Payload, &e)
	case "ThinkingDelta":
		var e ThinkingDelta
		return e, json.Unmarshal(env.Payload, &e)
	case "ToolCallStarted":
		var e ToolCallStarted
		return e, json.Unmarshal(env.Payload, &e)
	case "ToolCallProgress":
		var e ToolCallProgress
		return e, json.Unmarshal(env.Payload, &e)
	case "ToolResult":
		var e ToolResult
		return e, json.Unmarshal(env.Payload, &e)
	case "TurnStarted":
		var e TurnStarted
		return e, json.Unmarshal(env.Payload, &e)
	case "TurnEnded":
		var e TurnEnded
		return e, json.Unmarshal(env.Payload, &e)
	case "TurnTiming":
		var e TurnTiming
		return e, json.Unmarshal(env.Payload, &e)
	case "CompactionTriggered":
		var e CompactionTriggered
		return e, json.Unmarshal(env.Payload, &e)
	case "CostIncremented":
		var e CostIncremented
		return e, json.Unmarshal(env.Payload, &e)
	case "PermissionRequested":
		var e PermissionRequested
		return e, json.Unmarshal(env.Payload, &e)
	case "Cancelled":
		var e Cancelled
		return e, json.Unmarshal(env.Payload, &e)
	case "Error":
		var e Error
		return e, json.Unmarshal(env.Payload, &e)
	case "StreamGap":
		var e StreamGap
		return e, json.Unmarshal(env.Payload, &e)
	case "TokenExpiring":
		var e TokenExpiring
		return e, json.Unmarshal(env.Payload, &e)
	case "HookTimeout":
		var e HookTimeout
		return e, json.Unmarshal(env.Payload, &e)
	case "HookError":
		var e HookError
		return e, json.Unmarshal(env.Payload, &e)
	case "MCPServerDown":
		var e MCPServerDown
		return e, json.Unmarshal(env.Payload, &e)
	case "SessionEnded":
		var e SessionEnded
		return e, json.Unmarshal(env.Payload, &e)
	case "CustomEvent":
		var e CustomEvent
		return e, json.Unmarshal(env.Payload, &e)
	default:
		return nil, fmt.Errorf("control: unknown event kind %q", env.Kind)
	}
}

// ---------- Error reason codes ----------

// Stable Reason codes used in Error events. Strings are documented in
// docs/harness-architecture.md § User-facing errors.
const (
	ReasonHandshakeRequired      = "HandshakeRequired"
	ReasonHandshakeVersionMismatch = "HandshakeVersionMismatch"
	ReasonTurnInProgress         = "TurnInProgress"
	ReasonPermissionDenied       = "PermissionDenied"
	ReasonPermissionTimeout      = "PermissionTimeout"
	ReasonTokenExpired           = "TokenExpired"
	ReasonTokenRevoked           = "TokenRevoked"
	ReasonMCPServerSHA256Mismatch = "MCPServerSHA256Mismatch"
	ReasonMCPServerUnavailable   = "MCPServerUnavailable"
	ReasonHookHashChanged        = "HookHashChanged"
	ReasonHookTimeout            = "HookTimeout"
	ReasonRateLimited            = "RateLimited"
	ReasonBashCancelledMidCommand = "BashCancelledMidCommand"
	ReasonNonInteractiveAckRefused = "NonInteractiveAckRefused"
	ReasonCredentialHelperFailed = "CredentialHelperFailed"
	ReasonInvalidCommand         = "InvalidCommand"
)
