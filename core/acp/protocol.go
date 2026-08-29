package acp

import "encoding/json"

// ProtocolVersion is the ACP major version this package speaks. ACP
// responds with the client's version when it matches, otherwise with
// this one.
const ProtocolVersion = 1

// JSON-RPC 2.0 error codes plus the ACP-specific reserved range
// (-32000 to -32099). See the ErrorCode section of the ACP v1 schema.
const (
	ErrParseError       = -32700
	ErrInvalidRequest   = -32600
	ErrMethodNotFound   = -32601
	ErrInvalidParams    = -32602
	ErrInternalError    = -32603
	ErrAuthRequired     = -32000 // authentication is required
	ErrResourceNotFound = -32002 // a named resource (e.g. a session) does not exist
	ErrRequestCancelled = -32800
)

// Implementation names the client or agent on the wire (clientInfo /
// agentInfo in initialize).
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// FileSystemCapabilities is what the CLIENT advertised for fs access.
// Per the spec these are client-side methods (fs/read_text_file,
// fs/write_text_file) the agent may call; this server never calls
// them, so the values are recorded for embedders and nothing else.
type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// AuthClientCapabilities is the client's auth-related capability set.
type AuthClientCapabilities struct {
	// Terminal means the client can reproduce the agent's invocation
	// interactively, which would license terminal-type auth methods.
	// This server advertises no auth methods, so it is unused.
	Terminal bool `json:"terminal"`
}

// ClientCapabilities is what the client offered at initialize. All
// fields default to false: per the spec, capabilities omitted by the
// peer are UNSUPPORTED.
type ClientCapabilities struct {
	Auth     *AuthClientCapabilities `json:"auth,omitempty"`
	FS       *FileSystemCapabilities `json:"fs,omitempty"`
	Terminal bool                    `json:"terminal,omitempty"`
}

// PromptCapabilities declares which extra content types the agent
// accepts in session/prompt. All agents must accept text and
// resource_link; the rest is opt-in and this server opts out of every
// one, emitting the false values explicitly so the client learns the
// absence at initialize instead of by sending a rejected block later.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// McpCapabilities declares whether the agent can connect to MCP
// servers on the client's behalf. This server cannot; the explicit
// false values are part of the initialize declaration.
type McpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AgentCapabilities is the capability block this server returns at
// initialize.
type AgentCapabilities struct {
	// LoadSession is true only when the embedded agent implements
	// SessionLoader.
	LoadSession bool `json:"loadSession"`

	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
	McpCapabilities    McpCapabilities    `json:"mcpCapabilities"`
}

// AuthMethod describes one way the client could authenticate. Methods
// without a Type are protocol-driven ("agent"): the client picks them
// by calling authenticate with the ID. This server advertises none by
// default; Options.AuthMethods adds them.
type AuthMethod struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type,omitempty"` // "" (= agent) | "terminal"
	Args        []string          `json:"args,omitempty"` // terminal methods only
	Env         map[string]string `json:"env,omitempty"`  // terminal methods only
}

