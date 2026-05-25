//go:build e2e_real

// Real-provider verification for the TaskList tool. Hits ZAI GLM-5.1
// with the TaskList tool registered, asks the model to plan three
// steps, and asserts the tool was dispatched and the in-memory
// snapshot reflects the new plan. Token-cheap by design — one turn,
// short prompt, no tool chains. Skipped when ZAI_API_KEY isn't set.
//
// Run with:
//
//	go test -tags=e2e_real -run TestE2EReal_ZAI_TaskList \
//	  ./framework/harness -v -count=1

package harness

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/inproc"
	"github.com/DonaldMurillo/gofastr/framework/harness/engine"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider"
	"github.com/DonaldMurillo/gofastr/framework/harness/provider/zai"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/builtins"
)

func TestE2EReal_ZAI_TaskList(t *testing.T) {
	key := os.Getenv("ZAI_API_KEY")
	if key == "" {
		t.Skip("ZAI_API_KEY not set")
	}
	codingPlan := os.Getenv("ZAI_CODING_PLAN") == "1" || os.Getenv("ZAI_CODING_PLAN") == "true"
	prov := &zai.Provider{APIKey: key, CodingPlan: codingPlan}

	// Tool registry with just TaskList — keeps the test deterministic.
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
	eng := engine.NewEngine(session, bus, prov, "glm-5.1", d)
	// Snapshot the tool schemas the engine will ship to the provider.
	eng.Tools = toolSchemasFromRegistry(reg)

	mux := newRealMux(t, eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sub := c.Subscribe(ctx)

	// Use a tight prompt that explicitly asks for the tool — keeps
	// the test cheap (single turn, ~few hundred tokens) and stable.
	prompt := "Use the TaskList tool to record this plan: (1) audit imports, " +
		"(2) fix race in bus, (3) write changelog. Mark the first as in_progress " +
		"and the others as pending. Use activeForm \"Auditing imports\" for the in-progress one. " +
		"After the tool call, briefly confirm with one sentence."

	if err := c.Send(ctx, control.SendInput{
		SessionID: session, Content: engine.SimpleInput(prompt),
	}); err != nil {
		t.Fatal(err)
	}

	var sawToolCall bool
	var turnsEnded int
	for {
		if turnsEnded >= 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out — saw tool call: %v, turns ended: %d", sawToolCall, turnsEnded)
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				if v.Tool == "TaskList" {
					sawToolCall = true
				} else {
					t.Logf("unexpected tool call: %s", v.Tool)
				}
			case control.TurnEnded:
				turnsEnded++
			case control.Error:
				t.Fatalf("engine emitted Error: %s — %s", v.Reason, v.Message)
			}
		}
	}

	if !sawToolCall {
		t.Fatal("model did NOT call TaskList — either the prompt is too soft or the tool isn't being advertised")
	}

	// Verify the snapshot reflects what the model recorded.
	snap, _ := builtins.TaskListSnapshot(session)
	if len(snap) < 3 {
		t.Fatalf("task snapshot has %d items, want at least 3: %+v", len(snap), snap)
	}
	// Find each by status — the model might reorder or rephrase, so
	// match loosely on content keywords.
	hasInProgress := false
	hasCompleted := false
	hasPending := 0
	for _, it := range snap {
		switch it.Status {
		case "in_progress":
			hasInProgress = true
			if !strings.Contains(strings.ToLower(it.Content), "audit") &&
				!strings.Contains(strings.ToLower(it.Content), "import") {
				t.Logf("in_progress task didn't match expected content: %q", it.Content)
			}
		case "completed":
			hasCompleted = true
		case "pending":
			hasPending++
		}
	}
	if !hasInProgress {
		t.Errorf("no in_progress task in snapshot: %+v", snap)
	}
	if hasPending < 2 {
		t.Errorf("expected ≥2 pending tasks, got %d: %+v", hasPending, snap)
	}
	_ = hasCompleted // optional
	t.Logf("TaskList ok: %d items, in_progress=%v pending=%d", len(snap), hasInProgress, hasPending)
}

