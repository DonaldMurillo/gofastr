package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRespondSSE_StripsNewlinesFromEventName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()

	Respond(rec, req, SSE{Event: "update\nretry: 0", Data: "safe"})

	body := rec.Body.String()
	if strings.Contains(body, "retry: 0") {
		t.Fatalf("SECURITY: [sse] event name newline injected extra SSE control field: %q", body)
	}
}

func TestRespondSSE_StripsNewlinesFromEventID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()

	Respond(rec, req, SSE{ID: "42\nretry: 0", Event: "update", Data: "safe"})

	body := rec.Body.String()
	if strings.Contains(body, "retry: 0") {
		t.Fatalf("SECURITY: [sse] event id newline injected extra SSE control field: %q", body)
	}
}

func TestRespondSSE_DataCannotInjectSecondEvent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	rec := httptest.NewRecorder()

	Respond(rec, req, SSE{
		Event: "update",
		Data:  "hello\n\nid: forged\nevent: admin\ndata: owned",
	})

	body := rec.Body.String()
	if strings.Contains(body, "\n\nevent: admin") || strings.Contains(body, "\n\nid: forged") {
		t.Fatalf("SECURITY: [sse] data payload injected a second SSE frame: %q", body)
	}
}

func TestSSEStream_DataCannotInjectSecondEvent(t *testing.T) {
	events := make(chan SSE, 1)
	events <- SSE{
		Event: "update",
		Data:  "hello\n\nid: forged\nevent: admin\ndata: owned",
	}
	close(events)

	rec := httptest.NewRecorder()
	SSEStream(rec, events)

	body := rec.Body.String()
	if strings.Contains(body, "\n\nevent: admin") || strings.Contains(body, "\n\nid: forged") {
		t.Fatalf("SECURITY: [sse] stream data payload injected a second SSE frame: %q", body)
	}
}
