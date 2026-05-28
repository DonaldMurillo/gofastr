package stream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSSE_DataStripsCRandNUL pins that WriteData / WriteEvent never
// pass CR or NUL through inside the data payload. CR terminates a field
// on WHATWG-spec SSE parsers; NUL on legacy clients. Without this
// scrub, a `data: foo\rid: evil` payload would split into two fields.
func TestSSE_DataStripsCRandNUL(t *testing.T) {
	cases := []string{
		"hello\rretry: 0",
		"hello\nevent: forged",
		"hello\x00id: forged",
		"line1\r\nline2",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			rec := newFlushRecorder()
			sw := NewSSEWriter(rec)
			if err := sw.WriteData(p); err != nil {
				t.Fatalf("WriteData: %v", err)
			}
			body := rec.Body.String()
			if strings.Contains(body, "\r") || strings.Contains(body, "\x00") {
				t.Fatalf("WriteData leaked CR/NUL into wire: %q", body)
			}
		})
	}
}

// TestSSE_SetRetryDropsNonPositive verifies the writer never emits a
// `retry: 0` (or negative) line — that directive tells EventSource to
// reconnect with zero delay, an accidental DoS amplifier.
func TestSSE_SetRetryDropsNonPositive(t *testing.T) {
	for _, v := range []int{0, -1, -100} {
		rec := newFlushRecorder()
		sw := NewSSEWriter(rec)
		sw.SetRetry(v)
		body := rec.Body.String()
		if strings.Contains(body, "retry:") {
			t.Fatalf("SetRetry(%d) leaked retry directive: %q", v, body)
		}
	}
}

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

func TestSSE_LastEventIDHeaderStripsNewlines(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Last-Event-ID", "42\nevent: forged")

	got := LastEventID(req)
	if got != "42" {
		t.Fatalf("SECURITY: [sse] LastEventID header retained control payload %q. Attack: forged SSE fields on resume.", got)
	}
}

func TestSSE_LastEventIDHeaderStripsNUL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	req.Header.Set("Last-Event-ID", "42\x00event: forged")

	got := LastEventID(req)
	if got != "42" {
		t.Fatalf("SECURITY: [sse] LastEventID header retained NUL/control payload %q. Attack: resume-token protocol smuggling.", got)
	}
}

func TestSSE_LastEventIDQueryStripsNewlines(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream?last_event_id=42%0Aevent:%20forged", nil)

	got := LastEventID(req)
	if got != "42" {
		t.Fatalf("SECURITY: [sse] last_event_id query retained control payload %q. Attack: forged SSE fields via query resume token.", got)
	}
}

func TestSSE_LastEventIDQueryStripsNUL(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/stream?last_event_id=42%00event:%20forged", nil)

	got := LastEventID(req)
	if got != "42" {
		t.Fatalf("SECURITY: [sse] last_event_id query retained NUL/control payload %q. Attack: resume-token protocol smuggling via query parameter.", got)
	}
}

func TestSSE_WriteData_StripsInjectedIDNewlines(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)

	sw.SetID("42\nevent: forged")
	_ = sw.WriteData("hello")

	body := rr.Body.String()
	if strings.Contains(body, "event: forged") {
		t.Fatalf("SECURITY: [sse] queued ID injected a forged SSE field via WriteData: %q", body)
	}
}

func TestSSE_WriteData_StripsInjectedIDNUL(t *testing.T) {
	rr := httptest.NewRecorder()
	sw := NewSSEWriter(rr)

	sw.SetID("42\x00event: forged")
	_ = sw.WriteData("hello")

	body := rr.Body.String()
	if strings.Contains(body, "\x00") || strings.Contains(body, "event: forged") {
		t.Fatalf("SECURITY: [sse] queued ID retained NUL/control payload via WriteData: %q", body)
	}
}
