package protocol_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Testimonial: documents that the plan-approval gate is a UX nudge, not a
// security boundary; reported issue k-net-2 does not reproduce as a bug.
//
// Kiln's build-mode runtime has a single, deliberately unauthenticated
// HTTP transport (POST /kiln/tool/{name}). Both the human operator (via
// the floating panel) and the spawned agent (via $KILN_URL) drive the
// world through that same surface — there is no token, no session
// identity, and no operator/agent distinction anywhere in the kiln tree.
// A journal entry carries no origin, so ApprovePlan cannot tell who is
// calling it. The plan/approve handshake exists so the agent surfaces a
// destructive intent for a human to eyeball in the panel BEFORE it runs;
// it is not, and cannot be over this transport, an authorization check.
//
// The actual security boundary for the unauthenticated surface is the
// loopback bind (k-net-1: default --addr 127.0.0.1:8765). Once only
// local processes can reach the tool API, "the agent can approve its own
// plan" is the same trust level as "the agent can call delete_entity" —
// both are gated by who can reach localhost, not by the plan handshake.
//
// This test pins the intended behavior: an approve_plan call arriving
// over the same (only) transport succeeds, and the now-approved plan
// authorizes the destructive op it covers. If a future change adds an
// operator token and makes approval reject agent-originated calls, this
// testimonial must be revisited.
func TestPlanApprovalIsUXNudgeNotAuth(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()

	// Seed an entity to delete.
	if res := tools.AddEntity(ctx, protocol.AddEntityArgs{
		Entity: &world.Entity{Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}}},
	}); !res.OK {
		t.Fatalf("add_entity: %+v", res)
	}

	target := journal.PlanTarget{Op: "delete_entity", Name: "posts"}

	// Destructive op without a plan is blocked — the nudge is present.
	if res := tools.DeleteEntity(ctx, protocol.DeleteEntityArgs{Name: "posts"}); res.OK {
		t.Fatal("delete without plan should be blocked")
	} else if res.Kind != "needs_plan" {
		t.Fatalf("kind = %q, want needs_plan", res.Kind)
	}

	// The SAME caller proposes and then approves the plan over the same
	// transport — there is no second, privileged channel. Self-approval
	// succeeds by design.
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{
		PlanID: "p1", Steps: []string{"drop posts"}, Targets: []journal.PlanTarget{target},
	}); !res.OK {
		t.Fatalf("propose_plan: %+v", res)
	}
	if res := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "p1"}); !res.OK {
		t.Fatalf("approve_plan over the agent transport is rejected, but the gate is a UX nudge and self-approval is intended: %+v", res)
	}

	// The self-approved plan now authorizes the destructive op.
	if res := tools.DeleteEntity(ctx, protocol.DeleteEntityArgs{Name: "posts", PlanID: "p1"}); !res.OK {
		t.Fatalf("delete with self-approved plan should succeed: %+v", res)
	}
}
