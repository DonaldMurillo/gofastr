package journal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofastr/gofastr/kiln/world"
)

// Kind discriminates Entry payloads.
type Kind string

const (
	KindWorldEdit     Kind = "world_edit"
	KindChatUser      Kind = "chat_user"
	KindChatAssistant Kind = "chat_assistant"
	KindToolCall      Kind = "tool_call"
	KindToolResult    Kind = "tool_result"
	KindPlanProposed  Kind = "plan_proposed"
	KindPlanApproved  Kind = "plan_approved"
)

// Op names a world mutation operation. Only set when Kind == KindWorldEdit.
type Op string

const (
	OpAddEntity     Op = "add_entity"
	OpUpdateEntity  Op = "update_entity"
	OpDeleteEntity  Op = "delete_entity"
	OpAddField      Op = "add_field"
	OpDeleteField   Op = "delete_field"
	OpAddPage       Op = "add_page"
	OpDeletePage    Op = "delete_page"
	OpAddHook       Op = "add_hook"
	OpDeleteHook    Op = "delete_hook"
	OpAddRoute      Op = "add_route"
	OpDeleteRoute   Op = "delete_route"
	OpAddSeed       Op = "add_seed"
	OpAddMiddleware Op = "add_middleware"
	OpSetAppConfig  Op = "set_app_config"
)

// Entry is one record in the append-only log. Payload is held as raw JSON
// so unknown kinds and ops survive a round-trip without losing data.
type Entry struct {
	ID        string          `json:"id"`
	Timestamp time.Time       `json:"ts"`
	Kind      Kind            `json:"kind"`
	Op        Op              `json:"op,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// NewEntry builds an Entry around a Go-typed payload. The caller supplies
// the ID (typically a ULID) so external callers control monotonicity.
func NewEntry(id string, ts time.Time, kind Kind, op Op, payload any) (Entry, error) {
	var raw json.RawMessage
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return Entry{}, fmt.Errorf("journal: marshal payload: %w", err)
		}
		raw = buf
	}
	return Entry{
		ID:        id,
		Timestamp: ts,
		Kind:      kind,
		Op:        op,
		Payload:   raw,
	}, nil
}

// Decode unmarshals an Entry's Payload into v.
func (e Entry) Decode(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, v)
}

// --- Payload types for world-edit ops -----------------------------------

type AddEntityPayload struct {
	Entity *world.Entity `json:"entity"`
}

type UpdateEntityPayload struct {
	Entity *world.Entity `json:"entity"`
	Prev   *world.Entity `json:"prev,omitempty"`
}

type DeleteEntityPayload struct {
	Name string        `json:"name"`
	Prev *world.Entity `json:"prev,omitempty"`
}

type AddFieldPayload struct {
	Entity string      `json:"entity"`
	Field  world.Field `json:"field"`
}

type DeleteFieldPayload struct {
	Entity string       `json:"entity"`
	Field  string       `json:"field"`
	Prev   *world.Field `json:"prev,omitempty"`
}

type AddPagePayload struct {
	Page *world.Page `json:"page"`
}

type DeletePagePayload struct {
	Path string      `json:"path"`
	Prev *world.Page `json:"prev,omitempty"`
}

type AddHookPayload struct {
	Hook *world.Hook `json:"hook"`
}

type DeleteHookPayload struct {
	ID   string      `json:"id"`
	Prev *world.Hook `json:"prev,omitempty"`
}

type AddRoutePayload struct {
	Route *world.Route `json:"route"`
}

type DeleteRoutePayload struct {
	Method string       `json:"method"`
	Path   string       `json:"path"`
	Prev   *world.Route `json:"prev,omitempty"`
}

type AddSeedPayload struct {
	Seed *world.Seed `json:"seed"`
}

type AddMiddlewarePayload struct {
	Middleware *world.Middleware `json:"middleware"`
}

type SetAppConfigPayload struct {
	Config world.AppConfig  `json:"config"`
	Prev   *world.AppConfig `json:"prev,omitempty"`
}

// --- Payload types for chat / plan kinds --------------------------------

type ChatMessagePayload struct {
	Text string `json:"text"`
}

type ToolCallPayload struct {
	CallID string         `json:"call_id"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
}

type ToolResultPayload struct {
	CallID string `json:"call_id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Hint   string `json:"hint,omitempty"`
}

type PlanProposedPayload struct {
	PlanID string   `json:"plan_id"`
	Steps  []string `json:"steps"`
	Reason string   `json:"reason,omitempty"`
}

type PlanApprovedPayload struct {
	PlanID   string `json:"plan_id"`
	Modified bool   `json:"modified,omitempty"`
}
