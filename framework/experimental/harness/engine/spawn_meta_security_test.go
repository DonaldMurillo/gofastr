package engine

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// A sub-agent's catalog leaves out the meta tools (TaskList, Agent) so
// it cannot re-plan the parent's task list or chain more spawns, but
// the model's output is not bound by the catalog: a tool_use naming a
// tool it was never shown (a prompt-injected page asking for one) went
// to the shared registry and ran. A sub-agent's dispatcher refuses the
// meta tools by name, and still runs the tools it was given.
func TestSubAgentCannotCallHiddenMetaTools(t *testing.T) {
	for _, name := range []string{"TaskList", "Agent"} {
		t.Run(name, func(t *testing.T) {
			session := ids.NewSessionID()
			bus := NewBus(session)
			defer bus.Close()
			reg := tool.NewRegistry()
			meta := make(chan struct{}, 1)
			read := make(chan struct{}, 1)
			if err := reg.Register(context.Background(), staticSource{name: "test", tools: []tool.Tool{
				signalTool{fakeTool: fakeTool{name: name, out: "ok"}, ran: meta},
				signalTool{fakeTool: fakeTool{name: "Read", out: "ok"}, ran: read},
			}}); err != nil {
				t.Fatal(err)
			}
			call := func(id, tool string) []provider.StreamEvent {
				return []provider.StreamEvent{
					{Kind: provider.KindToolUseStart, ToolUse: &control.ToolUse{ID: id, Name: tool}},
					{Kind: provider.KindToolUseDelta, InputDelta: `{}`},
					{Kind: provider.KindToolUseStop},
					{Kind: provider.KindStop, FinishReason: "tool_use"},
				}
			}
			prov := &fakeProvider{scripts: [][]provider.StreamEvent{
				call("call_meta", name),
				call("call_read", "Read"),
				{{Kind: provider.KindTextDelta, Text: "done"}, {Kind: provider.KindStop, FinishReason: "stop"}},
			}}
			e := NewEngine(session, bus, prov, "m", NewDispatcher(bus, reg))
			e.Tools = []provider.ToolSchema{{Name: name}, {Name: "Read"}}
			if _, err := e.Spawn(context.Background(), "", "do x"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-meta:
				t.Fatalf("a sub-agent ran %s, a tool its catalog leaves out", name)
			default:
			}
			select {
			case <-read:
			default:
				t.Fatal("a sub-agent could not run Read, a tool it was given")
			}
		})
	}
}
