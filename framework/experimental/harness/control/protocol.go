package control

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// ProtocolVersion is the SemVer of the wire protocol.
const ProtocolVersion = "0.1.0"

// CanonicalFormVersion is the version of the canonical message form provider adapters target.
const CanonicalFormVersion = 1

// Schema versions of the sub-schemas reported in the handshake response.
const (
	SchemaVersionTokenClaim = 1
	SchemaVersionProfile    = 1
	SchemaVersionSessionLog = 1
)

// ResourceURIScheme is the URI scheme of MCP-server resources.
const ResourceURIScheme = "harness/v1"

// IdentityClass distinguishes human-driven clients from agent-driven ones.
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

// ContentBlock is a content item in a message; Type selects which field is set.
type ContentBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ToolUse    *ToolUse        `json:"tool_use,omitempty"`
	ToolResult *ToolResultBlk  `json:"tool_result,omitempty"`
	Thinking   json.RawMessage `json:"thinking,omitempty"`
	Image      *ImageBlock     `json:"image,omitempty"`
	Yield      *YieldBlock     `json:"yield,omitempty"`
}

// ToolUse is a model-emitted tool invocation.
type ToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResultBlk is the result of a tool invocation, fed back to the model.
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

// Command is the closed union of client-to-engine verbs; plugins extend it with CustomCommand.
type Command interface {
	isCommand()
	// CommandKind is the wire discriminator, one of handshake.command_kinds.
	CommandKind() string
}

type SendInput struct {
	SessionID ids.SessionID  `json:"sessionId"`
	Content   []ContentBlock `json:"content"`
	// Wait is "" or "none" to return at once, "turn" to block until TurnEnded.
	Wait string `json:"wait,omitempty"`
}

type CancelTurn struct {
	SessionID ids.SessionID `json:"sessionId"`
}

type AnswerPermission struct {
	SessionID ids.SessionID `json:"sessionId"`
	CallID    ids.CallID    `json:"callId"`
	Decision  Decision      `json:"decision"`
	Scope     PermitScope   `json:"scope,omitempty"`
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
	// ScopeAlways persists the rule so later harness runs honor it without asking.
	ScopeAlways PermitScope = "always"
)

