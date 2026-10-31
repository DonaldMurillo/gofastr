package live_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// delete_entity drops the table, and its seeds and hooks go with it in
// the same edit. They used to stay in the world: the edit succeeded, and
// freeze then refused the orphan seed, and generation the orphan hook
// (2026-10-27 K22), so the world could never graduate. Undo cuts the
// delete from the journal, which brings back all of them.
func TestDeleteSeededEntityFreezes(t *testing.T) {
	l, db := newTestLive(t, journal.NewMemory())
	tools := protocol.New(l)
	ctx := t.Context()
	if r := tools.SetAppConfig(ctx, protocol.SetAppConfigArgs{Config: world.AppConfig{Name: "app", Module: "example.com/app"}}); !r.OK {
		t.Fatalf("set_app_config: %+v", r)
	}
	for _, name := range []string{"posts", "tags"} {
		if r := tools.AddEntity(ctx, protocol.AddEntityArgs{Entity: &world.Entity{Name: name, Fields: []world.Field{{Name: "title", Type: "string"}}}}); !r.OK {
			t.Fatalf("add_entity %s: %+v", name, r)
		}
		if r := tools.AddSeed(ctx, protocol.AddSeedArgs{Seed: &world.Seed{Entity: name, Rows: []map[string]any{{"title": "hi"}}}}); !r.OK {
			t.Fatalf("add_seed %s: %+v", name, r)
		}
		if r := tools.AddHook(ctx, protocol.AddHookArgs{Hook: &world.Hook{
			ID: name + "_title", Entity: name, When: "before_create",
			Action: world.Action{Kind: world.ActionValidate, Params: map[string]any{
				"expression": `entity.title != ""`, "message": "title required",
			}},
		}}); !r.OK {
			t.Fatalf("add_hook %s: %+v", name, r)
		}
	}
	if r := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID: "p1", Steps: []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !r.OK {
		t.Fatalf("propose_plan: %+v", r)
	}
	if r := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "p1"}); !r.OK {
		t.Fatalf("approve_plan: %+v", r)
	}
	if r := tools.DeleteEntity(ctx, protocol.DeleteEntityArgs{Name: "posts", PlanID: "p1"}); !r.OK {
		t.Fatalf("delete_entity: %+v", r)
	}
	var seeds []*world.Seed
	var hooks []*world.Hook
	var spec error
	l.ReadSession(func(s *journal.Session) {
		seeds, hooks = s.World.Seeds, s.World.Hooks
		_, spec = freeze.AppSpecYAML(s.World)
	})
	if len(seeds) != 1 || seeds[0].Entity != "tags" {
		t.Fatalf("seeds after delete = %v, want only the tags seed", seeds)
	}
	if len(hooks) != 1 || hooks[0].Entity != "tags" {
		t.Fatalf("hooks after delete = %v, want only the tags hook", hooks)
	}
	if spec != nil {
		t.Fatalf("freeze after delete_entity: %v", spec)
	}
	if r := tools.Undo(ctx, protocol.UndoArgs{}); !r.OK {
		t.Fatalf("undo: %+v", r)
	}
	l.ReadSession(func(s *journal.Session) { seeds, hooks = s.World.Seeds, s.World.Hooks })
	if len(seeds) != 2 || len(hooks) != 2 {
		t.Fatalf("after undo: %d seeds and %d hooks, want 2 of each", len(seeds), len(hooks))
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM posts`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("posts rows after undo = %d (%v), want 1", n, err)
	}
}
