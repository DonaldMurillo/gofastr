package live_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
)

// Property: no CR/LF in outbound SSE field bodies. ServeSSE is a
// hand-rolled SSE writer; unlike the framework's core/stream.SSEWriter
// (which truncates the event name at the first CR/LF/NUL via
// stripSSEControlChars), it must not let a CR/LF inside Event.Kind
// terminate the `event:` field and spawn injected SSE field lines.
// Live.Notify is exported and accepts arbitrary kind strings, so the
// property is asserted at the writer surface, not at Notify's callers.
func TestServeSSEScrubEventKind(t *testing.T) {
	cases := []struct {
		name      string
		kind      string
		summary   string
		wantEvent string // non-empty: the exact `event:` line expected
	}{
		{"clean kind streams intact", "world_edit", "clean-marker", "event: world_edit"},
		{"LF in kind cannot inject fields", "world_edit\ndata: injected", "lf-marker", ""},
		{"CR in kind cannot inject fields", "world_edit\rdata: injected", "cr-marker", ""},
		{"CRLF in kind cannot inject fields", "world_edit\r\ndata: injected", "crlf-marker", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := newTestLive(t, journal.NewMemory())

			srv := httptest.NewServer(http.HandlerFunc(l.ServeSSE))
			defer srv.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Per-test client whose transport closes idle conns at cleanup
			// (same rationale as TestSSEHandlerStreamsEvents: pooled
			// keep-alive connections exhaust the macOS ephemeral range
			// under parallel package execution).
			tr := &http.Transport{}
			t.Cleanup(tr.CloseIdleConnections)
			client := &http.Client{Transport: tr}

			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/.kiln/events", nil)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("connect: %v", err)
			}
			defer resp.Body.Close()

			// The handler writes `event: ready` only after subscribing,
			// so seeing it proves a subsequent Notify cannot race the
			// subscription.
			buf := bufio.NewReader(resp.Body)
			var body []byte
			deadline := time.Now().Add(3 * time.Second)
			readUntil := func(marker string) bool {
				for time.Now().Before(deadline) {
					b, err := buf.ReadByte()
					if err == io.EOF {
						return false
					}
					if err != nil {
						continue
					}
					body = append(body, b)
					if strings.Contains(string(body), marker) {
						return true
					}
				}
				return false
			}
			if !readUntil("event: ready") {
				t.Fatalf("stream never opened; body so far: %q", body)
			}

			l.Notify(tc.kind, tc.summary)
			if !readUntil("\"summary\":\"" + tc.summary + "\"") {
				t.Fatalf("event never streamed; body so far: %q", body)
			}
			cancel()

			// Mirror the WHATWG SSE parser: a field is terminated by CR,
			// LF, or CRLF, so normalize CR before splitting into lines.
			lines := strings.Split(strings.ReplaceAll(string(body), "\r", "\n"), "\n")
			for _, ln := range lines {
				if strings.HasPrefix(ln, "data: injected") || strings.HasPrefix(ln, "event: injected") {
					t.Errorf("injected SSE field line %q; full body: %q", ln, body)
				}
			}
			if tc.wantEvent != "" {
				found := false
				for _, ln := range lines {
					if ln == tc.wantEvent {
						found = true
					}
				}
				if !found {
					t.Errorf("body missing clean event line %q; full body: %q", tc.wantEvent, body)
				}
			}
		})
	}
}
