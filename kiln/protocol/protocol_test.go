package protocol_test

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
	"github.com/DonaldMurillo/gofastr/framework"
)

func newTools(t *testing.T) *protocol.Tools {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-proto")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l)
}

func TestAddEntityHappyPath(t *testing.T) {
	tools := newTools(t)
	res := tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{
			Name:   "posts",
			Fields: []world.Field{{Name: "title", Type: "string", Required: true}},
		},
	})
	if !res.OK {
		t.Fatalf("expected OK, got %+v", res)
	}
}

func TestAddEntityDuplicate(t *testing.T) {
	tools := newTools(t)
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	res := tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: posts})
	if !res.OK {
		t.Fatalf("first add: %+v", res)
	}
	res = tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: posts})
	if res.OK {
		t.Fatal("duplicate should fail")
	}
	if res.Kind != "conflict" {
		t.Errorf("kind = %q, want conflict", res.Kind)
	}
}

func TestAddFieldUnknownEntity(t *testing.T) {
	tools := newTools(t)
	res := tools.AddField(context.Background(), protocol.AddFieldArgs{
		Entity: "missing",
		Field:  world.Field{Name: "x", Type: "string"},
	})
	if res.OK {
		t.Fatal("expected failure")
	}
	if res.Kind != "not_found" {
		t.Errorf("kind = %q, want not_found", res.Kind)
	}
}

func TestDeleteEntityRequiresApprovedPlan(t *testing.T) {
	tools := newTools(t)
	posts := &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}}
	if r := tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: posts}); !r.OK {
		t.Fatal("add: ", r)
	}

	// 1) No plan_id → needs_plan.
	res := tools.DeleteEntity(context.Background(), protocol.DeleteEntityArgs{Name: "posts"})
	if res.OK {
		t.Fatal("first call without plan must be blocked")
	}
	if res.Kind != "needs_plan" {
		t.Errorf("kind = %q, want needs_plan", res.Kind)
	}

	// 2) Propose a plan but don't approve → still blocked.
	if r := tools.ProposePlan(context.Background(), protocol.ProposePlanArgs{
		PlanID:  "p1",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !r.OK {
		t.Fatal("propose: ", r)
	}
	res = tools.DeleteEntity(context.Background(), protocol.DeleteEntityArgs{Name: "posts", PlanID: "p1"})
	if res.OK || res.Kind != "needs_plan" {
		t.Errorf("unapproved plan should block, got %+v", res)
	}

	// 3) Approved plan → succeeds.
	if r := tools.ApprovePlan(context.Background(), protocol.ApprovePlanArgs{PlanID: "p1"}); !r.OK {
		t.Fatal("approve: ", r)
	}
	res = tools.DeleteEntity(context.Background(), protocol.DeleteEntityArgs{Name: "posts", PlanID: "p1"})
	if !res.OK {
		t.Fatalf("approved plan should authorize delete, got %+v", res)
	}

	// 4) Replay attempt: re-add posts and try the same plan again — must block.
	if r := tools.AddEntity(context.Background(), protocol.AddEntityArgs{Entity: posts}); !r.OK {
		t.Fatal("re-add: ", r)
	}
	res = tools.DeleteEntity(context.Background(), protocol.DeleteEntityArgs{Name: "posts", PlanID: "p1"})
	if res.OK {
		t.Errorf("plan reuse should be blocked, got OK")
	}
	if res.Kind != "needs_plan" {
		t.Errorf("replay kind = %q, want needs_plan", res.Kind)
	}
}

func TestDeleteEntityWrongTargetBlocked(t *testing.T) {
	tools := newTools(t)
	if r := tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "users", Fields: []world.Field{{Name: "email", Type: "string"}}},
	}); !r.OK {
		t.Fatal(r)
	}
	// Plan covers users, agent tries to delete posts.
	if r := tools.ProposePlan(context.Background(), protocol.ProposePlanArgs{
		PlanID:  "p1",
		Steps:   []string{"drop users"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "users"}},
	}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.ApprovePlan(context.Background(), protocol.ApprovePlanArgs{PlanID: "p1"}); !r.OK {
		t.Fatal(r)
	}
	res := tools.DeleteEntity(context.Background(), protocol.DeleteEntityArgs{Name: "posts", PlanID: "p1"})
	if res.OK {
		t.Fatalf("plan covering users must not authorize deleting posts, got %+v", res)
	}
}

