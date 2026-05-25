package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
)

// recordingProvider captures each outbound Request so tests can
// assert what tool schemas were actually advertised to the model.
type recordingProvider struct {
	requests []*provider.Request
	events   []provider.StreamEvent
}

func (recordingProvider) Name() string { return "recording" }
func (p *recordingProvider) Chat(_ context.Context, r *provider.Request) (<-chan provider.StreamEvent, error) {
	p.requests = append(p.requests, r)
	ch := make(chan provider.StreamEvent, len(p.events))
	for _, ev := range p.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}
func (recordingProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (recordingProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// TestSpawnEventsDoNotLeakToParentBus: sub-agent's intermediate
// events (thinking, tool calls, text deltas) must NOT appear on the
// parent's event bus. Otherwise the user's chat fills up with the
// sub-agent's verbose internal reasoning — exactly the "dumped in
// main chat" complaint about sess_01KSE3JKR…. CC's Task tool hides
// sub-agent internals; only the final summary surfaces via the
// ToolResult on the Agent tool call.
//
// Asserts: subscribe to PARENT bus before Spawn; after Spawn returns,
// the parent bus should have received NO TextDelta or ThinkingDelta
// or ToolCallStarted events from the sub-agent loop.
func TestSpawnEventsDoNotLeakToParentBus(t *testing.T) {
	// Provider emits text + a tool call + more text — would be noisy
	// in the main chat if it leaked.
	prov := &recordingProvider{events: []provider.StreamEvent{
		{Kind: provider.KindThinkingDelta, Thinking: []byte(`"reasoning..."`)},
		{Kind: provider.KindTextDelta, Text: "sub-agent talking"},
		{Kind: provider.KindStop, FinishReason: "stop"},
	}}
	session := ids.NewSessionID()
	parentBus := NewBus(session)
	defer parentBus.Close()
	d := NewDispatcher(parentBus, tool.NewRegistry())
	e := NewEngine(session, parentBus, prov, "m", d)

	// Subscribe to parent bus BEFORE Spawn.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parentSub := parentBus.Subscribe(ctx)
	collected := make(chan []control.EventEnvelope, 1)
	go func() {
		var got []control.EventEnvelope
		for {
			select {
			case env := <-parentSub:
				got = append(got, env)
			case <-time.After(200 * time.Millisecond):
				collected <- got
				return
			}
		}
	}()

	if _, err := e.Spawn(context.Background(), "", "do x"); err != nil {
		t.Fatal(err)
	}
	got := <-collected
	for _, env := range got {
		switch env.Kind {
		case "TextDelta", "ThinkingDelta", "ToolCallStarted", "ToolResult", "TurnStarted", "TurnEnded":
			t.Errorf("sub-agent event %q leaked to parent bus", env.Kind)
		}
	}
}

// TestSpawnPrependsFocusHint: every Spawn must inject a system
// message telling the sub-agent to stay focused (limited tool
// calls, return concise summary). Without it, GLM-5.1 happily
// fires 30+ WebFetches per sub-agent (sess_01KSE3JKR…).
//
// Asserts the FIRST request sent to the provider includes a system
// message containing the focus directive — the parent passes "" as
// systemHint, so the engine must inject one itself.
func TestSpawnPrependsFocusHint(t *testing.T) {
	prov := &recordingProvider{events: []provider.StreamEvent{
		{Kind: provider.KindStop, FinishReason: "stop"},
	}}
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	d := NewDispatcher(bus, tool.NewRegistry())
	e := NewEngine(session, bus, prov, "m", d)

	if _, err := e.Spawn(context.Background(), "", "do x"); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) == 0 {
		t.Fatal("no request captured")
	}
	first := prov.requests[0]
	// The focus hint should arrive on a System-role message that
	// mentions "focused", "concise", or similar.
	var sysContent string
	for _, m := range first.Messages {
		if m.Role == provider.RoleSystem {
			for _, b := range m.Content {
				if b.Type == "text" {
					sysContent += b.Text
				}
			}
		}
	}
	if sysContent == "" {
		t.Fatalf("no system message in sub-agent's first request — focus hint not injected")
	}
	lc := strings.ToLower(sysContent)
	wantOne := []string{"focused", "concise", "sub-agent"}
	hit := false
	for _, w := range wantOne {
		if strings.Contains(lc, w) {
			hit = true
			break
		}
	}
	if !hit {
		t.Errorf("sub-agent system hint missing focus language: %q", sysContent)
	}
}

// TestSpawnCapsInnerIterations: sub-agents must have a TIGHTER
// iteration cap than parent turns. 25 rounds is fine for a top-level
// turn that's coordinating multiple things; a "do one focused thing"
// sub-agent should give up much sooner so a single sub-task can't
// burn the whole budget.
//
// Asserts: sub-agent's Tree is configured with a maxIter < the
// parent constant. We test this by checking that the helper
// SubAgentMaxIterations returns a tighter value.
func TestSubAgentMaxIterationsIsTighter(t *testing.T) {
	if SubAgentMaxIterations >= maxInnerLoopIterations {
		t.Errorf("SubAgentMaxIterations (%d) must be < parent cap (%d)",
			SubAgentMaxIterations, maxInnerLoopIterations)
	}
	if SubAgentMaxIterations < 3 {
		t.Errorf("SubAgentMaxIterations (%d) too small — should leave room for 1 plan + 1-2 tool rounds + summary",
			SubAgentMaxIterations)
	}
}

// TestSpawnEnforcesTighterCap: drive a sub-agent against a provider
// that ALWAYS returns tool_use; the sub-agent must TERMINATE in
// bounded time (proves the iteration cap fires). Since sub-agent
// events are now on a private bus and don't leak to the parent, we
// can't count tool calls — instead we count how many times the
// scripted provider was hit (== inner loop iterations).
func TestSpawnEnforcesTighterCap(t *testing.T) {
	infinite := []provider.StreamEvent{
		{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: "call", Name: "Read"}},
		{Kind: provider.KindToolUseDelta, InputDelta: `{"path":"/etc/hosts"}`},
		{Kind: provider.KindToolUseStop},
		{Kind: provider.KindStop, FinishReason: "tool_use"},
	}
	scripts := make([][]provider.StreamEvent, 50)
	for i := range scripts {
		scripts[i] = append([]provider.StreamEvent(nil), infinite...)
	}
	prov := &scriptedSpawnProvider{scripts: scripts}
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(), readOnlySource{}); err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(bus, reg)
	e := NewEngine(session, bus, prov, "m", d)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		_, _ = e.Spawn(context.Background(), "", "loop")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sub-agent never terminated within 5s — tighter cap not firing")
	}
	elapsed := time.Since(start)
	// Provider hit count == number of inner-loop iterations. Must be
	// at most SubAgentMaxIterations (+1 for the final non-tool round).
	if prov.idx > SubAgentMaxIterations+1 {
		t.Errorf("sub-agent ran %d provider rounds — wanted ≤ %d",
			prov.idx, SubAgentMaxIterations+1)
	}
	t.Logf("sub-agent capped at %d rounds in %v (target cap = %d)",
		prov.idx, elapsed, SubAgentMaxIterations)
}

