package stream

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/sseframe"
)

// EventType represents the kind of SSE event.
type EventType int

const (
	// Message is a standard data event.
	Message EventType = iota
	// Error signals an error to the client.
	Error
	// Done is the terminal sentinel event.
	Done
	// Custom is a named event with an arbitrary event type string.
	Custom
)

// Event represents a single Server-Sent Event.
type Event struct {
	Type EventType
	Name string // used when Type == Custom
	Data string
	ID   string // optional Last-Event-ID value
}

// Encode formats an Event as one SSE frame. The ID and a Custom name are
// cut at their first CR, LF or NUL, CR and NUL are removed from Data, and
// each line of Data gets its own "data:" field.
func Encode(e Event) string {
	var name string
	switch e.Type {
	case Error:
		name = "error"
	case Done:
		name = "done"
	case Custom:
		name = e.Name
	}
	var b strings.Builder
	sseframe.Append(&b, e.ID, name, false, e.Data)
	return b.String()
}

// String returns a human-readable label for the EventType.
func (t EventType) String() string {
	switch t {
	case Message:
		return "message"
	case Error:
		return "error"
	case Done:
		return "done"
	case Custom:
		return "custom"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}
