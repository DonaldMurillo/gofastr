package live_test

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// A delete the session refuses leaves its plan unspent on the live path.
// The spend used to come before the existence check, so the approval was
// marked consumed in memory with no journal record behind it. Live.Apply
// also restores the session from the journal on any refusal; no op
// changes the session and then fails any more, so that branch has no
// input that reaches it and no test here.
func TestRefusedEntryLeavesPlanUnspent(t *testing.T) {
	l, _ := newTestLive(t, journal.NewMemory())
	tools := protocol.New(l)
	if res := tools.ProposePlan(t.Context(), protocol.ProposePlanArgs{
		PlanID: "p1", Steps: []string{"drop h1"},
		Targets: []journal.PlanTarget{{Op: "delete_hook", Name: "h1"}},
	}); !res.OK {
		t.Fatalf("propose_plan: %+v", res)
	}
	if res := tools.ApprovePlan(t.Context(), protocol.ApprovePlanArgs{PlanID: "p1"}); !res.OK {
		t.Fatalf("approve_plan: %+v", res)
	}
	del := newEntry(t, "d1", time.Now(), journal.KindWorldEdit, journal.OpDeleteHook,
		journal.DeleteHookPayload{ID: "h1", PlanID: "p1"})
	if err := l.Apply(del); err == nil {
		t.Fatal("delete_hook of an absent hook applied")
	}
	l.ReadSession(func(sess *journal.Session) {
		if len(sess.Consumed["p1"]) != 0 {
			t.Errorf("refused delete_hook left the plan spent: Consumed = %v", sess.Consumed)
		}
	})
}