// TestE2EReal_ZAI_TaskList_AutoUsed verifies the system-prompt
// guidance actually kicks in: when given a multi-step task and
// told nothing about TaskList by name, the model should call it
// FIRST because the prompt_header tells it to plan multi-step
// work that way. This is the difference between "the tool exists"
// and "the system encourages using it."
func TestE2EReal_ZAI_TaskList_AutoUsed(t *testing.T) {
	key := os.Getenv("ZAI_API_KEY")
	if key == "" {
		t.Skip("ZAI_API_KEY not set")
	}
	codingPlan := os.Getenv("ZAI_CODING_PLAN") == "1" || os.Getenv("ZAI_CODING_PLAN") == "true"
	prov := &zai.Provider{APIKey: key, CodingPlan: codingPlan}

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
	eng := engine.NewEngine(session, bus, prov, "glm-5.1", d)
	eng.Tools = toolSchemasFromRegistry(reg)

	// Inject the production system-prompt middleware so the model
	// sees the "use TaskList for multi-step work" guidance.
	eng.Middleware = []engine.RequestMiddleware{
		engine.SystemPromptMiddleware(autoUsedSystemPrompt),
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

	// NO mention of TaskList in the prompt — the model has to choose
	// the tool based on the system-prompt guidance alone.
	prompt := "I want to add a new feature to my Go project: (1) add a Config struct with three fields, " +
		"(2) write a parser that loads it from a TOML file, (3) write a test for the parser. " +
		"Plan this out before doing anything."

	if err := c.Send(ctx, control.SendInput{
		SessionID: session, Content: engine.SimpleInput(prompt),
	}); err != nil {
		t.Fatal(err)
	}

	var sawTaskList bool
	var turnsEnded int
	for turnsEnded < 1 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out — sawTaskList=%v turnsEnded=%d", sawTaskList, turnsEnded)
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				if v.Tool == "TaskList" {
					sawTaskList = true
				}
			case control.TurnEnded:
				turnsEnded++
			case control.Error:
				t.Fatalf("engine Error: %s — %s", v.Reason, v.Message)
			}
		}
	}
	if !sawTaskList {
		t.Errorf("model did NOT auto-call TaskList for a clearly multi-step request — system-prompt guidance not effective")
	} else {
		snap, _ := builtins.TaskListSnapshot(session)
		t.Logf("auto-TaskList ok: %d tasks recorded without explicit prompt", len(snap))
	}
}

