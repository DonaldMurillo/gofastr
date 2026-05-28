package stream

import (
	"strings"
	"testing"
)

func TestEventEncode_StripsNewlinesFromID(t *testing.T) {
	out := Encode(Event{ID: "42\nevent: forged", Data: "hello"})
	if strings.Contains(out, "event: forged") {
		t.Fatalf("SECURITY: [sse-event] Event.Encode injected forged SSE field from ID: %q", out)
	}
}

func TestEventEncode_StripsNULFromID(t *testing.T) {
	out := Encode(Event{ID: "42\x00event: forged", Data: "hello"})
	if strings.Contains(out, "\x00") || strings.Contains(out, "event: forged") {
		t.Fatalf("SECURITY: [sse-event] Event.Encode retained control payload in ID: %q", out)
	}
}

func TestEventEncode_StripsNewlinesFromCustomEventName(t *testing.T) {
	out := Encode(Event{Type: Custom, Name: "update\ndata: forged", Data: "hello"})
	if strings.Contains(out, "data: forged") {
		t.Fatalf("SECURITY: [sse-event] Event.Encode injected forged SSE field from custom name: %q", out)
	}
}

func TestEventEncode_StripsNULFromCustomEventName(t *testing.T) {
	out := Encode(Event{Type: Custom, Name: "update\x00data: forged", Data: "hello"})
	if strings.Contains(out, "\x00") || strings.Contains(out, "data: forged") {
		t.Fatalf("SECURITY: [sse-event] Event.Encode retained control payload in custom name: %q", out)
	}
}