// ContentBlock is one displayable piece of a message. Prompt input is
// limited to Type "text" and "resource_link" (the baseline every ACP
// agent must accept); the server rejects image, audio, and resource
// blocks with invalid params because initialize declared them absent.
type ContentBlock struct {
	Type string `json:"type"` // "text" | "resource_link"
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`  // resource_link
	Name string `json:"name,omitempty"` // resource_link
}

// Content block type constants.
const (
	ContentText         = "text"
	ContentResourceLink = "resource_link"
)

// Tool call kinds (ToolKind in the ACP schema). They only guide client
// icons and display.
const (
	ToolKindRead       = "read"
	ToolKindEdit       = "edit"
	ToolKindDelete     = "delete"
	ToolKindMove       = "move"
	ToolKindSearch     = "search"
	ToolKindExecute    = "execute"
	ToolKindThink      = "think"
	ToolKindFetch      = "fetch"
	ToolKindSwitchMode = "switch_mode"
	ToolKindOther      = "other"
)

// Tool call statuses (ToolCallStatus in the ACP schema).
const (
	ToolStatusPending    = "pending"
	ToolStatusInProgress = "in_progress"
	ToolStatusCompleted  = "completed"
	ToolStatusFailed     = "failed"
)

// ToolCallContent is one content item attached to a tool call: a
// wrapped content block, a file diff, or a terminal reference. Only
// the "content" variant is produced by this package; the other
// variants exist so embedders can construct every frame the schema
// allows.
type ToolCallContent struct {
	Type       string        `json:"type"` // "content" | "diff" | "terminal"
	Content    *ContentBlock `json:"content,omitempty"`
	Path       string        `json:"path,omitempty"`    // diff
	OldText    *string       `json:"oldText,omitempty"` // diff
	NewText    string        `json:"newText,omitempty"` // diff
	TerminalID string        `json:"terminalId,omitempty"`
}

// TextToolContent wraps text as a tool-call content item.
func TextToolContent(text string) ToolCallContent {
	return ToolCallContent{Type: "content", Content: &ContentBlock{Type: ContentText, Text: text}}
}

// ToolCall reports a tool invocation starting (sessionUpdate
// "tool_call").
type ToolCall struct {
	ToolCallID string            `json:"toolCallId"`
	Title      string            `json:"title"`
	Kind       string            `json:"kind,omitempty"`
	Status     string            `json:"status,omitempty"`
	Content    []ToolCallContent `json:"content,omitempty"`
	RawInput   map[string]any    `json:"rawInput,omitempty"`
	RawOutput  map[string]any    `json:"rawOutput,omitempty"`
}

// ToolCallUpdate patches a previously reported tool call
// (sessionUpdate "tool_call_update"). Every field except ToolCallID is
// optional; omitted fields keep their previous value. Nil pointers
// mean "no change", which marshals to absent.
type ToolCallUpdate struct {
	ToolCallID string            `json:"toolCallId"`
	Title      *string           `json:"title,omitempty"`
	Kind       *string           `json:"kind,omitempty"`
	Status     *string           `json:"status,omitempty"`
	Content    []ToolCallContent `json:"content,omitempty"`
	RawInput   map[string]any    `json:"rawInput,omitempty"`
	RawOutput  map[string]any    `json:"rawOutput,omitempty"`
}

// Plan entry priorities and statuses (PlanEntryPriority /
// PlanEntryStatus in the ACP schema).
const (
	PlanPriorityHigh   = "high"
	PlanPriorityMedium = "medium"
	PlanPriorityLow    = "low"

	PlanStatusPending    = "pending"
	PlanStatusInProgress = "in_progress"
	PlanStatusCompleted  = "completed"
)

// PlanEntry is one task in an execution plan. Every plan update must
// carry the complete entry list; the client replaces the plan
// wholesale.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

// Permission option kinds (PermissionOptionKind in the ACP schema).
const (
	PermissionAllowOnce    = "allow_once"
	PermissionAllowAlways  = "allow_always"
	PermissionRejectOnce   = "reject_once"
	PermissionRejectAlways = "reject_always"
)

// PermissionOption is one choice offered to the user in a
// session/request_permission call.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// Permission outcomes.
const (
	OutcomeSelected  = "selected"
	OutcomeCancelled = "cancelled"
)

// RequestPermissionOutcome is the user's decision on a
// session/request_permission call: either OutcomeCancelled, or
// OutcomeSelected with the chosen OptionID.
type RequestPermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

// Stop reasons (StopReason in the ACP schema).
const (
	StopEndTurn         = "end_turn"
	StopMaxTokens       = "max_tokens"
	StopMaxTurnRequests = "max_turn_requests"
	StopRefusal         = "refusal"
	StopCancelled       = "cancelled"
)

// Update is one session/update body, discriminated by SessionUpdate.
// Content holds a ContentBlock for message chunks and a
// []ToolCallContent for tool calls; the constructors keep the two
// shapes type-safe.
type Update struct {
	SessionUpdate string         `json:"sessionUpdate"`
	MessageID     string         `json:"messageId,omitempty"`
	Content       any            `json:"content,omitempty"`
	Entries       []PlanEntry    `json:"entries,omitempty"`
	ToolCallID    string         `json:"toolCallId,omitempty"`
	Title         string         `json:"title,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	Status        string         `json:"status,omitempty"`
	RawInput      map[string]any `json:"rawInput,omitempty"`
	RawOutput     map[string]any `json:"rawOutput,omitempty"`
}

