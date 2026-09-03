package framework

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
)

// The wiring's smaller branches: option validation, the accessor, the
// no-DB store fallback, the empty-skill refusal, tool-name grouping
// against custom and namespaced tools, and the data-part reader's
// refusals. Each is a real behaviour a host can hit, not a coverage
// number.

func TestA2A_PathMustStartWithSlash(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "must start with /") {
			t.Fatalf("WithA2A accepted a relative path: recovered %v", r)
		}
	}()
	NewApp(WithA2A(A2AConfig{Path: "a2a"}))
}

func TestA2A_AccessorNilUntilStarted(t *testing.T) {
	installA2AOwnerExtractor(t)
	app := NewApp(WithA2A(A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}))
	if app.A2A() != nil {
		t.Fatal("A2A() must be nil before Start mounts the exchange")
	}
	if NewApp().A2A() != nil || NewApp().A2ASkills() != nil {
		t.Fatal("an app without WithA2A must report no exchange and no skills")
	}
	// No DB: the memory store carries the exchange.
	startOnRandomPort(t, app)
	if app.A2A() == nil {
		t.Fatal("A2A() must be the mounted server after Start")
	}
	if got := app.A2A().Skills(); len(got) != 1 || got[0].ID != "echo" {
		t.Fatalf("skills = %+v, want the echo skill only (no entities registered)", got)
	}
}

func TestA2A_NoSkillsAtAllFailsMount(t *testing.T) {
	installA2AOwnerExtractor(t)
	app := NewApp(WithA2A(A2AConfig{DisableEntitySkills: true}))
	err := app.mountA2A()
	if err == nil || !strings.Contains(err.Error(), "at least one skill") {
		t.Fatalf("mount with no skills: err = %v, want the core/a2a refusal", err)
	}
}

func TestA2A_EntitySkillGroupingIgnoresCustomTools(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}})
	noop := func(context.Context, map[string]any) (any, error) { return nil, nil }
	for _, name := range []string{"report_create", "notes_purge", "audit.export", "v2.notes.list", "v2.ghost.get"} {
		if err := env.app.MCP.RegisterTool(name, name, map[string]any{"type": "object"}, noop); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	sets := env.app.entitySkillSets()
	var keys []string
	for _, s := range sets {
		key := s.entity
		if s.ns != "" {
			key = s.ns + "." + s.entity
		}
		keys = append(keys, key)
	}
	// report_create: no "report" entity; notes_purge: not a CRUD action;
	// audit.export: not the 3-part namespaced shape; v2.ghost.get: no
	// "ghost" entity. v2.notes.list groups under the v2 namespace.
	if strings.Join(keys, ",") != "notes,v2.notes" {
		t.Fatalf("entity skill sets = %v, want [notes v2.notes]", keys)
	}
	v2 := env.app.newEntitySkill(sets[1])
	if v2.ID != "entity.v2.notes" || v2.Tags[1] != "v2.notes" {
		t.Fatalf("namespaced skill = %+v", v2)
	}
	// A tool with no schema properties describes as action().
	if !strings.Contains(v2.Description, "\nlist()") {
		t.Fatalf("description for a propertyless tool = %q", v2.Description)
	}
}

func TestCapitalizeEdges(t *testing.T) {
	for in, want := range map[string]string{"": "", "Notes": "Notes", "notes": "Notes", "1x": "1x"} {
		if got := capitalize(in); got != want {
			t.Errorf("capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntityInvocationRefusals(t *testing.T) {
	txt := "list please"
	num := any(3.0)
	noOp := any(map[string]any{"skill": "entity.notes"})
	badArgs := any(map[string]any{"operation": "list", "arguments": "nope"})
	nilArgs := any(map[string]any{"operation": "get", "arguments": nil})
	for name, msg := range map[string]*a2a.Message{
		"nil message":     nil,
		"text only":       {Parts: []a2a.Part{{Text: &txt}}},
		"non-object data": {Parts: []a2a.Part{{Data: &num}}},
		"no operation":    {Parts: []a2a.Part{{Data: &noOp}}},
	} {
		if _, _, err := entityInvocation(msg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, _, err := entityInvocation(&a2a.Message{Parts: []a2a.Part{{Data: &badArgs}}}); err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Errorf("string arguments: err = %v", err)
	}
	op, args, err := entityInvocation(&a2a.Message{Parts: []a2a.Part{{Data: &nilArgs}}})
	if err != nil || op != "get" || args == nil || len(args) != 0 {
		t.Errorf("nil arguments: op=%q args=%v err=%v, want get with empty args", op, args, err)
	}
}

// TestEntityInvocationMissingOperationFallsThrough pins the data-part
// loop's presence-before-type contract: a part whose object simply
// LACKS "operation" (e.g. a routing hint) is skipped, not rejected, so
// a valid operation in a LATER part still parses. The laxcoerce bug
// shape: a failed type assertion on a missing key is not absence of
// the key.
func TestEntityInvocationMissingOperationFallsThrough(t *testing.T) {
	hint := any(map[string]any{"skill": "entity.notes"})
	valid := any(map[string]any{"operation": "list"})
	op, args, err := entityInvocation(&a2a.Message{Parts: []a2a.Part{
		{Data: &hint},
		{Data: &valid},
	}})
	if err != nil {
		t.Fatalf("missing key before a later valid part must fall through, got err = %v", err)
	}
	if op != "list" || args == nil || len(args) != 0 {
		t.Fatalf("op=%q args=%v, want list with empty args", op, args)
	}
}

// TestEntityInvocationPresentNonStringRejected: a PRESENT non-string
// "operation" is a client error, not a skip — the empty-string skip
// and the missing-key skip must not swallow it.
func TestEntityInvocationPresentNonStringRejected(t *testing.T) {
	num := any(3.0)
	badOp := any(map[string]any{"operation": num})
	if _, _, err := entityInvocation(&a2a.Message{Parts: []a2a.Part{{Data: &badOp}}}); err == nil ||
		!strings.Contains(err.Error(), `data part "operation" must be a string`) {
		t.Fatalf("present non-string operation: err = %v, want the must-be-a-string client error", err)
	}
	// A nil-valued operation is present-but-not-a-string too.
	nilOp := any(map[string]any{"operation": nil})
	if _, _, err := entityInvocation(&a2a.Message{Parts: []a2a.Part{{Data: &nilOp}}}); err == nil {
		t.Fatal("nil-valued operation must be rejected, not skipped")
	}
}

// A tool refusal (update without an id) fails the task and carries the
// refusal text; it is not a rejection, which is reserved for a request
// the skill cannot even read.
func TestA2A_ToolErrorFailsTask(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}})
	status, body := env.a2aRequest(t, "tok-alice", a2aSendMessage("update", map[string]any{}))
	if status != 200 {
		t.Fatalf("status = %d: %s", status, body)
	}
	task := a2aTask(t, body)
	if taskState(task) != string(a2a.TaskStateFailed) {
		t.Fatalf("state = %s, want TASK_STATE_FAILED: %s", taskState(task), body)
	}
}
