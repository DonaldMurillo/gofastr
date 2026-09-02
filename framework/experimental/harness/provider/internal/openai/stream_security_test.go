package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

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

// countParkedPumps reports how many goroutines are inside
// parseSSEStream right now, by name from their stack. The pump takes
// no context and cannot observe an abandoned consumer, so this is the
// direct observation of the leak the test below asserts about.
func countParkedPumps() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "parseSSEStream")
}

// TestAbortMidStreamReleasesPump pins the abort-cleanup property: when
// the consumer's context is cancelled mid-stream, the adapter must not
// strand its pump goroutine (and, with it, the undrained event channel
// and the un-Closed response body). This is the shape every real abort
// takes: engine CollectStream selects ctx.Done, returns, and never
// reads the channel again. parseSSEStream receives no context, so once
// the 32-slot channel buffer fills it parks on a send forever — one
// leaked goroutine + open body per cancelled turn, for the life of the
// process.
//
// RED today: the pump count never returns to its baseline after
// cancel + settle. The fix shape (NOT applied): pass ctx (or a done
// channel) into parseSSEStream and select on it for every send, plus
// body.Close() on abort.
func TestAbortMidStreamReleasesPump(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// More complete chunks than the channel's 32-slot buffer, so
		// the pump is guaranteed to park on a send once the consumer
		// walks away, and the body never reaches EOF either.
		for range 200 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			flusher.Flush()
		}
		// Hold the response open: a stream that ends would let the
		// pump drain-then-exit on its own and hide the park.
		<-r.Context().Done()
	}))
	defer srv.Close()

	before := countParkedPumps()
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, err := c.Chat(ctx, &provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	// Consume one event the way CollectStream does before an abort,
	// then cancel and STOP READING — exactly the engine's behaviour
	// on ctx.Done: nobody ever drains this channel again.
	<-ch
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for countParkedPumps() > before {
		if time.Now().After(deadline) {
			t.Errorf("SECURITY: [goroutine-leak] %d parseSSEStream pump(s) still parked after the consumer aborted; "+
				"every cancelled turn strands one goroutine, the event channel, and an un-Closed response body for the life of the process",
				countParkedPumps()-before)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestOversizeLineSignalsTerminal extends the truncation property to
// the scanner-boundary surface: a single SSE line larger than the 4 MiB
// scanner buffer must surface a terminal event (KindError via
// bufio.ErrTooLong), never a silent close. A hostile or broken upstream
// that pads one line past the cap otherwise looks like a clean stop.
func TestOversizeLineSignalsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", strings.Repeat("x", 5*1024*1024))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var terminal bool
	for ev := range ch {
		if ev.Kind == provider.KindStop || ev.Kind == provider.KindError {
			terminal = true
		}
	}
	if !terminal {
		t.Error("SECURITY: stream whose single line exceeds the scanner buffer closed with no terminal event; " +
			"an oversize-line failure is indistinguishable from a finished response")
	}
}

// TestHTTPErrorBodyBounded pins the resource bound on the error path:
// a non-200 response body is read through a 64 KiB LimitReader, so a
// hostile upstream cannot OOM the client by returning an endless error
// body. The bound must hold on every non-200 status, not just 4xx.
func TestHTTPErrorBodyBounded(t *testing.T) {
	for _, status := range []int{500, 429, 599} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write(bytes.Repeat([]byte("E"), 1<<20)) // 1 MiB
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
			_, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
			if err == nil {
				t.Fatal("expected error for non-200")
			}
			if n := len(err.Error()); n > 70*1024 {
				t.Errorf("SECURITY: error message carries %d bytes of upstream body; the 64 KiB read bound did not hold", n)
			}
		})
	}
}

// TestAbortVisibleToDrainingConsumer pins the consumer-visible half of
// abort: a consumer that KEEPS draining after cancel (CollectStream's
// select may lose the race to an in-flight event) must still see the
// already-streamed content followed by an explicit terminal event —
// cancellation is a KindError, never a bare channel close. This is the
// contract the leak test above complements from the cleanup side.
func TestAbortVisibleToDrainingConsumer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // hold open until the client aborts
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, err := c.Chat(ctx, &provider.Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	terminal := false
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			switch ev.Kind {
			case provider.KindTextDelta:
				text.WriteString(ev.Text)
				cancel() // abort as soon as real content arrived
			case provider.KindStop, provider.KindError:
				terminal = true
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate after abort")
	}
	if text.String() != "par" {
		t.Errorf("text = %q, want the pre-abort content delivered", text.String())
	}
	if !terminal {
		t.Error("SECURITY: aborted stream closed without a terminal event; cancellation is indistinguishable from a finished response")
	}
}
