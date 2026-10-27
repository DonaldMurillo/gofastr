package framework

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// a2aAdminOnlyGate is a WithMCPGate precondition that admits only u-bob,
// the shape of an admin-only /mcp.
func a2aAdminOnlyGate(ctx context.Context) error {
	if raw, ok := handler.GetUser(ctx); ok {
		if u, ok := raw.(*a2aTestUser); ok && u.id == "u-bob" {
			return nil
		}
	}
	return errors.New("mcp is admin-only")
}

// The agent card is public, and its entity skills name every entity and
// its field names. They came from the unfiltered tool registry, so a gate
// that closes /mcp to anonymous callers still published them to every
// visitor. The card lists what an anonymous tools/list would show.
func TestA2ACardHidesGatedEntitySkills(t *testing.T) {
	skillIDs := func(gate func(context.Context) error) []string {
		installA2AOwnerExtractor(t)
		app := NewApp(WithDB(sqliteDB(t)), WithA2A(A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}), WithMCPGate(gate))
		crudTrue := true
		app.Entity("notes", entity.EntityConfig{
			Table:    "notes",
			Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
			Exposure: &entity.ExposureConfig{CRUD: &crudTrue, MCP: true},
		}.WithTimestamps(false))
		var ids []string
		for _, sk := range app.A2ASkills() {
			ids = append(ids, sk.ID)
		}
		return ids
	}
	if got := skillIDs(MCPRequireUser()); len(got) != 1 || got[0] != "echo" {
		t.Errorf("SECURITY: [disclosure] a gate refusing anonymous callers left card skills %v, want only the hand-written echo", got)
	}
	if got := skillIDs(func(context.Context) error { return nil }); len(got) != 2 || got[1] != "entity.notes" {
		t.Errorf("an open gate's card skills = %v, want echo and entity.notes", got)
	}
}

// The entity skills call the MCP tools in process, and Server.CallTool
// skips the server-wide gate: a host that closed /mcp to everyone but its
// admin still served every entity's CRUD at POST /a2a to any signed-in
// caller.
func TestA2ASkillHonorsMCPGate(t *testing.T) {
	env := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{a2aEchoSkill()}}, WithMCPGate(a2aAdminOnlyGate))
	env.seedNote(t, "n1", "u-alice", "alice only")
	env.seedNote(t, "n2", "u-bob", "bob only")

	code, body := env.a2aRequest(t, "tok-alice", a2aSendMessage("list", map[string]any{}))
	if code != http.StatusOK {
		t.Fatalf("SendMessage: got %d body %s", code, body)
	}
	task := a2aTask(t, body)
	if got := taskState(task); got == "TASK_STATE_COMPLETED" || strings.Contains(string(body), "alice only") {
		t.Fatalf("SECURITY: [authz] the MCP gate refused alice but her A2A skill call ran (state %s): %s", got, body)
	}
	if !strings.Contains(string(body), "mcp is admin-only") {
		t.Errorf("the refusal must carry the gate's error, as /mcp does: %s", body)
	}

	// The gate the admin passes still lets the skill run.
	code, body = env.a2aRequest(t, "tok-bob", a2aSendMessage("list", map[string]any{}))
	if code != http.StatusOK {
		t.Fatalf("SendMessage as bob: got %d body %s", code, body)
	}
	task = a2aTask(t, body)
	if got := taskState(task); got != "TASK_STATE_COMPLETED" || !titleSet(artifactTitles(t, task))["bob only"] {
		t.Fatalf("an allowed caller's skill call = %s, want COMPLETED with bob's row: %s", got, body)
	}
}
