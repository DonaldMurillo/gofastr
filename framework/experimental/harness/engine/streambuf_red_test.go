//go:build red

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// CONTRACT-QUESTION red: the maintainer must pick the per-turn stream cap (and its
// size). Tool results are already hard-capped at 64 KiB per block
// (maxToolResultBytesPerBlock) precisely so "a single tool result can silently
// exceed the model's context window", but the provider stream feeding the SAME
// conversation buffers with no cap at all. Is an unbounded single-turn provider
// stream intended, or should CollectStream fail the turn loudly past a cap?
//
// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F1 Resource exhaustion from request-borne input
// Property: the engine must bound the total bytes it buffers from one provider turn
//           (text + thinking + tool-call argument deltas); past the cap the turn
//           fails loudly instead of accumulating without limit.
// Surfaces: framework/experimental/harness/engine/stream.go::CollectStream
//           (textBuf, summary.Thinking, curToolJSON — all unbounded),
//           framework/experimental/harness/provider/internal/openai/client.go::
//           parseSSEStream (caps one SSE line at 4 MiB but not the number of
//           events, so the 5-minute client timeout is the only bound)
// Finding: CollectStream buffers every TextDelta/ThinkingDelta/InputDelta a
//          provider emits. A hostile, compromised, or buggy endpoint (the BaseURL
//          is profile-configurable, so any HTTPS endpoint the operator pointed at
//          or anything that hijacks it) can stream multi-gigabyte turns for the
//          full 5-minute HTTP timeout: memory grows without bound in the harness
//          process and the turn's text is persisted into session history.
// Severity: medium — needs a hostile/misbehaving provider endpoint, but the
//           failure is process-wide OOM, and the asymmetry with the 64 KiB
//           tool-result cap shows the bound was intended to exist.
// Fix direction: cap accumulated text/thinking/args per turn (proposal: 8 MiB
//                total), emitting a terminal error event and failing the turn
//                when exceeded, mirroring maxToolResultBytesPerBlock.

// proposedStreamCapBytes is the proposed bound this probe asserts. The number is
// the maintainer's call; the property under test is that SOME bound well below
// "whatever the provider sends" exists.
const proposedStreamCapBytes = 8 << 20

func TestCollectStreamCapsTurnBytes(t *testing.T) {
	bus := NewBus(ids.NewSessionID())
	defer bus.Close()

	ch := make(chan provider.StreamEvent, 64)
	go func() {
		defer close(ch)
		// 2048 × 8 KiB = 16 MiB of text: well past the proposed cap,
		// far below anything a memory limit would catch.
		chunk := strings.Repeat("a", 8192)
		for range 2048 {
			ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: chunk}
		}
		// A well-formed terminal event: the turn "succeeds" from the
		// stream's point of view, the cap must be what stops it.
		ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
	}()

	summary, err := CollectStream(context.Background(), bus, ids.NewClientID(), ch)
	if err == nil && len(summary.Text) > proposedStreamCapBytes {
		t.Fatalf("SECURITY: [stream-unbounded-buffer] CollectStream buffered %d bytes of one provider turn with no cap and returned success; a hostile endpoint can drive the harness process OOM. Tool results are capped at 64 KiB/block — the stream needs the same shape of bound.", len(summary.Text))
	}
}
