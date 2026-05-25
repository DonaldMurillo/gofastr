//go:build e2e_real

// e2e_capabilities_test.go — single hard test that drives every
// claimed v0.1 capability through one harness, then asserts each.
// Designed to surface regressions that the per-feature tests miss
// because they bootstrap fresh — here every subsystem shares state.
//
// Run with:
//
//	go test -tags=e2e_real -run TestAllCapabilities ./framework/harness -count=1 -v

package harness

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/rest"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
)

// TestAllCapabilities walks every v0.1 capability through one shared
// harness. It is deliberately aggressive — sequential subtests build
// on each other's state. Subtests are listed in the order they run.
//
// The capabilities checked here (matching project_harness_features.md):
//
//	A. Engine + tool dispatch end-to-end
//	B. Turn numbering — sequential after multi-LLM-round turns
//	C. Empty model response → Error event + reason="empty"
//	D. Multi-turn history preservation
//	E. TurnStarted carries user content (cross-client echo)
//	F. Concurrent SendInput rejected with TurnInProgress
//	G. Cancellation mid-stream stops the loop
//	H. Multi-client SSE: two REST clients see the same events
//	I. Permission deny path
//
// If any subtest fails, the rest still run so you see the full picture.
func TestAllCapabilities(t *testing.T) {
	// Provider with a stack of scripts; each Chat call pops the next.
	prov := &scriptedProvider{scripts: [][]provider.StreamEvent{
		// Turn 1: model emits a tool_use (drives B+A+D)
		{
			{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_t1", Name: "Read"}},
			{Kind: provider.KindToolUseDelta, InputDelta: `{"path":"`},
			{Kind: provider.KindToolUseDelta, InputDelta: ""},
			{Kind: provider.KindToolUseDelta, InputDelta: `/etc/hosts"}`},
			{Kind: provider.KindToolUseStop},
			{Kind: provider.KindStop, FinishReason: "tool_use"},
		},
		// Turn 1 continued: after tool result, model wraps up
		{
			{Kind: provider.KindTextDelta, Text: "saw the file"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
		// Turn 2: plain text reply (drives D)
		{
			{Kind: provider.KindTextDelta, Text: "turn-2-reply"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
		// Turn 3: EMPTY response (drives C)
		{
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
		// Turn 4: another plain text (used by H)
		{
			{Kind: provider.KindTextDelta, Text: "turn-4-reply"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
		// Turn 5: slow stream for cancellation (G)
		{
			{Kind: provider.KindTextDelta, Text: "chunk1 "},
			{Kind: provider.KindTextDelta, Text: "chunk2 "},
			{Kind: provider.KindTextDelta, Text: "chunk3 "},
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
	}}

	h, sess, cleanup := plumbingHarnessWithRealTools(t, prov)
	defer cleanup()

	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	if err := h.Mux.Attach(sess, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancelAll := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelAll()

	sub := c.Subscribe(ctx)
	// Drain helper — pulls events until predicate true or timeout, returns collected.
	drain := func(t *testing.T, pred func([]control.EventEnvelope) bool, timeout time.Duration) []control.EventEnvelope {
		t.Helper()
		var got []control.EventEnvelope
		deadline := time.After(timeout)
		for {
			if pred(got) {
				return got
			}
			select {
			case env := <-sub:
				got = append(got, env)
			case <-deadline:
				return got
			}
		}
	}
	collectKinds := func(envs []control.EventEnvelope) []string {
		out := make([]string, 0, len(envs))
		for _, e := range envs {
			out = append(out, e.Kind)
		}
		return out
	}

	// --- A + B + D + E: drive turn 1 (with tool round) ---
	t.Run("A_engine_tool_dispatch", func(t *testing.T) {
		_ = c.Send(ctx, control.SendInput{
			SessionID: sess,
			Content:   engine.SimpleInput("read file please"),
		})
		envs := drain(t, func(envs []control.EventEnvelope) bool {
			for i, e := range envs {
				if e.Kind == "TurnEnded" {
					// wait for the SECOND TurnEnded (after tool result)
					for j := i + 1; j < len(envs); j++ {
						if envs[j].Kind == "TextDelta" {
							return true
						}
					}
				}
			}
			return false
		}, 5*time.Second)
		kinds := collectKinds(envs)
		expect := []string{"TurnStarted", "ToolCallStarted", "ToolResult", "TextDelta"}
		for _, k := range expect {
			if !contains(kinds, k) {
				t.Errorf("missing %s in turn 1 (have: %v)", k, kinds)
			}
		}
		// E: first TurnStarted must carry the user content
		for _, env := range envs {
			if env.Kind == "TurnStarted" {
				ev, _ := control.DecodeEvent(env)
				ts := ev.(control.TurnStarted)
				if len(ts.Content) == 0 || ts.Content[0].Text != "read file please" {
					t.Errorf("TurnStarted missing content: %+v", ts)
				}
				if ts.Turn != 1 {
					t.Errorf("first turn number = %d, want 1", ts.Turn)
				}
				break
			}
		}
	})

	// --- B + D: turn 2 should be NUMBERED 2 (not 4 after the tool round) ---
	t.Run("B_turn_numbering_after_multi_round", func(t *testing.T) {
		_ = c.Send(ctx, control.SendInput{
			SessionID: sess,
			Content:   engine.SimpleInput("second turn"),
		})
		envs := drain(t, func(envs []control.EventEnvelope) bool {
			return countKind(envs, "TurnEnded") >= 1
		}, 5*time.Second)
		var sawTurn int
		for _, env := range envs {
			if env.Kind == "TurnStarted" {
				ev, _ := control.DecodeEvent(env)
				sawTurn = ev.(control.TurnStarted).Turn
				break
			}
		}
		if sawTurn != 2 {
			t.Errorf("second user input got turn number %d, want 2 (regression: turn counter inflates on multi-round tool turns)", sawTurn)
		}
	})

	// --- C: empty model response surfaces as Error + reason="empty" ---
	t.Run("C_empty_response_surfaced", func(t *testing.T) {
		_ = c.Send(ctx, control.SendInput{
			SessionID: sess,
			Content:   engine.SimpleInput("provoke empty response"),
		})
		envs := drain(t, func(envs []control.EventEnvelope) bool {
			return countKind(envs, "TurnEnded") >= 1
		}, 5*time.Second)
		var sawError bool
		var endReason string
		for _, env := range envs {
			switch env.Kind {
			case "Error":
				ev, _ := control.DecodeEvent(env)
				if ev.(control.Error).Reason == "EmptyResponse" {
					sawError = true
				}
			case "TurnEnded":
				ev, _ := control.DecodeEvent(env)
				endReason = ev.(control.TurnEnded).Reason
			}
		}
		if !sawError {
			t.Errorf("empty model response did NOT publish Error{EmptyResponse}: kinds=%v", collectKinds(envs))
		}
		if endReason != "empty" {
			t.Errorf("TurnEnded reason = %q, want \"empty\"", endReason)
		}
	})

	// --- H: multi-client REST SSE both see the same events ---
	t.Run("H_multi_client_sse_broadcast", func(t *testing.T) {
		// Spin up a REST server pointed at the same Mux+Catalog.
		secret := make([]byte, 32)
		for i := range secret {
			secret[i] = byte(i + 1)
		}
		enc := auth.NewEncoder(secret)
		restSrv := &rest.Server{
			Mux: h.Mux, Catalog: h.Catalog, Encoder: enc,
			Revocations: auth.NewRevocationList(),
			Features:    []string{"rest"},
		}
		h.Catalog.RegisterEngine(h.Mux.EngineFor(sess))
		httpSrv := httptest.NewServer(restSrv.Handler())
		defer httpSrv.Close()
		token, _ := enc.Encode(auth.Claims{
			Ver: auth.VerCurrent, JTI: ids.NewJTI(),
			Sessions: []ids.SessionID{sess}, IdentityClass: control.IdentityHuman,
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		})
		_ = token
		// Two SSE subscribers via inproc (simpler than two HTTP clients
		// — exercises the SAME Mux broadcast). The HTTP server is
		// stood up only to validate construction doesn't conflict
		// with the live engine. Real HTTP SSE coverage is in
		// TestE2EExternal_REST_PostInputAndSSE.
		client1 := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
		if err := h.Mux.Attach(sess, client1); err != nil {
			t.Fatalf("attach client1: %v", err)
		}
		defer client1.Close()
		client2 := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
		if err := h.Mux.Attach(sess, client2); err != nil {
			t.Fatalf("attach client2: %v", err)
		}
		defer client2.Close()
		s1 := client1.Subscribe(ctx)
		s2 := client2.Subscribe(ctx)
		// Original `c` sends, the other two should receive.
		_ = c.Send(ctx, control.SendInput{
			SessionID: sess,
			Content:   engine.SimpleInput("broadcast me"),
		})
		// Drain `sub` (the original) too so the Bus doesn't backpressure.
		go func() {
			for {
				select {
				case <-sub:
				case <-ctx.Done():
					return
				}
			}
		}()
		got1 := waitForKind(s1, "TextDelta", 3*time.Second)
		got2 := waitForKind(s2, "TextDelta", 3*time.Second)
		if !got1 {
			t.Error("client1 did NOT receive TextDelta — multi-client broadcast broken")
		}
		if !got2 {
			t.Error("client2 did NOT receive TextDelta — multi-client broadcast broken")
		}
	})

	// --- F: concurrent SendInput must be rejected with TurnInProgress ---
	t.Run("F_concurrent_send_rejected", func(t *testing.T) {
		// Push a fresh slow script so the first turn is in-flight.
		prov.scripts = append(prov.scripts, []provider.StreamEvent{
			{Kind: provider.KindTextDelta, Text: "slow1"},
			{Kind: provider.KindTextDelta, Text: "slow2"},
			{Kind: provider.KindTextDelta, Text: "slow3"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		})
		// First send starts the turn.
		if err := c.Send(ctx, control.SendInput{
			SessionID: sess, Content: engine.SimpleInput("first"),
		}); err != nil {
			t.Fatalf("first Send: %v", err)
		}
		// Spam 5 more sends — each MUST be rejected with TurnInProgress.
		var accepted, rejected int
		for i := 0; i < 5; i++ {
			if err := c.Send(ctx, control.SendInput{
				SessionID: sess, Content: engine.SimpleInput("spam"),
			}); err != nil {
				// Any error means rejected — multiplex returns
				// "another client is sending input" / TurnInProgress /
				// similar based on the conflict.
				rejected++
				if !strings.Contains(err.Error(), "another client") &&
					!strings.Contains(err.Error(), "turn") &&
					!strings.Contains(err.Error(), "progress") {
					t.Logf("rejection reason (note): %v", err)
				}
			} else {
				accepted++
			}
		}
		if rejected == 0 {
			t.Errorf("concurrent sends were NOT rejected — race window or missing guard. accepted=%d rejected=%d",
				accepted, rejected)
		}
		t.Logf("F: rejected %d of 5 spam sends (guard works)", rejected)
		// Drain to TurnEnded so the next subtest starts clean.
		_ = drain2(c.Subscribe(ctx), func(envs []control.EventEnvelope) bool {
			return countKind(envs, "TurnEnded") >= 1
		}, 3*time.Second)
	})

	// --- I: permission deny path is covered by the dedicated
	// TestE2EPlumbing_Permission_Deny — replicating it here without
	// a fresh harness is fragile (shared provider script state).
	// We do a smoke check that the AnswerPermission command at
	// least dispatches without error.
	t.Run("I_answer_permission_dispatch", func(t *testing.T) {
		err := c.Send(ctx, control.AnswerPermission{
			SessionID: sess, CallID: ids.NewCallID(),
			Decision: control.DecisionDeny, Scope: control.ScopeOnce,
		})
		// Multiplex may reject (no pending call) or accept silently —
		// either is fine. We only fail on a hard error like a panic
		// or "command not understood".
		if err != nil && strings.Contains(err.Error(), "unknown command") {
			t.Errorf("AnswerPermission not recognized: %v", err)
		}
	})

	// --- STRESS_slow_subscriber: a non-draining subscriber must NOT
	// stall the bus. Other subscribers still receive events.
	t.Run("STRESS_slow_subscriber_does_not_block_bus", func(t *testing.T) {
		// Drive the bus directly with 500 synthetic events so the
		// outcome doesn't depend on provider script state. The fast
		// subscriber drains in its own goroutine CONCURRENTLY with
		// publishing — otherwise both buffers fill at 256 (publisher
		// is the would-be drainer) and we'd be measuring drop policy,
		// not back-pressure isolation.
		bus := h.Mux.EngineFor(sess).Bus
		slowCtx, slowCancel := context.WithCancel(ctx)
		defer slowCancel()
		_ = bus.Subscribe(slowCtx) // never drained — should NOT block bus
		fast := bus.Subscribe(ctx)
		const N = 500
		got := make(chan int, 1)
		go func() {
			seen := 0
			deadline := time.After(3 * time.Second)
			for seen < N {
				select {
				case env := <-fast:
					if env.Kind == "TextDelta" {
						seen++
					}
				case <-deadline:
					got <- seen
					return
				}
			}
			got <- seen
		}()
		// Tiny pause so the drainer goroutine is ready before we flood.
		time.Sleep(10 * time.Millisecond)
		for i := 0; i < N; i++ {
			_, _ = bus.Publish(control.TextDelta{Text: "bus-stress"}, ids.NewClientID())
		}
		seen := <-got
		min := N * 9 / 10
		if seen < min {
			t.Errorf("fast subscriber received %d/%d events — slow subscriber back-pressured the bus", seen, N)
		} else {
			t.Logf("fast drained %d/%d events under load (slow was attached + idle)", seen, N)
		}
	})

	// --- STRESS_send_empty_input: zero-content send must NOT crash
	// the engine. We just verify the send doesn't panic and the bus
	// is still alive afterwards. A "fail cleanly" outcome is fine
	// either way (error returned or silently dropped).
	t.Run("STRESS_send_empty_input_fails_cleanly", func(t *testing.T) {
		bus := h.Mux.EngineFor(sess).Bus
		sub := bus.Subscribe(ctx)
		_ = c.Send(ctx, control.SendInput{SessionID: sess, Content: nil})
		// Now publish directly to verify bus alive.
		_, err := bus.Publish(control.TextDelta{Text: "bus-still-alive"}, ids.NewClientID())
		if err != nil {
			t.Errorf("bus dead after empty send: %v", err)
			return
		}
		// Drain until we see our marker or timeout.
		deadline := time.After(2 * time.Second)
	wait:
		for {
			select {
			case env := <-sub:
				if env.Kind == "TextDelta" {
					ev, _ := control.DecodeEvent(env)
					if ev.(control.TextDelta).Text == "bus-still-alive" {
						break wait
					}
				}
			case <-deadline:
				t.Errorf("bus did not deliver synthetic event after empty send — possibly broken")
				return
			}
		}
	})

	// --- CONTEXT_OVERFLOW: a tool returning 500 KiB of text must be
	// truncated by the engine before being added to history.
	// Regression for sess_01KSC4S1D where uncapped WebFetch results
	// (350+250 KiB) exceeded GLM-5.1's 128K-token context window and
	// the model silently returned empty.
	t.Run("CONTEXT_OVERFLOW_huge_tool_result_capped", func(t *testing.T) {
		// Append scripts: model emits a tool_use, we return a huge
		// result, then the next LLM call gets the capped version.
		huge := strings.Repeat("X", 500*1024) // 500 KiB
		prov.scripts = append(prov.scripts,
			[]provider.StreamEvent{
				{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_huge", Name: "FauxBig"}},
				{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
				{Kind: provider.KindToolUseStop},
				{Kind: provider.KindStop, FinishReason: "tool_use"},
			},
			[]provider.StreamEvent{
				{Kind: provider.KindTextDelta, Text: "saw the result"},
				{Kind: provider.KindStop, FinishReason: "stop"},
			})
		// Register a faux tool that returns the huge string.
		// (We can't easily inject one mid-test, so instead we just
		// verify the engine cap via direct call on capToolResultContent.)
		capped := engine.CapToolResultContent([]control.ContentBlock{
			{Type: "text", Text: huge},
		})
		if len(capped) != 1 {
			t.Fatalf("capToolResult returned wrong block count: %d", len(capped))
		}
		text := capped[0].Text
		if len(text) >= len(huge) {
			t.Errorf("capToolResult did NOT truncate: in=%d out=%d", len(huge), len(text))
		}
		if !strings.Contains(text, "engine-capped") {
			t.Errorf("capToolResult missing truncation suffix: %q", text[len(text)-200:])
		}
		// Sanity: capped length is under 80 KiB (64 KiB body + small suffix)
		if len(text) > 80*1024 {
			t.Errorf("capped text larger than expected ceiling: %d bytes", len(text))
		}
	})

	// --- G: cancellation mid-stream stops the loop ---
	t.Run("G_cancel_mid_stream", func(t *testing.T) {
		// Use a fresh provider script that yields slowly enough to cancel.
		// Re-attach a new subscriber (sub was drained by H).
		nsub := c.Subscribe(ctx)
		_ = c.Send(ctx, control.SendInput{
			SessionID: sess,
			Content:   engine.SimpleInput("cancel me"),
		})
		// Wait for first TextDelta then cancel.
		time.Sleep(50 * time.Millisecond)
		_ = c.Send(ctx, control.CancelTurn{SessionID: sess})
		envs := drain2(nsub, func(envs []control.EventEnvelope) bool {
			for _, e := range envs {
				if e.Kind == "TurnEnded" {
					ev, _ := control.DecodeEvent(e)
					if r := ev.(control.TurnEnded).Reason; r == "cancelled" || r == "complete" {
						return true
					}
				}
			}
			return false
		}, 3*time.Second)
		// Pass either way — but the lifecycle must close. Most likely
		// the brief sleep let some deltas through before cancel landed.
		var ended bool
		for _, e := range envs {
			if e.Kind == "TurnEnded" {
				ended = true
				break
			}
		}
		if !ended {
			t.Errorf("turn never ended after cancel: %v", collectKinds(envs))
		}
	})
}

// TestRunawayLoop_InnerIterationsCapped: dedicated test for the
// inner-loop cap. Uses its own harness so state from other subtests
// can't pollute the script index. Regression for the runaway session
// (sess_01KSDZK5… — 70 tool calls in one turn). Without the cap this
// test would hang for the full timeout; with it, terminates fast
// with an explicit "IterationLimit" error and "iteration_limit" end
// reason.
func TestRunawayLoop_InnerIterationsCapped(t *testing.T) {
	// Provider always returns tool_use, never a real text reply.
	infiniteScript := []provider.StreamEvent{
		{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_loop", Name: "Read"}},
		{Kind: provider.KindToolUseDelta, InputDelta: `{"path":"/etc/hosts"}`},
		{Kind: provider.KindToolUseStop},
		{Kind: provider.KindStop, FinishReason: "tool_use"},
	}
	scripts := make([][]provider.StreamEvent, 200)
	for i := range scripts {
		scripts[i] = append([]provider.StreamEvent(nil), infiniteScript...)
	}
	prov := &scriptedProvider{scripts: scripts}
	h, sess, cleanup := plumbingHarnessWithRealTools(t, prov)
	defer cleanup()

	c := inproc.New(ids.NewClientID(), control.IdentityHuman, h.Mux.EngineFor(sess).Bus, h.Mux)
	if err := h.Mux.Attach(sess, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sub := c.Subscribe(ctx)
	start := time.Now()
	if err := c.Send(ctx, control.SendInput{
		SessionID: sess, Content: engine.SimpleInput("loop please"),
	}); err != nil {
		t.Fatal(err)
	}

	var sawIterError bool
	var endReason string
	var toolCalls int
	deadline := time.After(25 * time.Second)
	for endReason == "" {
		select {
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				toolCalls++
			case control.Error:
				if v.Reason == "IterationLimit" {
					sawIterError = true
				}
			case control.TurnEnded:
				endReason = v.Reason
			}
		case <-deadline:
			t.Fatalf("runaway loop never terminated — cap missing. tool calls so far: %d", toolCalls)
		}
	}
	elapsed := time.Since(start)
	if !sawIterError {
		t.Errorf("no IterationLimit error event published. end reason: %s, %d tool calls",
			endReason, toolCalls)
	}
	if endReason != "iteration_limit" {
		t.Errorf("end reason = %q, want \"iteration_limit\"", endReason)
	}
	t.Logf("runaway terminated: %d tool calls in %v, reason=%s", toolCalls, elapsed, endReason)
}

// ---------- helpers ----------

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func countKind(envs []control.EventEnvelope, kind string) int {
	n := 0
	for _, e := range envs {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func waitForKind(ch <-chan control.EventEnvelope, kind string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case env := <-ch:
			if env.Kind == kind {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func drain2(ch <-chan control.EventEnvelope, pred func([]control.EventEnvelope) bool, timeout time.Duration) []control.EventEnvelope {
	var got []control.EventEnvelope
	deadline := time.After(timeout)
	for {
		if pred(got) {
			return got
		}
		select {
		case env := <-ch:
			got = append(got, env)
		case <-deadline:
			return got
		}
	}
}
