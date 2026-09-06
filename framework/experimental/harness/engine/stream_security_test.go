package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Property family: stream-tail integrity. A provider stream must end
// with an explicit terminal event (KindStop or KindError) before its
// summary counts as a complete turn, and a truncated tail must not
// corrupt engine state (History, bus events) downstream.
//
// Reachability (pass-3 recon, HARNESS-STREAM-BARE-CLOSE-SUCCESS): the
// shared internal/openai parseSSEStream only emits KindError on
// scanner errors or malformed chunks. A TCP drop aligned to an SSE
// line boundary (complete `data: ...` line, EOF before [DONE] or any
// finish_reason chunk) exits the loop with scanner.Err() == nil and
// closes the channel with no terminal event; zai and openrouter
// delegate to the same loop. CollectStream (stream.go:67-71) then
// returns (summary, nil) and loop.go:183-184 records reason
// "complete" — a truncated answer is indistinguishable from a
// finished one. ErrStreamClosed (stream.go:126-128) is declared for
// exactly this case and is never returned.

// TestCollectStreamFailsClosedOnBareClose asserts the fail-closed
// side: a channel that closes without KindStop/KindError is a broken
// stream, not a successful one.
//
// RED today: the bare-close cases return (summary, nil).
// Proposed minimal fix (NOT applied — test-only pass): track whether
// a terminal event was seen; on close-without-terminal return
// (summary, ErrStreamClosed), preserving partial text like the
// KindError path already does.
//
// PIN-FLIP NOTE (recorded per pass-3 assignment): the recon claimed
// stream_test.go's normal-close tests pin the weak contract (bare
// close = success) and would need a KindStop added when the engine
// fails closed. Verified empirically — that claim is wrong in this
// tree: TestCollectStreamTextOnly and TestCollectStreamToolUse both
// already send an explicit KindStop before close (stream_test.go:20,
// :70), and applying the fail-closed return to a throwaway copy of
// the tree broke ZERO tests across all of framework/experimental/
// harness/... (failure sets byte-identical to baseline; the 8 visible
// REDs are earlier passes' pins). So no existing pin travels with the
// fix: the weak contract is held up by the absence of a test, and
// this file becomes the strong contract the fix must satisfy.
func TestCollectStreamFailsClosedOnBareClose(t *testing.T) {
	cases := []struct {
		name     string
		events   []provider.StreamEvent
		wantErr  bool   // want an error (errors.Is ErrStreamClosed)
		wantText string // partial text that must survive, if any
	}{
		{
			name: "bare close mid text",
			events: []provider.StreamEvent{
				{Kind: provider.KindTextDelta, Text: "par"},
				{Kind: provider.KindTextDelta, Text: "tial"},
			},
			wantErr:  true,
			wantText: "partial",
		},
		{
			name: "bare close mid tool args",
			events: []provider.StreamEvent{
				{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_x", Name: "Echo"}},
				{Kind: provider.KindToolUseDelta, InputDelta: `{"path":"/et`},
			},
			wantErr: true,
		},
		{
			name: "empty stream, no terminal",
			// Shape the shared openai loop produces for a body that
			// is EOF'd before any data: line.
			events:  nil,
			wantErr: true,
		},
		{
			name: "close after explicit KindStop stays success",
			events: []provider.StreamEvent{
				{Kind: provider.KindTextDelta, Text: "done"},
				{Kind: provider.KindStop, FinishReason: "stop"},
			},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := NewBus(ids.NewSessionID())
			defer bus.Close()
			stream := make(chan provider.StreamEvent, len(tc.events)+1)
			for _, ev := range tc.events {
				stream <- ev
			}
			close(stream)

			summary, err := CollectStream(context.Background(), bus, ids.NewClientID(), stream)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("SECURITY: bare close parsed as success (FinishReason=%q, Text=%q); want ErrStreamClosed — truncated turns are indistinguishable from complete ones", summary.FinishReason, summary.Text)
				}
				if !errors.Is(err, ErrStreamClosed) {
					t.Errorf("err = %v, want errors.Is(err, ErrStreamClosed)", err)
				}
				// Partial output must survive for the error report,
				// mirroring the KindError path (stream.go:112).
				if tc.wantText != "" && summary.Text != tc.wantText {
					t.Errorf("partial text = %q, want %q preserved", summary.Text, tc.wantText)
				}
			} else {
				if err != nil {
					t.Fatalf("explicit terminal event then close must stay success, got %v", err)
				}
			}
		})
	}
}

