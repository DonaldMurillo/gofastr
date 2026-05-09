package journal

import (
	"time"

	"github.com/gofastr/gofastr/kiln/world"
)

// Session is the materialized state derived from replaying a journal.
type Session struct {
	World *world.World     `json:"world"`
	Chat  []ChatEvent      `json:"chat"`
	Plans map[string]*Plan `json:"plans"`
}

// ChatEvent is one entry on the conversation timeline. Exactly one of
// Message, Call, or Result is non-nil based on Kind.
type ChatEvent struct {
	EntryID   string              `json:"entry_id"`
	Timestamp time.Time           `json:"ts"`
	Kind      Kind                `json:"kind"`
	Message   *ChatMessagePayload `json:"message,omitempty"`
	Call      *ToolCallPayload    `json:"call,omitempty"`
	Result    *ToolResultPayload  `json:"result,omitempty"`
}

// Plan represents a multi-step plan proposed by the agent. Approved plans
// retain ApprovedAt; unapproved plans have a zero ApprovedAt.
type Plan struct {
	PlanID     string    `json:"plan_id"`
	ProposedAt time.Time `json:"proposed_at"`
	Steps      []string  `json:"steps"`
	Reason     string    `json:"reason,omitempty"`
	Approved   bool      `json:"approved,omitempty"`
	ApprovedAt time.Time `json:"approved_at,omitempty"`
	Modified   bool      `json:"modified,omitempty"`
}

// NewSession returns an empty Session with an empty world.
func NewSession() *Session {
	return &Session{
		World: world.New(),
		Plans: map[string]*Plan{},
	}
}
