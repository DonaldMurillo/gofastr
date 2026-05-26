package stream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSE_EventNameInjection verifies that event names containing newlines
// don't break the SSE protocol. Attack: injecting false events via newline
// in event name.
func TestSSE_EventNameInjection(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)

	maliciousEvent := "message\nevent: injected"
	sw.WriteEvent(maliciousEvent, "data")

	body := rr.Body.String()
	// The event name should not create a new "event:" line
	lines := strings.Split(body, "\n")
	eventCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			eventCount++
		}
	}
	if eventCount > 1 {
		t.Errorf("SECURITY: [sse] event name with newline created %d event directives (want 1). Attack: SSE protocol injection via malicious event name.", eventCount)
	}
}

// TestSSE_DataInjection verifies that data containing SSE protocol
// directives is handled. Attack: injecting "event: evil" in data field.
func TestSSE_DataInjection(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)

	maliciousData := "normal\nevent: injected\ndata: pwned"
	sw.WriteData(maliciousData)

	body := rr.Body.String()
	// SSE multi-line data is split and prefixed with "data: " per line
	// So "event: injected" should become "data: event: injected" — safe.
	if strings.Contains(body, "event: injected\n") && !strings.Contains(body, "data: event: injected") {
		t.Errorf("SECURITY: [sse] raw event directive in data not prefixed. Attack: SSE injection via data field.")
	}
}

// TestSSE_CommentInjection verifies that SSE comments with control
// characters are handled. Attack: injecting via comment field.
func TestSSE_CommentInjection(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)

	sw.WriteComment("test\r\nSet-Cookie: evil=true")
	body := rr.Body.String()

	if strings.Contains(body, "Set-Cookie:") {
		t.Errorf("SECURITY: [sse] comment field injected HTTP header. Attack: header injection via SSE comment.")
	}
}

// TestSSE_CorrectContentType verifies that the Content-Type is set to
// text/event-stream. Attack: wrong content-type allowing browser
// interpretation as HTML.
func TestSSE_CorrectContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)
	sw.WriteData("test")

	ct := rr.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("SECURITY: [sse] Content-Type = %q (want text/event-stream). Attack: wrong content-type may allow HTML rendering.", ct)
	}
}

// TestSSE_CacheControlSet verifies that Cache-Control: no-cache is set
// to prevent caching of streaming data. Attack: CDN/proxy caches
// streaming responses.
func TestSSE_CacheControlSet(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)
	sw.WriteData("test")

	cc := rr.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("SECURITY: [sse] Cache-Control = %q (want no-cache). Attack: cached SSE responses may serve stale data or leak to other users.", cc)
	}
}

// TestSSE_LastEventIDFromHeader verifies that Last-Event-ID is read from
// the request header. Attack: injecting via query param when header is
// more trusted.
func TestSSE_LastEventIDFromHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Last-Event-ID", "event-42")

	got := LastEventID(req)
	if got != "event-42" {
		t.Errorf("LastEventID from header = %q, want %q", got, "event-42")
	}
}

// TestSSE_LastEventIDQueryFallback verifies that Last-Event-ID falls back
// to query parameter when header is absent.
func TestSSE_LastEventIDQueryFallback(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream?last_event_id=event-99", nil)

	got := LastEventID(req)
	if got != "event-99" {
		t.Errorf("LastEventID from query = %q, want %q", got, "event-99")
	}
}

// TestSSE_LastEventIDHeaderPriority verifies that the header value takes
// priority over query parameter when both are present.
func TestSSE_LastEventIDHeaderPriority(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream?last_event_id=query-id", nil)
	req.Header.Set("Last-Event-ID", "header-id")

	got := LastEventID(req)
	if got != "header-id" {
		t.Errorf("SECURITY: [sse] LastEventID = %q (want header-id, not query-id). Attack: query param override of trusted header value.", got)
	}
}
