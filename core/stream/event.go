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
//
// ID and custom event names are truncated at the first CR/LF/NUL — those
// bytes terminate an SSE field and would let a caller-supplied value
// inject forged directives ("event: forged", "data: pwned"…) below it.
// Multi-line data is split on '\n' and each line is re-prefixed with
// "data: " so an injected blank line ("\n\n") still appears as a single
// event frame to the client. CRs and NULs in data are stripped for the
// same reason.
func Encode(e Event) string {
	var b strings.Builder

	// id field
	if id := stripSSEControlChars(e.ID); id != "" {
		b.WriteString("id: ")
		b.WriteString(id)
		b.WriteByte('\n')
	}

	// event field
	switch e.Type {
	case Error:
		b.WriteString("event: error\n")
	case Done:
		b.WriteString("event: done\n")
	case Custom:
		if name := stripSSEControlChars(e.Name); name != "" {
			b.WriteString("event: ")
			b.WriteString(name)
			b.WriteByte('\n')
		}
	case Message:
		// W3C spec: omit event field → defaults to "message"
	}

	// data field — split on newlines per spec; strip CR/NUL per line so
	// no caller-supplied bytes can re-introduce a field boundary inside
	// a single emitted line.
	data := strings.ReplaceAll(e.Data, "\r", "")
	data = strings.ReplaceAll(data, "\x00", "")
	for _, line := range strings.Split(data, "\n") {
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