// TestRunTurnBareCloseNotRecordedComplete is the loop-level companion:
// the same bare close driven through RunTurn must not append the
// partial text to History as a final assistant message nor end the
// turn with reason "complete" (loop.go:176-184 today does both).
//
// RED today: RunTurn returns nil, History gains the assistant
// message, TurnEnded.Reason == "complete".
func TestRunTurnBareCloseNotRecordedComplete(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	d := NewDispatcher(bus, reg)

	// Script has no KindStop: fakeProvider closes the channel after
	// the last event (loop_test.go:33-38) — exactly the boundary-EOF
	// shape the shared openai loop produces on a dropped connection.
	prov := &fakeProvider{scripts: [][]provider.StreamEvent{{
		{Kind: provider.KindTextDelta, Text: "The answer is 4"},
	}}}
	e := NewEngine(session, bus, prov, "fake-model", d)

	ctx := t.Context()
	sub := bus.Subscribe(ctx)
	runErr := e.RunTurn(context.Background(), ids.NewClientID(), SimpleInput("hi"))

	if runErr == nil {
		t.Error("SECURITY: RunTurn returned nil on a stream that closed without a terminal event; truncated output was accepted as the final answer")
	}

	// The partial assistant text must not be recorded as a completed
	// assistant turn.
	if n := len(e.History); n > 0 && e.History[n-1].Role == provider.RoleAssistant {
		t.Errorf("SECURITY: History ends with an assistant message (%d entries) recorded from a truncated stream", n)
	}

	var endReason string
	deadline := time.After(2 * time.Second)
collect:
	for endReason == "" {
		select {
		case env, ok := <-sub:
			if !ok {
				break collect
			}
			if env.Kind == "TurnEnded" {
				if ev, derr := control.DecodeEvent(env); derr == nil {
					if te, ok := ev.(control.TurnEnded); ok {
						endReason = te.Reason
					}
				}
			}
		case <-deadline:
			break collect
		}
	}
	if endReason == "complete" {
		t.Error(`SECURITY: TurnEnded.Reason == "complete" for a truncated stream; want "error"`)
	}
}

// TestTruncatedToolInputKeepsHistoryValid pins the second stream-tail
// property: whatever flushTool stores in ToolUse.Input, nothing
// invalid may cross into History or bus events — both re-marshal the
// json.RawMessage, and encoding/json rejects invalid bytes, so one
// truncated tool-args stream poisons every later provider request
// (loop.go:176-180 appends it to History) and silently drops the
// ToolCallStarted intent row (tool_dispatch.go:50-55 discards the
// Publish error).
//
// CONTRACT-CONFLICT (reported, asserted shape is fix-agnostic): the
// flushTool comment (stream.go:50-51) says "Try to validate the
// accumulated JSON; if invalid, keep the raw bytes so the model can
// be told" — but no validation exists. This test asserts the
// comment's promise; whether the fix validates, sanitizes, or flags
// is the fix author's call, but the marshalability below must hold
// either way.
//
// RED today: both marshals fail on the raw `{"path":"/et` fragment.
func TestTruncatedToolInputKeepsHistoryValid(t *testing.T) {
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()

	// Explicit KindStop isolates this property from the bare-close
	// one: the stream terminated cleanly, only the tool args are
	// truncated.
	stream := make(chan provider.StreamEvent, 4)
	stream <- provider.StreamEvent{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_x", Name: "Echo"}}
	stream <- provider.StreamEvent{Kind: provider.KindToolUseDelta, InputDelta: `{"path":"/et`}
	stream <- provider.StreamEvent{Kind: provider.KindToolUseStop}
	stream <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "tool_calls"}
	close(stream)

	summary, err := CollectStream(context.Background(), bus, ids.NewClientID(), stream)
	if err != nil {
		t.Fatalf("explicit terminal event must stay success: %v", err)
	}
	if len(summary.ToolUses) != 1 {
		t.Fatalf("tool uses = %d, want 1", len(summary.ToolUses))
	}

	// (a) What loop.go:176-180 appends to History must re-marshal.
	content := assistantContentFromSummary(summary)
	if _, merr := json.Marshal(content); merr != nil {
		t.Errorf("SECURITY: assistant content from truncated tool args does not marshal, so the next provider request fails and the session bricks until history is trimmed: %v", merr)
	}

	// (b) What tool_dispatch.go:50-55 publishes as ToolCallStarted
	// must survive envelope encoding — today the marshal error is
	// discarded and the persistence-layer intent row is silently
	// lost.
	_, eerr := control.EncodeEvent(1, control.ToolCallStarted{
		CallID: ids.NewCallID(),
		Tool:   "Echo",
		Args:   summary.ToolUses[0].Input,
	}, session, ids.NewClientID(), time.Now())
	if eerr != nil {
		t.Errorf("SECURITY: ToolCallStarted with truncated args fails EncodeEvent, so the tool-intent row is dropped instead of persisted: %v", eerr)
	}
}

