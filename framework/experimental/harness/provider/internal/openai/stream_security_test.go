package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// Property: an SSE stream that ends without a terminal marker — no
// `data: [DONE]` line and no finish_reason chunk — is a truncated
// stream and must surface a terminal event (KindError or KindStop)
// before the channel closes. parseSSEStream only emits KindError on
// scanner errors or malformed chunks (client.go:293-295, :366-368);
// a TCP drop aligned to an SSE line boundary (complete `data:` line,
// then clean EOF) exits the scan loop with scanner.Err() == nil and
// the deferred close(ch) fires silently. Downstream, engine
// CollectStream treats the bare close as success and the turn is
// recorded "complete" — pinned from the engine side in
// engine/stream_security_test.go; this file pins the adapter side of
// the same property.
//
// RED today: the boundary-EOF cases close with no terminal event.
// Proposed minimal fix (NOT applied — test-only pass): after the
// scan loop, when scanner.Err() == nil but no [DONE] and no
// finish_reason were seen, emit KindError (e.g. wrapping
// io.ErrUnexpectedEOF) before the deferred close. Verified on a
// throwaway copy of the tree: that fix keeps openai, zai, and
// openrouter (which share this loop) fully green — no existing pin
// travels with it.

// TestStreamBoundaryEOFSignalsTruncation feeds complete SSE lines
// whose stream then ends cleanly (no [DONE]) and asserts the channel
// still terminates explicitly.
func TestStreamBoundaryEOFSignalsTruncation(t *testing.T) {
	textChunk := `{"choices":[{"delta":{"content":"par"}}]}`
	toolChunk := fmt.Sprintf(
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","function":{"name":"Echo","arguments":%s}}]}}]}`,
		strconv.Quote(`{"path":"/et`),
	)
	finishChunk := `{"choices":[{"delta":{},"finish_reason":"stop"}]}`

	sse := func(chunks ...string) string {
		var b strings.Builder
		for _, c := range chunks {
			b.WriteString("data: ")
			b.WriteString(c)
			b.WriteString("\n\n")
		}
		return b.String()
	}

	cases := []struct {
		name        string
		body        string
		wantContent string // content that must arrive before the stream dies
	}{
		{
			// Connection drop after a complete data: line, mid text.
			name:        "boundary EOF mid text",
			body:        sse(textChunk),
			wantContent: "par",
		},
		{
			// Drop while tool arguments are still streaming: worst
			// case downstream, since a half-formed tool_use is
			// flushed into History.
			name: "boundary EOF mid tool args",
			body: sse(toolChunk),
		},
		{
			// Some providers end the stream on a finish_reason chunk
			// without a [DONE] sentinel: that IS a terminal marker.
			name: "finish_reason without DONE is terminal",
			body: sse(finishChunk),
		},
		{
			// Control: [DONE] present stays a clean stop.
			name: "DONE present is terminal",
			body: sse(textChunk) + "data: [DONE]\n\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
			ch, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			var (
				text     strings.Builder
				terminal bool
			)
			for ev := range ch {
				switch ev.Kind {
				case provider.KindTextDelta:
					text.WriteString(ev.Text)
				case provider.KindStop, provider.KindError:
					terminal = true
				}
			}
			if tc.wantContent != "" && text.String() != tc.wantContent {
				t.Fatalf("text = %q, want %q (stream must deliver content before dying)", text.String(), tc.wantContent)
			}
			if !terminal {
				t.Errorf("SECURITY: stream closed with no terminal event (got text %q then bare EOF); truncation is indistinguishable from a finished response", text.String())
			}
		})
	}
}
