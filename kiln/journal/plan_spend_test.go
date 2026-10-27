package journal

import (
	"testing"
)

// A plan target is spent by the edit it authorizes. spendPlan used to
// run before the existence check, so a delete of something absent
// failed AND marked the approval consumed: the in-memory session kept a
// spent target that no journal entry records, and the real delete that
// followed answered needs_plan until a restart re-derived consumption.
func TestFailedDeleteLeavesPlanUnspent(t *testing.T) {
	cases := []struct {
		target  PlanTarget
		op      Op
		payload any
	}{
		{PlanTarget{Op: "delete_hook", Name: "h1"}, OpDeleteHook, DeleteHookPayload{ID: "h1", PlanID: "p1"}},
		{PlanTarget{Op: "delete_route", Name: "GET /x"}, OpDeleteRoute, DeleteRoutePayload{Method: "GET", Path: "/x", PlanID: "p1"}},
		{PlanTarget{Op: "delete_field", Name: "ghost.title"}, OpDeleteField, DeleteFieldPayload{Entity: "ghost", Field: "title", PlanID: "p1"}},
		{PlanTarget{Op: "delete_entity", Name: "ghost"}, OpDeleteEntity, DeleteEntityPayload{Name: "ghost", PlanID: "p1"}},
		{PlanTarget{Op: "delete_page", Name: "/ghost"}, OpDeletePage, DeletePagePayload{Path: "/ghost", PlanID: "p1"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.op), func(t *testing.T) {
			s := NewSession()
			mustApply(t, s, planEntry(KindPlanProposed, PlanProposedPayload{
				PlanID: "p1", Steps: []string{"delete"}, Targets: []PlanTarget{tc.target},
			}))
			mustApply(t, s, planEntry(KindPlanApproved, PlanApprovedPayload{PlanID: "p1"}))
			if err := Apply(s, worldEdit(tc.op, tc.payload)); err == nil {
				t.Fatalf("%s of an absent target succeeded", tc.op)
			}
			if len(s.Consumed["p1"]) != 0 {
				t.Errorf("failed %s spent the plan: Consumed = %v", tc.op, s.Consumed)
			}
		})
	}
}