// MarshalJSON emits the sessionUpdate discriminator first, then the
// fields in schema order, omitting everything the constructor left
// unset. A typed-nil Content (e.g. an empty []ToolCallContent) is
// omitted rather than serialized as null.
func (u Update) MarshalJSON() ([]byte, error) {
	var buf []byte
	appendKV := func(key string, val any) error {
		if len(buf) > 0 {
			buf = append(buf, ',')
		}
		k, err := json.Marshal(key)
		if err != nil {
			return err
		}
		buf = append(buf, k...)
		buf = append(buf, ':')
		v, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf = append(buf, v...)
		return nil
	}
	if err := appendKV("sessionUpdate", u.SessionUpdate); err != nil {
		return nil, err
	}
	if u.MessageID != "" {
		if err := appendKV("messageId", u.MessageID); err != nil {
			return nil, err
		}
	}
	if u.ToolCallID != "" {
		if err := appendKV("toolCallId", u.ToolCallID); err != nil {
			return nil, err
		}
	}
	if u.Title != "" {
		if err := appendKV("title", u.Title); err != nil {
			return nil, err
		}
	}
	if u.Kind != "" {
		if err := appendKV("kind", u.Kind); err != nil {
			return nil, err
		}
	}
	if u.Status != "" {
		if err := appendKV("status", u.Status); err != nil {
			return nil, err
		}
	}
	switch c := u.Content.(type) {
	case nil:
		// omit
	case []ToolCallContent:
		if len(c) > 0 {
			if err := appendKV("content", c); err != nil {
				return nil, err
			}
		}
	case ContentBlock:
		if err := appendKV("content", c); err != nil {
			return nil, err
		}
	default:
		if err := appendKV("content", c); err != nil {
			return nil, err
		}
	}
	if len(u.Entries) > 0 {
		if err := appendKV("entries", u.Entries); err != nil {
			return nil, err
		}
	}
	if len(u.RawInput) > 0 {
		if err := appendKV("rawInput", u.RawInput); err != nil {
			return nil, err
		}
	}
	if len(u.RawOutput) > 0 {
		if err := appendKV("rawOutput", u.RawOutput); err != nil {
			return nil, err
		}
	}
	return append(append([]byte{'{'}, buf...), '}'), nil
}

// Session update discriminators.
const (
	UpdateUserMessageChunk  = "user_message_chunk"
	UpdateAgentMessageChunk = "agent_message_chunk"
	UpdatePlan              = "plan"
	UpdateToolCall          = "tool_call"
	UpdateToolCallUpdate    = "tool_call_update"
)

// UserMessageChunk streams one chunk of the user's message (used when
// replaying a loaded session). Chunks sharing a MessageID belong to
// one message.
func UserMessageChunk(messageID, text string) Update {
	return Update{
		SessionUpdate: UpdateUserMessageChunk,
		MessageID:     messageID,
		Content:       ContentBlock{Type: ContentText, Text: text},
	}
}

// AgentMessageChunk streams one chunk of the agent's reply.
func AgentMessageChunk(messageID, text string) Update {
	return Update{
		SessionUpdate: UpdateAgentMessageChunk,
		MessageID:     messageID,
		Content:       ContentBlock{Type: ContentText, Text: text},
	}
}

// PlanUpdate replaces the session's execution plan. entries must be
// the complete plan; clients discard the previous one.
func PlanUpdate(entries []PlanEntry) Update {
	return Update{SessionUpdate: UpdatePlan, Entries: entries}
}

// NewToolCall reports a tool invocation beginning; follow it with
// ToolCallUpdateFrame as it progresses.
func NewToolCall(call ToolCall) Update {
	return Update{
		SessionUpdate: UpdateToolCall,
		ToolCallID:    call.ToolCallID,
		Title:         call.Title,
		Kind:          call.Kind,
		Status:        call.Status,
		Content:       call.Content,
		RawInput:      call.RawInput,
		RawOutput:     call.RawOutput,
	}
}

// ToolCallUpdateFrame patches a tool call reported earlier. Only the
// fields set on u are transmitted.
func ToolCallUpdateFrame(u ToolCallUpdate) Update {
	return Update{
		SessionUpdate: UpdateToolCallUpdate,
		ToolCallID:    u.ToolCallID,
		Title:         strOrEmpty(u.Title),
		Kind:          strOrEmpty(u.Kind),
		Status:        strOrEmpty(u.Status),
		Content:       u.Content,
		RawInput:      u.RawInput,
		RawOutput:     u.RawOutput,
	}
}

// strOrEmpty dereferences a nil-able string pointer for the omitempty
// Update fields; nil stays "" so the key is omitted.
func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PromptText flattens prompt blocks the way the baseline allows: text
// blocks contribute their text, resource_link blocks contribute their
// URI. The server has already rejected other block types by the time
// an embedder sees them.
func PromptText(blocks []ContentBlock) string {
	out := ""
	for _, b := range blocks {
		switch b.Type {
		case ContentText, ContentResourceLink:
			if out != "" {
				out += "\n"
			}
			if b.Type == ContentText {
				out += b.Text
			} else {
				out += b.URI
			}
		}
	}
	return out
}