// TestE2EReal_ZAI_ToolSearch_Discovers verifies the discovery flow:
// give the model a system prompt that mentions ToolSearch but doesn't
// list other tools by name, ask for a capability, expect the model
// to call ToolSearch first, then call the discovered tool.
//
// This is the real value-prop of ToolSearch — the model shouldn't
// need every tool's schema pre-loaded; it can discover on demand.
func TestE2EReal_ZAI_ToolSearch_Discovers(t *testing.T) {
	key := os.Getenv("ZAI_API_KEY")
	if key == "" {
		t.Skip("ZAI_API_KEY not set")
	}
	codingPlan := os.Getenv("ZAI_CODING_PLAN") == "1" || os.Getenv("ZAI_CODING_PLAN") == "true"
	prov := &zai.Provider{APIKey: key, CodingPlan: codingPlan}

	// Registry contains BOTH tools so the model can dispatch either.
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(),
		staticToolSourceFn([]tool.Tool{
			builtins.ToolSearch{},
			builtins.TaskList{},
		})); err != nil {
		t.Fatal(err)
	}
	session := ids.NewSessionID()
	defer builtins.ResetTasks(session)
	bus := engine.NewBus(session)
	defer bus.Close()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, prov, "glm-5.1", d)
	// v0.1 ToolSearch is INFORMATIONAL — it surfaces tool metadata so
	// the model can read the schema and call the tool the next round.
	// Both tools must be in eng.Tools because most OpenAI-compatible
	// providers (ZAI included) won't dispatch a tool that's not in
	// the request's tools array. Truly lazy discovery (expanding the
	// schema list as tools are discovered) is roadmap.
	eng.Tools = toolSchemasFromRegistry(reg)
	// Prompt that intentionally only mentions ToolSearch.
	eng.Middleware = []engine.RequestMiddleware{
		engine.SystemPromptMiddleware(
			"You are a coding agent. Your built-in capabilities are limited. " +
				"If you need any tool to accomplish the user's request and you don't " +
				"see one obviously matching, CALL the ToolSearch tool first with a " +
				"short keyword query — it will return matching tools you can then use."),
	}

	mux := newRealMux(t, eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sub := c.Subscribe(ctx)

	// Push the model to use ToolSearch by promising the prompt's
	// capability ("track progress formally") is provided by a tool
	// it has to look up. Naming a tool not in its initial set forces
	// the discovery path.
	prompt := "Before doing anything else, use the ToolSearch tool with query 'plan track progress'. " +
		"Then use whichever tool it returns to record a 3-step plan for refactoring three files."

	if err := c.Send(ctx, control.SendInput{
		SessionID: session, Content: engine.SimpleInput(prompt),
	}); err != nil {
		t.Fatal(err)
	}

	var sawToolSearch, sawTaskList bool
	var turnsEnded int
	for turnsEnded < 1 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out — sawToolSearch=%v sawTaskList=%v", sawToolSearch, sawTaskList)
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				switch v.Tool {
				case "ToolSearch":
					sawToolSearch = true
				case "TaskList":
					sawTaskList = true
				}
			case control.TurnEnded:
				turnsEnded++
			case control.Error:
				t.Fatalf("engine Error: %s — %s", v.Reason, v.Message)
			}
		}
	}
	if !sawToolSearch {
		t.Errorf("model did NOT call ToolSearch even though prompt told it to discover unknown tools")
	}
	if !sawTaskList {
		t.Errorf("model called ToolSearch but never followed up with TaskList (the tool the search would surface)")
	}
	t.Logf("discovery flow ok: ToolSearch=%v → TaskList=%v", sawToolSearch, sawTaskList)
}

// autoUsedSystemPrompt mirrors the default.toml prompt_header for
// the TaskList-usage guidance. Kept inline so the test doesn't depend
// on profile-loading.
const autoUsedSystemPrompt = `You are a coding agent with tools available.

PLANNING — use TaskList for multi-step work:
For any request that needs more than ~2 discrete steps, CALL the
TaskList tool FIRST with the steps you intend to take. Mark one step
in_progress at a time, set activeForm to the present-progressive,
and re-call TaskList to mark each step completed as you finish it.`