func TestRejectPlanBlocksApproval(t *testing.T) {
	tools := newTools(t)
	if r := tools.ProposePlan(context.Background(), protocol.ProposePlanArgs{
		PlanID:  "p1",
		Steps:   []string{"drop posts"},
		Targets: []journal.PlanTarget{{Op: "delete_entity", Name: "posts"}},
	}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.RejectPlan(context.Background(), protocol.RejectPlanArgs{PlanID: "p1", Reason: "not now"}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.ApprovePlan(context.Background(), protocol.ApprovePlanArgs{PlanID: "p1"}); r.OK {
		t.Fatal("approve after reject must fail")
	}
}

func TestWorldGetReturnsCurrentState(t *testing.T) {
	tools := newTools(t)
	if r := tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !r.OK {
		t.Fatal(r)
	}
	res := tools.WorldGet(context.Background(), protocol.WorldGetArgs{})
	if !res.OK {
		t.Fatalf("WorldGet: %+v", res)
	}
	w, ok := res.Result.(*world.World)
	if !ok {
		t.Fatalf("Result type = %T", res.Result)
	}
	if _, exists := w.Entities["posts"]; !exists {
		t.Error("posts missing from WorldGet")
	}
}

func TestWorldGetWithPath(t *testing.T) {
	tools := newTools(t)
	tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	})
	res := tools.WorldGet(context.Background(), protocol.WorldGetArgs{Path: "entities.posts"})
	if !res.OK {
		t.Fatalf("path get: %+v", res)
	}
	ent, ok := res.Result.(*world.Entity)
	if !ok {
		t.Fatalf("type = %T, want *world.Entity", res.Result)
	}
	if ent.Name != "posts" {
		t.Errorf("got %q", ent.Name)
	}
}

func TestPlanProposeApprove(t *testing.T) {
	tools := newTools(t)
	res := tools.ProposePlan(context.Background(), protocol.ProposePlanArgs{
		PlanID: "p1",
		Steps:  []string{"add posts", "add comments"},
		Reason: "user wants a blog",
	})
	if !res.OK {
		t.Fatalf("propose: %+v", res)
	}
	res = tools.ApprovePlan(context.Background(), protocol.ApprovePlanArgs{PlanID: "p1"})
	if !res.OK {
		t.Fatalf("approve: %+v", res)
	}
	res = tools.ApprovePlan(context.Background(), protocol.ApprovePlanArgs{PlanID: "missing"})
	if res.OK {
		t.Fatal("approving unknown plan should fail")
	}
}

func TestUndoTruncatesJournal(t *testing.T) {
	tools := newTools(t)
	tools.AddEntity(context.Background(), protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	})
	tools.AddField(context.Background(), protocol.AddFieldArgs{
		Entity: "posts",
		Field:  world.Field{Name: "body", Type: "text"},
	})
	res := tools.Undo(context.Background(), protocol.UndoArgs{})
	if !res.OK {
		t.Fatalf("undo: %+v", res)
	}
	w := tools.WorldGet(context.Background(), protocol.WorldGetArgs{}).Result.(*world.World)
	if len(w.Entities["posts"].Fields) != 1 {
		t.Errorf("after undo, fields = %d, want 1", len(w.Entities["posts"].Fields))
	}
}

func TestChatRecordsMessages(t *testing.T) {
	tools := newTools(t)
	if r := tools.Chat(context.Background(), protocol.ChatArgs{Role: "user", Text: "hello"}); !r.OK {
		t.Fatal(r)
	}
	if r := tools.Chat(context.Background(), protocol.ChatArgs{Role: "assistant", Text: "hi back"}); !r.OK {
		t.Fatal(r)
	}
	w := tools.WorldGet(context.Background(), protocol.WorldGetArgs{Path: "_chat"})
	if !w.OK {
		t.Fatalf("worldget _chat: %+v", w)
	}
	chat, ok := w.Result.([]journal.ChatEvent)
	if !ok {
		t.Fatalf("type = %T", w.Result)
	}
	if len(chat) != 2 {
		t.Errorf("expected 2 chat events, got %d", len(chat))
	}
}

func TestToolsListAndDescribe(t *testing.T) {
	tools := newTools(t)
	all := tools.List()
	if len(all) == 0 {
		t.Fatal("List returned no tools")
	}
	for _, name := range []string{"world_get", "add_entity", "delete_entity", "undo"} {
		if _, ok := tools.Describe(name); !ok {
			t.Errorf("missing tool descriptor for %q", name)
		}
	}
}
