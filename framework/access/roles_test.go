package access

import (
	"context"
	"reflect"
	"testing"
)

// TestGetRoles_RoundTrips confirms GetRoles reads back the roles stored
// by WithRoles — the reader half that unblocks role-based UI branching.
func TestGetRoles_RoundTrips(t *testing.T) {
	ctx := WithRoles(context.Background(), []string{"editor", "admin"})
	got := GetRoles(ctx)
	if !reflect.DeepEqual(got, []string{"editor", "admin"}) {
		t.Fatalf("GetRoles = %v, want [editor admin]", got)
	}
}

// TestGetRoles_NilCtx returns nil for a nil context rather than panicking.
func TestGetRoles_NilCtx(t *testing.T) {
	if got := GetRoles(nil); got != nil {
		t.Fatalf("GetRoles(nil) = %v, want nil", got)
	}
}

// TestGetRoles_NoRoles returns nil when nothing was stored.
func TestGetRoles_NoRoles(t *testing.T) {
	if got := GetRoles(context.Background()); got != nil {
		t.Fatalf("GetRoles(empty) = %v, want nil", got)
	}
}
