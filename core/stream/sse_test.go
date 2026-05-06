package stream

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// flushRecorder wraps httptest.ResponseRecorder to support http.Flusher.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed int
}

func (f *flushRecorder) Flush() {
	f.flushed++
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

// --- SSEWriter tests -------------------------------------------------------

func TestWriteEvent(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	err := sse.WriteEvent("update", "hello world")
	if err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}

	body := rec.Body.String()
	want := "event: update\ndata: hello world\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
	if rec.flushed == 0 {
		t.Error("expected Flush to be called")
	}
}

func TestWriteData(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	err := sse.WriteData("just data")
	if err != nil {
		t.Fatalf("WriteData: %v", err)
	}

	body := rec.Body.String()
	want := "data: just data\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

func TestWriteComment(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	err := sse.WriteComment("ping")
	if err != nil {
		t.Fatalf("WriteComment: %v", err)
	}

	body := rec.Body.String()
	want := ": ping\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

func TestMultipleEvents(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteData("first")
	_ = sse.WriteEvent("custom", "second")
	_ = sse.WriteComment("keepalive")
	_ = sse.WriteData("third")

	body := rec.Body.String()

	want := strings.Join([]string{
		"data: first\n\n",
		"event: custom\ndata: second\n\n",
		": keepalive\n\n",
		"data: third\n\n",
	}, "")

	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
	if rec.flushed != 4 {
		t.Errorf("expected 4 flushes, got %d", rec.flushed)
	}
}

func TestContentTypeHeader(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteData("x") // triggers ensureHeaders

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
}

func TestCacheControlHeader(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteData("x")

	cc := rec.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

func TestConnectionHeader(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteData("x")

	conn := rec.Header().Get("Connection")
	if conn != "keep-alive" {
		t.Errorf("Connection = %q, want %q", conn, "keep-alive")
	}
}

func TestHeadersNotSetBeforeFirstWrite(t *testing.T) {
	rec := newFlushRecorder()
	_ = NewSSEWriter(rec)

	// No write yet — headers should not be sent
	ct := rec.Header().Get("Content-Type")
	if ct != "" {
		t.Errorf("Content-Type should be empty before first write, got %q", ct)
	}
}

func TestSetRetry(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	sse.SetRetry(5)

	body := rec.Body.String()
	if !strings.Contains(body, "retry: 5\n") {
		t.Errorf("expected retry field in body, got %q", body)
	}
}

func TestSetID(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	sse.SetID("42")
	_ = sse.WriteData("payload")

	body := rec.Body.String()
	if !strings.Contains(body, "id: 42\n") {
		t.Errorf("expected id field in body, got %q", body)
	}
}

func TestSetIDOnlyAppliesOnce(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	sse.SetID("42")
	_ = sse.WriteData("first")
	_ = sse.WriteData("second")

	body := rec.Body.String()
	count := strings.Count(body, "id: 42\n")
	if count != 1 {
		t.Errorf("expected id to appear once, got %d occurrences", count)
	}
}

func TestMultilineData(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteData("line1\nline2\nline3")

	body := rec.Body.String()
	want := "data: line1\ndata: line2\ndata: line3\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

// --- LastEventID tests -----------------------------------------------------

func TestLastEventIDFromHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Last-Event-ID", "123")

	id := LastEventID(req)
	if id != "123" {
		t.Errorf("LastEventID = %q, want %q", id, "123")
	}
}

func TestLastEventIDFromQueryParam(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events?last_event_id=456", nil)

	id := LastEventID(req)
	if id != "456" {
		t.Errorf("LastEventID = %q, want %q", id, "456")
	}
}

func TestLastEventIDHeaderOverridesQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events?last_event_id=456", nil)
	req.Header.Set("Last-Event-ID", "123")

	id := LastEventID(req)
	if id != "123" {
		t.Errorf("LastEventID = %q, want %q (header should win)", id, "123")
	}
}

func TestLastEventIDEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	id := LastEventID(req)
	if id != "" {
		t.Errorf("LastEventID = %q, want empty", id)
	}
}

// --- Convenience method tests ----------------------------------------------

func TestWriteMessage(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteMessage("hello")

	body := rec.Body.String()
	want := "event: message\ndata: hello\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

func TestWriteError(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteError("something broke")

	body := rec.Body.String()
	want := "event: error\ndata: something broke\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

func TestWriteDone(t *testing.T) {
	rec := newFlushRecorder()
	sse := NewSSEWriter(rec)

	_ = sse.WriteDone()

	body := rec.Body.String()
	want := "event: done\ndata: [DONE]\n\n"
	if body != want {
		t.Errorf("got %q, want %q", body, want)
	}
}

// --- Encode tests ----------------------------------------------------------

func TestEncodeMessage(t *testing.T) {
	e := Event{Type: Message, Data: "hello"}
	got := Encode(e)
	want := "data: hello\n\n"
	if got != want {
		t.Errorf("Encode(Message) = %q, want %q", got, want)
	}
}

func TestEncodeError(t *testing.T) {
	e := Event{Type: Error, Data: "fail"}
	got := Encode(e)
	want := "event: error\ndata: fail\n\n"
	if got != want {
		t.Errorf("Encode(Error) = %q, want %q", got, want)
	}
}

func TestEncodeDone(t *testing.T) {
	e := Event{Type: Done, Data: "[DONE]"}
	got := Encode(e)
	want := "event: done\ndata: [DONE]\n\n"
	if got != want {
		t.Errorf("Encode(Done) = %q, want %q", got, want)
	}
}

func TestEncodeCustom(t *testing.T) {
	e := Event{Type: Custom, Name: "update", Data: "payload"}
	got := Encode(e)
	want := "event: update\ndata: payload\n\n"
	if got != want {
		t.Errorf("Encode(Custom) = %q, want %q", got, want)
	}
}

func TestEncodeWithID(t *testing.T) {
	e := Event{Type: Message, Data: "hi", ID: "99"}
	got := Encode(e)
	want := "id: 99\ndata: hi\n\n"
	if got != want {
		t.Errorf("Encode with ID = %q, want %q", got, want)
	}
}

func TestEncodeMultiline(t *testing.T) {
	e := Event{Type: Message, Data: "a\nb\nc"}
	got := Encode(e)
	want := "data: a\ndata: b\ndata: c\n\n"
	if got != want {
		t.Errorf("Encode multiline = %q, want %q", got, want)
	}
}

// --- ChunkedWriter tests ---------------------------------------------------

func TestChunkedWriteChunk(t *testing.T) {
	rec := newFlushRecorder()
	cw := NewChunkedWriter(rec)

	err := cw.WriteChunk([]byte("hello"))
	if err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	if rec.Body.String() != "hello" {
		t.Errorf("got %q, want %q", rec.Body.String(), "hello")
	}
	if rec.flushed == 0 {
		t.Error("expected Flush after WriteChunk")
	}
}

func TestChunkedMultipleWrites(t *testing.T) {
	rec := newFlushRecorder()
	cw := NewChunkedWriter(rec)

	_ = cw.WriteChunk([]byte("aaa"))
	_ = cw.WriteChunk([]byte("bbb"))

	if rec.Body.String() != "aaabbb" {
		t.Errorf("got %q, want %q", rec.Body.String(), "aaabbb")
	}
	if rec.flushed != 2 {
		t.Errorf("expected 2 flushes, got %d", rec.flushed)
	}
}

func TestChunkedClose(t *testing.T) {
	rec := newFlushRecorder()
	cw := NewChunkedWriter(rec)

	_ = cw.WriteChunk([]byte("data"))
	_ = cw.Close()

	// Close adds one more flush
	if rec.flushed != 2 {
		t.Errorf("expected 2 flushes (write + close), got %d", rec.flushed)
	}

	// Close is idempotent
	_ = cw.Close()
	if rec.flushed != 3 {
		t.Errorf("expected 3 flushes after second Close, got %d", rec.flushed)
	}
}
