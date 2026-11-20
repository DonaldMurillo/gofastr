package journal

import "testing"

// A plan gets one verdict: approving a rejected plan and rejecting an
// approved one are both refused, and so is a verdict on an unknown plan.
func TestReplayPlanVerdictIsFinal(t *testing.T) {
	propose := planEntry(KindPlanProposed, PlanProposedPayload{PlanID: "p1", Steps: []string{"x"}})
	cases := []struct {
		name  string
		first Entry
		then  Entry
		want  string
	}{
		{"approve after reject", planEntry(KindPlanRejected, PlanRejectedPayload{PlanID: "p1"}),
			planEntry(KindPlanApproved, PlanApprovedPayload{PlanID: "p1"}), `plan_approved on already-rejected plan "p1"`},
		{"reject after approve", planEntry(KindPlanApproved, PlanApprovedPayload{PlanID: "p1"}),
			planEntry(KindPlanRejected, PlanRejectedPayload{PlanID: "p1"}), `plan_rejected on already-approved plan "p1"`},
	}
	for _, tc := range cases {
		s := NewSession()
		mustApply(t, s, propose)
		mustApply(t, s, tc.first)
		if err := Apply(s, tc.then); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
	for kind, e := range map[string]Entry{
		"plan_approved": planEntry(KindPlanApproved, PlanApprovedPayload{PlanID: "nope"}),
		"plan_rejected": planEntry(KindPlanRejected, PlanRejectedPayload{PlanID: "nope"}),
	} {
		want := kind + ` references unknown plan_id "nope"`
		if err := Apply(NewSession(), e); err == nil || err.Error() != want {
			t.Errorf("%s on an unknown plan: err = %v, want %q", kind, err, want)
		}
	}
}
