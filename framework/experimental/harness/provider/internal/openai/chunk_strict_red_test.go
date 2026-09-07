//go:build red

package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// RED TEST — open finding, 2026-09-07 adversarial round 5, phase 2 (family
// enumeration; tier T3). Tests-only; no fix applied.
//
// CONTRACT-QUESTION (answered before the property): the SSE chunk arrives
// from the CONFIGURED provider — the trust anchor and the single consumer of
// this decode — not from an unauthenticated client, so leniency here is a
// defensible posture. The counter-precedent is in-repo: the harness already
// strict-decodes a trusted peer's RESPONSE wherever it executes one
// (core/acp/server.go:221 — session/request_permission decodes the agent's
// outcome via handler.UnmarshalStrict), which makes refusing ambiguity
// before text and tool deltas execute a defensible promotion, not a new
// doctrine. Maintainer call: promote (keep this test, fix as below) or keep
// lenient (delete this test and record the decision).
//
// Property: provider-response JSON refuses ambiguity before its text and
// tool deltas execute — a chunk carrying duplicate or case-folded keys must
// surface a terminal error event, never emit the smuggled delta, because
// stdlib json resolves such keys last-wins and the engine would record text
// or a tool call any first-read intermediary (proxy, gateway, trace) parsed
// differently.
//
// Surfaces: client.go::parseSSEStream chunk decode :334-335 — plain
// json.Unmarshal of each data: payload into streamChunk (the folded
// "Choices" spelling still matches the `choices` tag); the same lenient
// decode serves zai and openrouter (openrouter.go:60-71 delegates Chat to
// this client; openrouter.go:130 decodes the /models catalog the same way).
//
// Finding (verified today, legs below): the folded chunk
// {"choices":[],"Choices":[{"delta":{"content":"evil"}}]} emits a
// TextDelta "evil" — Go's tag-insensitive match folds "Choices" onto
// `choices` and the later occurrence wins; the exact-duplicate spelling
// emits its smuggled delta the same way, then the stream finishes clean.
//
// Fix direction: decode each chunk through the strict-key walk
// (handler.UnmarshalStrict / CheckObjectKeys) and surface the failure as
// the existing KindError terminal path ("parse chunk: …") — the truncation
// pins in stream_security_test.go already require a terminal event on a
// dead stream, so an ambiguous chunk should terminate it identically.

// TestStreamChunkRedRefusesFoldedKeys: a stub provider that smuggles its
// delta behind a folded (and a duplicated) key must fail the stream with an
// error event rather than emit the delta. The clean guard proves ordinary
// chunks still stream, so the refusal demanded below can only come from key
// strictness.
func TestStreamChunkRedRefusesFoldedKeys(t *testing.T) {
	sse := func(chunks ...string) string {
		var b strings.Builder
		for _, c := range chunks {
			b.WriteString("data: ")
			b.WriteString(c)
			b.WriteString("\n\n")
		}
		return b.String()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse(
			`{"choices":[],"Choices":[{"delta":{"content":"evil-fold"}}]}`,
			`{"choices":[],"choices":[{"delta":{"content":"evil-dup"}}]}`,
		)+"data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "sk-test", Name: "test"}
	ch, err := c.Chat(context.Background(), &provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("setup broken: chat: %v", err)
	}
	var text strings.Builder
	sawError := false
	for ev := range ch {
		switch ev.Kind {
		case provider.KindTextDelta:
			text.WriteString(ev.Text)
		case provider.KindError:
			sawError = true
		}
	}
	if strings.Contains(text.String(), "evil") || !sawError {
		t.Errorf("SECURITY: [provider-chunk-lenient] the stream emitted smuggled delta text %q from ambiguous chunks (error event seen: %v): stdlib json folded \"Choices\" onto the choices tag and kept the last duplicate, so the engine records text any first-read intermediary parsed differently — strict-decode each chunk and terminate the stream through the KindError path, the way core/acp already strict-decodes a trusted peer's response (:221)", text.String(), sawError)
	}

	// GREEN-guard: clean chunks stream their text and terminate.
	clean := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse(
			`{"choices":[{"delta":{"content":"par"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		))
	}))
	defer clean.Close()
	c2 := &Client{BaseURL: clean.URL, APIKey: "sk-test", Name: "test"}
	ch2, err := c2.Chat(context.Background(), &provider.Request{Model: "m"})
	if err != nil {
		t.Fatalf("setup broken: clean chat: %v", err)
	}
	var got strings.Builder
	terminal := false
	for ev := range ch2 {
		switch ev.Kind {
		case provider.KindTextDelta:
			got.WriteString(ev.Text)
		case provider.KindStop, provider.KindError:
			terminal = true
		}
	}
	if got.String() != "par" || !terminal {
		t.Fatalf("happy path: clean chunk must stream (text=%q terminal=%v)", got.String(), terminal)
	}
}
