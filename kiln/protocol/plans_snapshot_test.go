package protocol_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// world_get on _plans returned the session's own plan map. The transport
// encodes the result after the read lock is released, while approve_plan
// writes the map and flips Approved on the stored *Plan, so a concurrent
// encode is a fatal map iteration/write (2026-10-27 K2). The result is a
// copy: a later approve does not reach a result already handed out.
func TestPlansReadIsASnapshot(t *testing.T) {
	tools := newTools(t)
	ctx := context.Background()
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{PlanID: "p1", Steps: []string{"one"}}); !res.OK {
		t.Fatalf("propose: %+v", res)
	}
	got := tools.WorldGet(ctx, protocol.WorldGetArgs{Path: "_plans"})
	if !got.OK {
		t.Fatalf("world_get: %+v", got)
	}
	plans, ok := got.Result.(map[string]*journal.Plan)
	if !ok {
		t.Fatalf("result is %T", got.Result)
	}

	if res := tools.ApprovePlan(ctx, protocol.ApprovePlanArgs{PlanID: "p1"}); !res.OK {
		t.Fatalf("approve: %+v", res)
	}
	if res := tools.ProposePlan(ctx, protocol.ProposePlanArgs{PlanID: "p2", Steps: []string{"two"}}); !res.OK {
		t.Fatalf("propose p2: %+v", res)
	}
	if plans["p1"].Approved {
		t.Fatal("approving p1 changed a _plans result already returned")
	}
	if _, seen := plans["p2"]; seen {
		t.Fatal("a later proposal reached a _plans result already returned")
	}
}
