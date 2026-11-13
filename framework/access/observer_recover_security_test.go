package access

import (
	"context"
	"testing"
)

// R11 U4-02: observe ran the installed observer inline with no recover,
// the twin hook and event were already fixed for (#669). A panicking
// observer is coverage tooling failing, and it must not take the
// permission check, or the goroutine running it, with it. The verdict
// still comes back.
func TestPanickingObserverKeepsVerdict(t *testing.T) {
	SetObserver(func(Evaluation) { panic("observer boom") })
	defer SetObserver(nil)

	policy := NewRolePolicy()
	policy.Grant("editor", "posts:read")
	ctx := WithRoles(WithPolicy(context.Background(), policy), []string{"editor"})

	// A panic in the observer would cross these calls and fail the test.
	if !Can(ctx, "posts:read") {
		t.Fatal("granted permission was denied after the observer panicked")
	}
	if Can(ctx, "posts:delete") {
		t.Fatal("ungranted permission was allowed after the observer panicked")
	}
}
