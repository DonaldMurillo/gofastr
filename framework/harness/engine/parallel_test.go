package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// slowTool simulates a tool that takes a measurable amount of work.
// Tracks concurrent in-flight count so the test can prove parallel
// execution happened (not just "fast sequential").
type slowTool struct {
	delay       time.Duration
	maxInFlight *int64 // pointer so multiple instances share the high-water mark
	inFlight    *int64
}

func (slowTool) Name() string        { return "Slow" }
func (slowTool) Description() string { return "test tool that sleeps" }
func (slowTool) Mutating() bool      { return false }
func (slowTool) InputSchema() []byte { return []byte(`{}`) }
func (s slowTool) Run(_ context.Context, _ tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	cur := atomic.AddInt64(s.inFlight, 1)
	defer atomic.AddInt64(s.inFlight, -1)
	for {
		max := atomic.LoadInt64(s.maxInFlight)
		if cur <= max || atomic.CompareAndSwapInt64(s.maxInFlight, max, cur) {
			break
		}
	}
	time.Sleep(s.delay)
	return &tool.ToolResult{Content: []control.ContentBlock{{Type: "text", Text: "done"}}}, nil
}

type slowToolSource struct{ tool slowTool }

func (s slowToolSource) Name() string { return "slow-src" }
func (s slowToolSource) Tools(_ context.Context) ([]tool.Tool, error) {
	return []tool.Tool{s.tool}, nil
}

// TestDispatchRunsToolUsesConcurrently: when the model returns N
// tool_uses in a SINGLE response, the engine should dispatch them
// concurrently. Total wall-clock time should be ≈ delay (parallel),
// not N×delay (sequential).
//
// Pre-fix: sequential `for _, tu := range summary.ToolUses` → 3×500ms = 1.5s
// Post-fix: parallel goroutines → ~500ms
func TestDispatchRunsToolUsesConcurrently(t *testing.T) {
	var maxInFlight, inFlight int64
	st := slowTool{
		delay:       400 * time.Millisecond,
		maxInFlight: &maxInFlight,
		inFlight:    &inFlight,
	}
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(), slowToolSource{tool: st}); err != nil {
		t.Fatal(err)
	}

	// Provider returns ONE response with 3 tool_use blocks → engine
	// should dispatch them all in parallel.
	scripts := [][]provider.StreamEvent{
		{
			{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_a", Name: "Slow"}},
			{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
			{Kind: provider.KindToolUseStop},
			{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_b", Name: "Slow"}},
			{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
			{Kind: provider.KindToolUseStop},
			{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call_c", Name: "Slow"}},
			{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
			{Kind: provider.KindToolUseStop},
			{Kind: provider.KindStop, FinishReason: "tool_use"},
		},
		// Second round: model says "done" with text.
		{
			{Kind: provider.KindTextDelta, Text: "all done"},
			{Kind: provider.KindStop, FinishReason: "stop"},
		},
	}
	prov := &fakeProvider{scripts: scripts}

	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	d := NewDispatcher(bus, reg)
	e := NewEngine(session, bus, prov, "m", d)

	start := time.Now()
	if err := e.RunTurn(context.Background(), ids.NewClientID(), []control.ContentBlock{
		{Type: "text", Text: "do them"},
	}); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	// PROOF of parallel dispatch: at some point we had > 1 tool
	// in-flight simultaneously.
	if atomic.LoadInt64(&maxInFlight) < 2 {
		t.Errorf("max concurrent tools = %d, want ≥ 2 — dispatcher is serial",
			atomic.LoadInt64(&maxInFlight))
	}
	// Total elapsed should be much less than 3×400ms = 1.2s.
	// Sequential would be ~1.2s+; parallel ~0.4s+.
	if elapsed > 900*time.Millisecond {
		t.Errorf("turn took %v — sequential-shaped (want < 900ms)", elapsed)
	}
	t.Logf("parallel ok: %d max in-flight, %v elapsed", maxInFlight, elapsed)
}