// readOnlySource exposes a single Read tool for the cap test — we
// just need ANY tool that succeeds so the loop progresses.
type readOnlySource struct{}

func (readOnlySource) Name() string { return "test-read-only" }
func (readOnlySource) Tools(_ context.Context) ([]tool.Tool, error) {
	return []tool.Tool{noopReadTool{}}, nil
}

type noopReadTool struct{}

func (noopReadTool) Name() string                     { return "Read" }
func (noopReadTool) Description() string              { return "test stub" }
func (noopReadTool) Mutating() bool                   { return false }
func (noopReadTool) InputSchema() []byte              { return []byte(`{}`) }
func (noopReadTool) Run(_ context.Context, _ tool.ToolCall, _ tool.EventSink) (*tool.ToolResult, error) {
	return &tool.ToolResult{
		Content: []control.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}

// scriptedSpawnProvider local clone — defined here to avoid coupling
// to the e2e_capabilities_test.go variant.
type scriptedSpawnProvider struct {
	scripts [][]provider.StreamEvent
	idx     int
}

func (*scriptedSpawnProvider) Name() string { return "scripted-spawn" }
func (p *scriptedSpawnProvider) Chat(_ context.Context, _ *provider.Request) (<-chan provider.StreamEvent, error) {
	i := p.idx
	p.idx++
	ch := make(chan provider.StreamEvent, 16)
	go func() {
		defer close(ch)
		if i >= len(p.scripts) {
			ch <- provider.StreamEvent{Kind: provider.KindStop, FinishReason: "stop"}
			return
		}
		for _, ev := range p.scripts[i] {
			ch <- ev
		}
	}()
	return ch, nil
}
func (*scriptedSpawnProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (*scriptedSpawnProvider) TokenCount(_ context.Context, _ string, _ []provider.Message) (int, error) {
	return 0, nil
}

// TestSpawnStripsMetaToolsFromSubAgent: when Engine.Spawn runs a
// sub-agent, the sub-engine's outbound Request must NOT advertise
// TaskList or Agent. Sub-agents should not re-plan the parent's
// task list or recursively spawn further sub-agents — that's the
// cascade that ran sess_01KSDZK5… to 70+ tool calls (16 TaskList
// updates from inside sub-agents + 10 chained Agent spawns).
//
// Failing before the filter is added; passing after.
func TestSpawnStripsMetaToolsFromSubAgent(t *testing.T) {
	prov := &recordingProvider{events: []provider.StreamEvent{
		{Kind: provider.KindTextDelta, Text: "did the thing"},
		{Kind: provider.KindStop, FinishReason: "stop"},
	}}
	session := ids.NewSessionID()
	bus := NewBus(session)
	defer bus.Close()
	d := NewDispatcher(bus, tool.NewRegistry())
	e := NewEngine(session, bus, prov, "m", d)
	// Parent advertises ALL the meta tools — sub-agent should NOT
	// see them in its outbound Request.
	e.Tools = []provider.ToolSchema{
		{Name: "Read"},
		{Name: "Bash"},
		{Name: "TaskList"},
		{Name: "Agent"},
		{Name: "WebFetch"},
	}

	if _, err := e.Spawn(context.Background(), "", "do something"); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) == 0 {
		t.Fatal("sub-provider never received a request")
	}
	sentNames := map[string]bool{}
	for _, ts := range prov.requests[0].Tools {
		sentNames[ts.Name] = true
	}
	for _, banned := range []string{"TaskList", "Agent"} {
		if sentNames[banned] {
			t.Errorf("sub-agent received %q in its tool catalog — cascade not prevented", banned)
		}
	}
	for _, kept := range []string{"Read", "Bash", "WebFetch"} {
		if !sentNames[kept] {
			t.Errorf("sub-agent missing useful tool %q after meta-tool stripping: %v", kept, sentNames)
		}
	}
}
