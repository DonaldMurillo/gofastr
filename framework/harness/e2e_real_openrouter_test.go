//go:build e2e_real

// Real-provider tests for OpenRouter covering the harness features
// added since the original PONG smoke test. Designed to be cheap
// (single short turn each) so the full suite costs cents per run.
//
// Skipped when OPENROUTER_API_KEY isn't set.

package harness

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/openrouter"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/builtins"
)

func openRouterModel(t *testing.T) string {
	if m := os.Getenv("OPENROUTER_MODEL"); m != "" {
		return m
	}
	// Cheap default — adjust if it goes EOL.
	return "anthropic/claude-3.5-haiku"
}

// TestE2EReal_OpenRouter_TaskList: with TaskList registered, model
// should auto-call it when given a multi-step prompt. Mirrors the
// ZAI auto-use test — guards against the openai-adapter / content
// shape bug fired earlier (which was provider-specific).
func TestE2EReal_OpenRouter_TaskList(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	prov := &openrouter.Provider{APIKey: key}
	model := openRouterModel(t)

	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(),
		staticToolSourceFn([]tool.Tool{builtins.TaskList{}})); err != nil {
		t.Fatal(err)
	}
	session := ids.NewSessionID()
	defer builtins.ResetTasks(session)
	bus := engine.NewBus(session)
	defer bus.Close()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, prov, model, d)
	eng.Tools = toolSchemasFromRegistry(reg)
	eng.Middleware = []engine.RequestMiddleware{
		engine.SystemPromptMiddleware(
			"You have a TaskList tool. For multi-step requests call it FIRST with the plan."),
	}

	mux := newRealMux(t, eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sub := c.Subscribe(ctx)
	if err := c.Send(ctx, control.SendInput{
		SessionID: session,
		Content: engine.SimpleInput("Plan three steps to add a Config struct, " +
			"parse it from TOML, and write a test. Use the TaskList tool."),
	}); err != nil {
		t.Fatal(err)
	}

	var sawTaskList bool
	var ended int
	for ended < 1 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out — sawTaskList=%v", sawTaskList)
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				if v.Tool == "TaskList" {
					sawTaskList = true
				}
			case control.TurnEnded:
				ended++
			case control.Error:
				t.Logf("Error event: %s — %s", v.Reason, v.Message)
			}
		}
	}
	if !sawTaskList {
		t.Errorf("OpenRouter model did NOT call TaskList — adapter or system-prompt issue")
	}
	snap, _ := builtins.TaskListSnapshot(session)
	t.Logf("OpenRouter TaskList ok: %d tasks recorded", len(snap))
}

// TestE2EReal_OpenRouter_AssistantOnlyToolCalls: regression for the
// openai-content-omit bug — when an assistant message has ONLY
// tool_calls and no text, the wire format must use null content
// (not empty string). Triggers by asking for a tool-only response.
func TestE2EReal_OpenRouter_AssistantOnlyToolCalls(t *testing.T) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Skip("OPENROUTER_API_KEY not set")
	}
	prov := &openrouter.Provider{APIKey: key}
	model := openRouterModel(t)

	// Read tool so the model can dispatch something testable.
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(),
		staticToolSourceFn([]tool.Tool{builtins.Read{}})); err != nil {
		t.Fatal(err)
	}
	session := ids.NewSessionID()
	bus := engine.NewBus(session)
	defer bus.Close()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, prov, model, d)
	eng.Tools = toolSchemasFromRegistry(reg)

	mux := newRealMux(t, eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sub := c.Subscribe(ctx)
	if err := c.Send(ctx, control.SendInput{
		SessionID: session,
		Content:   engine.SimpleInput("Use the Read tool to read /etc/hosts. Just call the tool, no extra explanation."),
	}); err != nil {
		t.Fatal(err)
	}

	var sawToolCall, sawSecondText, sawEmpty bool
	var ended int
	for ended < 1 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out — toolCall=%v secondText=%v empty=%v",
				sawToolCall, sawSecondText, sawEmpty)
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				if v.Tool == "Read" {
					sawToolCall = true
				}
			case control.TextDelta:
				if sawToolCall && v.Text != "" {
					sawSecondText = true
				}
			case control.Error:
				if v.Reason == "EmptyResponse" {
					sawEmpty = true
				}
			case control.TurnEnded:
				ended++
			}
		}
	}
	if !sawToolCall {
		t.Fatal("OpenRouter model did NOT call Read")
	}
	if sawEmpty {
		t.Errorf("EmptyResponse fired after tool round — adapter sending content:'' again?")
	}
	if !sawSecondText {
		t.Errorf("no follow-up text after tool result — likely the omit-empty-content bug bit OR")
	}
	_ = json.Marshal // keep import
}