// Pins CollectStream's unbounded per-turn buffering, found by the
// 2026-09-04 red-probe round; fixed in CollectStreamWithCap capping
// text + thinking + tool-argument bytes per turn.
//
// Property: the engine bounds the total bytes it buffers from one
// provider turn (text + thinking + tool-call argument deltas); past
// the cap the turn fails loudly (ErrStreamCapExceeded) instead of
// accumulating without limit, and the partial output already received
// is kept for the transcript.
// Surfaces: engine/stream.go::CollectStream / CollectStreamWithCap
// (textBuf, summary.Thinking, curToolJSON — all three fed by whatever
// the endpoint sends), engine/loop.go::RunTurn via
// Engine.MaxStreamBytesPerTurn (0 = DefaultMaxStreamBytesPerTurn).
// The adapters' own bound (internal/openai parseSSEStream caps one
// SSE line at 4 MiB) never bounded the NUMBER of events; this is the
// total-turn bound, the stream-side twin of
// maxToolResultBytesPerBlock.
func TestCollectStreamCapsTurnBytes(t *testing.T) {
	if DefaultMaxStreamBytesPerTurn != 8<<20 {
		t.Fatalf("DefaultMaxStreamBytesPerTurn = %d, want 8 MiB", DefaultMaxStreamBytesPerTurn)
	}

	// One megabyte per turn keeps the test fast while proving the
	// bound is enforced at a size a hostile endpoint blows through in
	// milliseconds.
	const cap = 1 << 20
	cases := []struct {
		name    string
		payload []provider.StreamEvent // events to stream, in order
	}{
		{
			name: "text deltas",
			payload: func() []provider.StreamEvent {
				chunk := strings.Repeat("a", 8192)
				out := make([]provider.StreamEvent, 0, 300)
				for range 300 { // 300 * 8 KiB ≈ 2.4 MiB > 1 MiB cap
					out = append(out, provider.StreamEvent{Kind: provider.KindTextDelta, Text: chunk})
				}
				return out
			}(),
		},
		{
			name: "thinking deltas",
			payload: func() []provider.StreamEvent {
				block := []byte(strings.Repeat("t", 8192))
				out := make([]provider.StreamEvent, 0, 300)
				for range 300 {
					out = append(out, provider.StreamEvent{Kind: provider.KindThinkingDelta, Thinking: block})
				}
				return out
			}(),
		},
		{
			name: "tool-call argument deltas",
			payload: func() []provider.StreamEvent {
				out := []provider.StreamEvent{{
					Kind:    provider.KindToolUseStart,
					ToolUse: &control.ToolUse{ID: "call_cap", Name: "Echo"},
				}}
				chunk := strings.Repeat("x", 8192)
				for range 300 {
					out = append(out, provider.StreamEvent{Kind: provider.KindToolUseDelta, InputDelta: chunk})
				}
				return out
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := NewBus(ids.NewSessionID())
			defer bus.Close()

			ch := make(chan provider.StreamEvent, 64)
			go func() {
				defer close(ch)
				for _, ev := range tc.payload {
					ch <- ev
				}
				// A well-formed terminal event: the turn "succeeds" from
				// the stream's point of view, the cap must be what stops
				// it.
				ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
			}()

			summary, err := CollectStreamWithCap(context.Background(), bus, ids.NewClientID(), ch, cap)
			// Drain the leftovers so the sender goroutine can exit.
			go func() {
				for range ch {
				}
			}()
			if !errors.Is(err, ErrStreamCapExceeded) {
				t.Fatalf("SECURITY: [stream-unbounded-buffer] %s past the per-turn cap returned err=%v; want ErrStreamCapExceeded — a hostile endpoint can drive the harness process OOM (tool results are capped at 64 KiB/block, the stream needs the same shape of bound)", tc.name, err)
			}

			// The partial output is kept for the transcript, bounded by the cap.
			switch tc.name {
			case "text deltas":
				if len(summary.Text) == 0 || len(summary.Text) > cap {
					t.Fatalf("partial text = %d bytes; want (0, %d]", len(summary.Text), cap)
				}
			case "thinking deltas":
				total := 0
				for _, b := range summary.Thinking {
					total += len(b)
				}
				if total == 0 || total > cap {
					t.Fatalf("partial thinking = %d bytes; want (0, %d]", total, cap)
				}
			case "tool-call argument deltas":
				if len(summary.ToolUses) != 1 || len(summary.ToolUses[0].Input) == 0 {
					t.Fatalf("partial tool use not kept for the transcript: %+v", summary.ToolUses)
				}
			}
		})
	}

	// The default path caps too: CollectStream must apply
	// DefaultMaxStreamBytesPerTurn (8 MiB), not "whatever arrives". A
	// turn of 9 MiB crosses it.
	t.Run("default cap enforced", func(t *testing.T) {
		bus := NewBus(ids.NewSessionID())
		defer bus.Close()
		ch := make(chan provider.StreamEvent, 64)
		go func() {
			defer close(ch)
			chunk := strings.Repeat("a", 64*1024)
			for range 144 { // 9 MiB > 8 MiB default
				ch <- provider.StreamEvent{Kind: provider.KindTextDelta, Text: chunk}
			}
			ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
		}()
		summary, err := CollectStream(context.Background(), bus, ids.NewClientID(), ch)
		go func() {
			for range ch {
			}
		}()
		if !errors.Is(err, ErrStreamCapExceeded) {
			t.Fatalf("SECURITY: [stream-unbounded-buffer] CollectStream buffered a turn past the 8 MiB default (err=%v, text=%d); the default cap must be enforced, not just the explicit one", err, len(summary.Text))
		}
		if len(summary.Text) > DefaultMaxStreamBytesPerTurn {
			t.Fatalf("partial text %d exceeds the cap %d", len(summary.Text), DefaultMaxStreamBytesPerTurn)
		}
	})
}
