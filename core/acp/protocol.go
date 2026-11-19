package acp

import "encoding/json"

// ProtocolVersion is the ACP major version this package speaks.
const ProtocolVersion = 1

// JSON-RPC 2.0 error codes plus the ACP-specific reserved range (-32000 to
// -32099).
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
type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// AuthClientCapabilities is the client's auth-related capability set.
type AuthClientCapabilities struct {
	// Terminal licenses terminal-type auth methods.
	Terminal bool `json:"terminal"`
}

// ClientCapabilities is what the client offered at initialize.
type ClientCapabilities struct {
	Auth     *AuthClientCapabilities `json:"auth,omitempty"`
	FS       *FileSystemCapabilities `json:"fs,omitempty"`
	Terminal bool                    `json:"terminal,omitempty"`
}

// PromptCapabilities declares which extra content types the agent accepts in
// session/prompt.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// McpCapabilities declares whether the agent can connect to MCP servers on
// the client's behalf.
type McpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AgentCapabilities is the capability block initialize returns.
type AgentCapabilities struct {
	// LoadSession is true only when the embedded agent implements
	// SessionLoader.
	LoadSession bool `json:"loadSession"`

	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
	McpCapabilities    McpCapabilities    `json:"mcpCapabilities"`
}

// AuthMethod describes one way the client could authenticate.
type AuthMethod struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Type        string            `json:"type,omitempty"` // "" (= agent) | "terminal"
	Args        []string          `json:"args,omitempty"` // terminal methods only
	Env         map[string]string `json:"env,omitempty"`  // terminal methods only
}

// ContentBlock is one displayable piece of a message.
type ContentBlock struct {
	Type string `json:"type"` // "text" | "resource_link"
	Text string `json:"text,omitempty"`
	URI  string `json:"uri,omitempty"`  // resource_link
	Name string `json:"name,omitempty"` // resource_link
}

// MarshalJSON writes the fields the block's type requires even when empty.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	type plain ContentBlock
	switch b.Type {
	case ContentText:
		return json.Marshal(struct {
			plain
			Text string `json:"text"`
		}{plain(b), b.Text})
	case ContentResourceLink:
		return json.Marshal(struct {
			plain
			URI  string `json:"uri"`
			Name string `json:"name"`
		}{plain(b), b.URI, b.Name})
	}
	return json.Marshal(plain(b))
}

// Content block type constants.
const (
	ContentText         = "text"
	ContentResourceLink = "resource_link"
)

// Tool call kinds (ToolKind in the ACP schema).
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

// ToolCallContent is one content item attached to a tool call: a wrapped
// content block, a file diff, or a terminal reference.
type ToolCallContent struct {
	Type       string        `json:"type"` // "content" | "diff" | "terminal"
	Content    *ContentBlock `json:"content,omitempty"`
	Path       string        `json:"path,omitempty"`    // diff
	OldText    *string       `json:"oldText,omitempty"` // diff
	NewText    string        `json:"newText,omitempty"` // diff
	TerminalID string        `json:"terminalId,omitempty"`
}

// MarshalJSON writes the fields the item's type requires even when empty.
func (c ToolCallContent) MarshalJSON() ([]byte, error) {
	type plain ToolCallContent
	switch c.Type {
	case "diff":
		return json.Marshal(struct {
			plain
			Path    string `json:"path"`
			NewText string `json:"newText"`
		}{plain(c), c.Path, c.NewText})
	case "terminal":
		return json.Marshal(struct {
			plain
			TerminalID string `json:"terminalId"`
		}{plain(c), c.TerminalID})
	}
	return json.Marshal(plain(c))
}

// TextToolContent wraps text as a tool-call content item.
func TextToolContent(text string) ToolCallContent {
	return ToolCallContent{Type: "content", Content: &ContentBlock{Type: ContentText, Text: text}}
}

// ToolCall reports a tool invocation starting (sessionUpdate "tool_call").
type ToolCall struct {
	ToolCallID string            `json:"toolCallId"`
	Title      string            `json:"title"`
	Kind       string            `json:"kind,omitempty"`
	Status     string            `json:"status,omitempty"`
	Content    []ToolCallContent `json:"content,omitempty"`
	RawInput   map[string]any    `json:"rawInput,omitempty"`
	RawOutput  map[string]any    `json:"rawOutput,omitempty"`
}

// ToolCallUpdate patches a previously reported tool call (sessionUpdate
// "tool_call_update"); a nil field is left unchanged.
type ToolCallUpdate struct {
	ToolCallID string            `json:"toolCallId"`
	Title      *string           `json:"title,omitempty"`
	Kind       *string           `json:"kind,omitempty"`
	Status     *string           `json:"status,omitempty"`
	Content    []ToolCallContent `json:"content,omitempty"`
	RawInput   map[string]any    `json:"rawInput,omitempty"`
	RawOutput  map[string]any    `json:"rawOutput,omitempty"`
}

// Plan entry priorities and statuses (PlanEntryPriority / PlanEntryStatus in
// the ACP schema).
const (
	PlanPriorityHigh   = "high"
	PlanPriorityMedium = "medium"
	PlanPriorityLow    = "low"

	PlanStatusPending    = "pending"
	PlanStatusInProgress = "in_progress"
	PlanStatusCompleted  = "completed"
)

// PlanEntry is one task in an execution plan.
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

	set patchFields
}

type patchFields struct{ title, content, rawInput, rawOutput bool }

// MarshalJSON emits the sessionUpdate discriminator first, then the fields in
// schema order, omitting everything the constructor left unset.
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
	if u.Title != "" || u.set.title {
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
	case []ToolCallContent:
		if len(c) > 0 || u.set.content {
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
	if u.SessionUpdate == UpdatePlan {
		entries := u.Entries
		if entries == nil {
			entries = []PlanEntry{}
		}
		if err := appendKV("entries", entries); err != nil {
			return nil, err
		}
	} else if len(u.Entries) > 0 {
		if err := appendKV("entries", u.Entries); err != nil {
			return nil, err
		}
	}
	if len(u.RawInput) > 0 || u.set.rawInput {
		if err := appendKV("rawInput", u.RawInput); err != nil {
			return nil, err
		}
	}
	if len(u.RawOutput) > 0 || u.set.rawOutput {
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
// replaying a loaded session).
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

// PlanUpdate replaces the session's execution plan with the complete entries.
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

// ToolCallUpdateFrame patches a tool call reported earlier; a field set to
// an empty value is sent empty, which clears it.
func ToolCallUpdateFrame(u ToolCallUpdate) Update {
	return Update{
		set: patchFields{
			title:     u.Title != nil,
			content:   u.Content != nil,
			rawInput:  u.RawInput != nil,
			rawOutput: u.RawOutput != nil,
		},
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

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// PromptText flattens prompt blocks the way the baseline allows: text blocks
// contribute their text, resource_link blocks contribute their URI.
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
