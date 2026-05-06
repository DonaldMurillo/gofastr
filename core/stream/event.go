package stream

import (
	"fmt"
	"strings"
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

// Encode formats an Event as a W3C-compliant SSE frame.
//
// The output uses the following fields:
//   - "id:" when Event.ID is non-empty
//   - "event:" for Error, Done, and Custom types
//   - "data:" for the payload (multi-line data splits on \n)
//   - terminated by a blank line ("\n\n")
func Encode(e Event) string {
	var b strings.Builder

	// id field
	if e.ID != "" {
		b.WriteString("id: ")
		b.WriteString(e.ID)
		b.WriteByte('\n')
	}

	// event field
	switch e.Type {
	case Error:
		b.WriteString("event: error\n")
	case Done:
		b.WriteString("event: done\n")
	case Custom:
		if e.Name != "" {
			b.WriteString("event: ")
			b.WriteString(e.Name)
			b.WriteByte('\n')
		}
	case Message:
		// W3C spec: omit event field → defaults to "message"
	}

	// data field — split on newlines per spec
	for _, line := range strings.Split(e.Data, "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}

	// blank line terminates the event
	b.WriteByte('\n')
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
