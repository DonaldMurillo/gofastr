package mcp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// sseEvent is one dispatched SSE event: its event name and the data buffer
// reconstructed per the HTML/SSE spec (data: lines joined with "\n").
type sseEvent struct {
	event string
	data  string
}

// parseSSEEvents parses an SSE byte stream per the HTML spec and returns
// the dispatched events. It is the stand-in for any spec-compliant SSE
// consumer (there is no MCP-side decoder: the transport only writes SSE;
// reading is the client's job). Used to prove framing round-trips.
func parseSSEEvents(stream string) []sseEvent {
	// Normalize line endings (CRLF / CR → LF) per spec.
	stream = strings.ReplaceAll(stream, "\r\n", "\n")
	stream = strings.ReplaceAll(stream, "\r", "\n")

	var events []sseEvent
	var cur sseEvent
	var dataLines []string
	dispatch := func() {
		// A blank line dispatches an event only if there is data or an
		// event name was set; comments / heartbearts dispatch nothing.
		if len(dataLines) > 0 || cur.event != "" {
			cur.data = strings.Join(dataLines, "\n")
			events = append(events, cur)
		}
		cur = sseEvent{}
		dataLines = nil
	}
	for _, line := range strings.Split(stream, "\n") {
		if line == "" {
			dispatch()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment
		}
		field, value, hasColon := strings.Cut(line, ":")
		if hasColon && strings.HasPrefix(value, " ") {
			value = value[1:] // one optional leading space after ':'
		}
		switch field {
		case "event":
			cur.event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	return events
}

// TestStreamSSE_RoundTripsJSONRPCPayload verifies that a JSON-RPC payload
// whose string content contains "event:" and "data:" survives a trip
// through the SSE writer and parses back identically. Previously
// neutralizeSSEDataPayload rewrote those substrings inside the serialized
// JSON ("event:"→"event ", "data:"→"data "), corrupting the payload.
func TestStreamSSE_RoundTripsJSONRPCPayload(t *testing.T) {
	original := map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(7),
		"result": map[string]any{
			"msg": "draft event: sale and more data: leaked",
		},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var buf bytes.Buffer
	StreamSSE(&buf, "message", string(encoded))

	evs := parseSSEEvents(buf.String())
	if len(evs) != 1 {
		t.Fatalf("expected exactly 1 dispatched event, got %d; raw=%q", len(evs), buf.String())
	}
	if evs[0].event != "message" {
		t.Errorf("event name = %q, want %q", evs[0].event, "message")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(evs[0].data), &decoded); err != nil {
		t.Fatalf("dispatched data is not valid JSON (%v); raw=%q", err, evs[0].data)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Errorf("SECURITY: [mcp-sse] JSON-RPC payload corrupted by SSE framing.\n got %#v\nwant %#v\n raw data: %q", decoded, original, evs[0].data)
	}
}

// TestStreamSSE_MultilineDataRoundTrips verifies a payload containing real
// newlines plus the SSE-directive-looking substrings is delivered intact
// via spec multi-line `data:` framing (one data: line per \n), and does
// NOT inject a second event frame.
func TestStreamSSE_MultilineDataRoundTrips(t *testing.T) {
	payload := "line1\nline2 has event: x\ndata: y\nline4"

	var buf bytes.Buffer
	StreamSSE(&buf, "message", payload)

	evs := parseSSEEvents(buf.String())
	if len(evs) != 1 {
		t.Fatalf("SECURITY: [mcp-sse] multiline payload injected %d events (want 1); raw=%q", len(evs), buf.String())
	}
	if evs[0].event != "message" {
		t.Errorf("event name = %q, want %q", evs[0].event, "message")
	}
	if evs[0].data != payload {
		t.Errorf("SECURITY: [mcp-sse] multiline data not delivered intact.\n got %q\nwant %q", evs[0].data, payload)
	}
}