type CreateSession struct {
	Profile string `json:"profile"`
	// Resume, when non-nil, attaches the new run to an existing log.
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

// CustomCommand is a plugin-defined command, routed by Namespace.
type CustomCommand struct {
	SessionID ids.SessionID   `json:"sessionId"`
	Namespace string          `json:"namespace"`
	Verb      string          `json:"verb"`
	Payload   json.RawMessage `json:"payload"`
}

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

// AllCommandKinds returns the built-in command kinds in handshake order.
func AllCommandKinds() []string {
	return []string{
		"SendInput", "CancelTurn", "AnswerPermission",
		"CreateSession", "AttachSession", "DetachSession",
		"SetModel", "EnterPlanMode", "ExitPlanMode",
		"CustomCommand",
	}
}

// Event is the closed union of engine-emitted events; plugins extend it with CustomEvent.
type Event interface {
	isEvent()
	EventKind() string
}

// EventEnvelope is the event JSON every transport carries inside its own framing.
type EventEnvelope struct {
	ID         uint64          `json:"id"`
	Kind       string          `json:"kind"`
	Session    ids.SessionID   `json:"session"`
	Originator ids.ClientID    `json:"originator,omitempty"`
	TS         time.Time       `json:"ts"`
	Payload    json.RawMessage `json:"payload"`
	// SubAgent names the sub-agent run that published the event; empty for the session's own.
	SubAgent string `json:"subAgent,omitempty"`
}

type TextDelta struct {
	Text string `json:"text"`
}

type ThinkingDelta struct {
	Block json.RawMessage `json:"block"`
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
	CallID  ids.CallID     `json:"callId"`
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type TurnStarted struct {
	Turn       int          `json:"turn"`
	Originator ids.ClientID `json:"originator"`
	// Content is the input that started the turn, so other attached clients can render it.
	Content []ContentBlock `json:"content,omitempty"`
}

type TurnEnded struct {
	Turn   int    `json:"turn"`
	Reason string `json:"reason"`
}

// TurnTiming carries per-component durations for a finished turn.
type TurnTiming struct {
	Turn       int                      `json:"turn"`
	Components map[string]time.Duration `json:"components"`
	ToolCalls  []ToolTimingEntry        `json:"toolCalls,omitempty"`
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
	CallID     ids.CallID      `json:"callId"`
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Originator ids.ClientID    `json:"originator"`
	Reason     string          `json:"reason,omitempty"`
}

type Cancelled struct {
	Turn   int          `json:"turn"`
	By     ids.ClientID `json:"by"`
	Reason string       `json:"reason"`
}

type Error struct {
	Reason  string          `json:"reason"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

type StreamGap struct {
	From   uint64 `json:"from"`
	To     uint64 `json:"to"`
	Reason string `json:"reason"`
}

type TokenExpiring struct {
	JTI   ids.JTI   `json:"jti"`
	ExpAt time.Time `json:"expAt"`
}

type HookTimeout struct {
	Event   string        `json:"event"`
	Command string        `json:"command"`
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
	Reason string `json:"reason"`
}

// CustomEvent is the open extension event for plugin-defined wire events.
type CustomEvent struct {
	Namespace string          `json:"namespace"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
}

func (TextDelta) isEvent()           {}
func (ThinkingDelta) isEvent()       {}
func (ToolCallStarted) isEvent()     {}
func (ToolCallProgress) isEvent()    {}
func (ToolResult) isEvent()          {}
func (TurnStarted) isEvent()         {}
func (TurnEnded) isEvent()           {}
func (TurnTiming) isEvent()          {}
func (CompactionTriggered) isEvent() {}
func (CostIncremented) isEvent()     {}
func (PermissionRequested) isEvent() {}
func (Cancelled) isEvent()           {}
func (Error) isEvent()               {}
func (StreamGap) isEvent()           {}
func (TokenExpiring) isEvent()       {}
func (HookTimeout) isEvent()         {}
func (HookError) isEvent()           {}
func (MCPServerDown) isEvent()       {}
func (SessionEnded) isEvent()        {}
func (CustomEvent) isEvent()         {}

func (TextDelta) EventKind() string           { return "TextDelta" }
func (ThinkingDelta) EventKind() string       { return "ThinkingDelta" }
func (ToolCallStarted) EventKind() string     { return "ToolCallStarted" }
func (ToolCallProgress) EventKind() string    { return "ToolCallProgress" }
func (ToolResult) EventKind() string          { return "ToolResult" }
func (TurnStarted) EventKind() string         { return "TurnStarted" }
func (TurnEnded) EventKind() string           { return "TurnEnded" }
func (TurnTiming) EventKind() string          { return "TurnTiming" }
func (CompactionTriggered) EventKind() string { return "CompactionTriggered" }
func (CostIncremented) EventKind() string     { return "CostIncremented" }
func (PermissionRequested) EventKind() string { return "PermissionRequested" }
func (Cancelled) EventKind() string           { return "Cancelled" }
func (Error) EventKind() string               { return "Error" }
func (StreamGap) EventKind() string           { return "StreamGap" }
func (TokenExpiring) EventKind() string       { return "TokenExpiring" }
func (HookTimeout) EventKind() string         { return "HookTimeout" }
func (HookError) EventKind() string           { return "HookError" }
func (MCPServerDown) EventKind() string       { return "MCPServerDown" }
func (SessionEnded) EventKind() string        { return "SessionEnded" }
func (CustomEvent) EventKind() string         { return "CustomEvent" }

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

// Handshake is the response to GET /v1/handshake and the first WS/MCP frame.
type Handshake struct {
	ProtocolVersion         string   `json:"protocol_version"`
	CanonicalFormVersion    int      `json:"canonical_form_version"`
	SchemaVersionTokenClaim int      `json:"schema_version_token_claim"`
	SchemaVersionProfile    int      `json:"schema_version_profile"`
	SchemaVersionSessionLog int      `json:"schema_version_session_log"`
	CommandKinds            []string `json:"command_kinds"`
	EventKinds              []string `json:"event_kinds"`
	Features                []string `json:"features"`
	ResourceURIScheme       string   `json:"resource_uri_scheme"`
}

// CurrentHandshake returns this binary's handshake with the given feature list.
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

// FeaturesV01 is the feature set v0.1 builds advertise.
var FeaturesV01 = []string{
	"rest",
	"branching",
	"auto_approve",
	"plan_mode",
}

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

func unmarshalCommandBody(data []byte, dst any) error {
	if err := handler.CheckObjectKeys(data, handler.FoldJSONKey); err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// UnmarshalCommand decodes a wire envelope into a typed Command; an unknown kind errors.
func UnmarshalCommand(data []byte) (Command, error) {
	var env commandEnvelope
	if err := unmarshalCommandBody(data, &env); err != nil {
		return nil, err
	}
	switch env.Kind {
	case "SendInput":
		var c SendInput
		return c, unmarshalCommandBody(env.Body, &c)
	case "CancelTurn":
		var c CancelTurn
		return c, unmarshalCommandBody(env.Body, &c)
	case "AnswerPermission":
		var c AnswerPermission
		return c, unmarshalCommandBody(env.Body, &c)
	case "CreateSession":
		var c CreateSession
		return c, unmarshalCommandBody(env.Body, &c)
	case "AttachSession":
		var c AttachSession
		return c, unmarshalCommandBody(env.Body, &c)
	case "DetachSession":
		var c DetachSession
		return c, unmarshalCommandBody(env.Body, &c)
	case "SetModel":
		var c SetModel
		return c, unmarshalCommandBody(env.Body, &c)
	case "EnterPlanMode":
		var c EnterPlanMode
		return c, unmarshalCommandBody(env.Body, &c)
	case "ExitPlanMode":
		var c ExitPlanMode
		return c, unmarshalCommandBody(env.Body, &c)
	case "CustomCommand":
		var c CustomCommand
		return c, unmarshalCommandBody(env.Body, &c)
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

// DecodeEvent decodes an envelope's typed payload; an unknown kind errors.
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

// Stable Reason codes used in Error events.
const (
	ReasonHandshakeRequired        = "HandshakeRequired"
	ReasonHandshakeVersionMismatch = "HandshakeVersionMismatch"
	ReasonTurnInProgress           = "TurnInProgress"
	ReasonPermissionDenied         = "PermissionDenied"
	ReasonPermissionTimeout        = "PermissionTimeout"
	ReasonTokenExpired             = "TokenExpired"
	ReasonTokenRevoked             = "TokenRevoked"
	ReasonMCPServerSHA256Mismatch  = "MCPServerSHA256Mismatch"
	ReasonMCPServerUnavailable     = "MCPServerUnavailable"
	ReasonHookHashChanged          = "HookHashChanged"
	ReasonHookTimeout              = "HookTimeout"
	ReasonRateLimited              = "RateLimited"
	ReasonBashCancelledMidCommand  = "BashCancelledMidCommand"
	ReasonNonInteractiveAckRefused = "NonInteractiveAckRefused"
	ReasonCredentialHelperFailed   = "CredentialHelperFailed"
	ReasonInvalidCommand           = "InvalidCommand"
)
