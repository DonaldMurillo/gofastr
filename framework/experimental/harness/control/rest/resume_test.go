package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
)

// openEvents opens the SSE stream with an optional Last-Event-ID.
func openEvents(t *testing.T, url, tok, lastID string) (*http.Response, *streamBuf) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Harness-Token", tok)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("open SSE stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	buf := &streamBuf{}
	if resp.StatusCode == http.StatusOK {
		go func() { _, _ = io.Copy(buf, resp.Body) }()
	}
	return resp, buf
}

// A reconnect with Last-Event-ID gets the events it missed.
func TestSSEReplaysAfterLastEventID(t *testing.T) {
	s, sess, eng, tok, _, _ := newRevokedServer(t)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	for _, txt := range []string{"one", "two", "three"} {
		if _, err := eng.Bus.Publish(control.TextDelta{Text: txt}, ""); err != nil {
			t.Fatal(err)
		}
	}
	_, buf := openEvents(t, srv.URL+"/v1/sessions/"+string(sess)+"/events", tok, "1")
	ok := waitUntil(2*time.Second, func() bool {
		return strings.Contains(buf.snapshot(), "id: 3\n")
	})
	got := buf.snapshot()
	if !ok || !strings.Contains(got, "id: 2\n") {
		t.Fatalf("missed events not replayed: %q", got)
	}
	if strings.Contains(got, "id: 1\n") || strings.Contains(got, `"one"`) {
		t.Fatalf("event 1 replayed past Last-Event-ID: %q", got)
	}
	if strings.Index(got, "id: 2\n") > strings.Index(got, "id: 3\n") {
		t.Fatalf("replay out of order: %q", got)
	}
}

// A malformed Last-Event-ID is refused, not read as zero.
func TestSSERefusesBadLastEventID(t *testing.T) {
	s, sess, _, tok, _, _ := newRevokedServer(t)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	resp, _ := openEvents(t, srv.URL+"/v1/sessions/"+string(sess)+"/events", tok, "abc")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", resp.StatusCode)
	}
}

// A StreamGap has id 0 and must not reset the client's Last-Event-ID.
func TestSSEGapCarriesNoID(t *testing.T) {
	s, sess, eng, tok, _, _ := newRevokedServer(t)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	// Past the 1024-event window, with no log behind the bus.
	for range 1100 {
		if _, err := eng.Bus.Publish(control.TextDelta{Text: "x"}, ""); err != nil {
			t.Fatal(err)
		}
	}
	_, buf := openEvents(t, srv.URL+"/v1/sessions/"+string(sess)+"/events", tok, "0")
	if !waitUntil(3*time.Second, func() bool { return strings.Contains(buf.snapshot(), "id: 1100\n") }) {
		t.Fatalf("replay never reached the last event: %d bytes", len(buf.snapshot()))
	}
	got := buf.snapshot()
	if !strings.Contains(got, "event: StreamGap") {
		t.Fatalf("evicted range not marked with a StreamGap")
	}
	if strings.Contains(got, "id: 0\n") {
		t.Fatal("StreamGap written with id 0")
	}
}