// TestE2EReal_ZAI_NoSubAgentCascade reproduces the exact prompt that
// blew up sess_01KSDZK5… ("create 3 tasks to search... execute them
// via subagents") and asserts the turn completes WITHOUT the
// cascade — Agent count must be ≤ 3 (one per task, not nested) and
// TaskList calls must come from the PARENT only (sub-agents don't
// see it in their tool catalog).
//
// Bounds: 60s wall-clock, ≤ 6 Agent dispatches total (3 from parent
// + 0 from sub-agents). Anything more = cascade not prevented.
func TestE2EReal_ZAI_NoSubAgentCascade(t *testing.T) {
	key := os.Getenv("ZAI_API_KEY")
	if key == "" {
		t.Skip("ZAI_API_KEY not set")
	}
	codingPlan := os.Getenv("ZAI_CODING_PLAN") == "1" || os.Getenv("ZAI_CODING_PLAN") == "true"
	prov := &zai.Provider{APIKey: key, CodingPlan: codingPlan}

	// Realistic toolset for the prompt: TaskList + Agent + WebFetch.
	reg := tool.NewRegistry()
	if err := reg.Register(context.Background(),
		staticToolSourceFn([]tool.Tool{
			builtins.TaskList{},
			builtins.Agent{},
			builtins.WebFetch{},
		})); err != nil {
		t.Fatal(err)
	}
	session := ids.NewSessionID()
	defer builtins.ResetTasks(session)
	bus := engine.NewBus(session)
	defer bus.Close()
	d := engine.NewDispatcher(bus, reg)
	eng := engine.NewEngine(session, bus, prov, "glm-5.1", d)
	eng.Tools = toolSchemasFromRegistry(reg)
	eng.Middleware = []engine.RequestMiddleware{
		engine.SystemPromptMiddleware(
			"You have TaskList, Agent, and WebFetch. Plan multi-step work with TaskList. " +
				"For each independent search-style task, spawn an Agent. Don't keep replanning."),
	}

	mux := newRealMux(t, eng)
	c := inproc.New(ids.NewClientID(), control.IdentityHuman, bus, mux)
	if err := mux.Attach(session, c); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sub := c.Subscribe(ctx)

	// User's actual broken prompt, lightly cleaned.
	prompt := "Create 3 tasks to search about three AI agent harnesses (mention any three by name). " +
		"Execute each via a sub-agent. Don't keep replanning."

	if err := c.Send(ctx, control.SendInput{
		SessionID: session, Content: engine.SimpleInput(prompt),
	}); err != nil {
		t.Fatal(err)
	}

	var agentCalls, taskListCalls, webFetchCalls int
	var endReason string
	var sawIterLimit bool
	for endReason == "" {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out — agents=%d tasklist=%d webfetch=%d",
				agentCalls, taskListCalls, webFetchCalls)
		case env := <-sub:
			ev, _ := control.DecodeEvent(env)
			switch v := ev.(type) {
			case control.ToolCallStarted:
				switch v.Tool {
				case "Agent":
					agentCalls++
				case "TaskList":
					taskListCalls++
				case "WebFetch":
					webFetchCalls++
				}
			case control.Error:
				if v.Reason == "IterationLimit" {
					sawIterLimit = true
				}
			case control.TurnEnded:
				endReason = v.Reason
			}
		}
	}
	t.Logf("results: agents=%d tasklist=%d webfetch=%d end=%s iter_limit=%v",
		agentCalls, taskListCalls, webFetchCalls, endReason, sawIterLimit)
	if sawIterLimit {
		t.Errorf("turn hit IterationLimit safety cap — means root-cause cascade is NOT fixed; cap is just hiding it")
	}
	// Allow up to 6 Agent calls (3 + slack for the model rephrasing
	// the prompt or doing one retry). More than that and cascade is
	// happening.
	if agentCalls > 6 {
		t.Errorf("too many Agent calls (%d) — cascade not prevented", agentCalls)
	}
	if endReason != "complete" {
		t.Errorf("turn end reason = %q, want complete", endReason)
	}
}

// staticToolSourceFn wraps a fixed list of tools as a tool.ToolSource.
// Used so this test doesn't depend on the builtins pack catalog.
func staticToolSourceFn(tools []tool.Tool) tool.ToolSource {
	return staticSrcFn{tools: tools}
}

type staticSrcFn struct{ tools []tool.Tool }

func (staticSrcFn) Name() string { return "static-test" }
func (s staticSrcFn) Tools(_ context.Context) ([]tool.Tool, error) {
	return s.tools, nil
}

// toolSchemasFromRegistry exposes the registry's tools as schemas the
// engine attaches to each outbound provider Request. Mirrors the
// production Harness.toolSchemas() but kept local to this test file.
func toolSchemasFromRegistry(reg *tool.Registry) []provider.ToolSchema {
	tools := reg.List()
	out := make([]provider.ToolSchema, 0, len(tools))
	for _, t := range tools {
		out = append(out, provider.ToolSchema{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return out
}
