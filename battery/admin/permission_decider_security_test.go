package admin

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// The modules:manage lever and the grant and revoke checks read the
// caller's permissions from the policy alone, so a Decider that denied
// an admin that one permission was never asked (2026-10-27 A34). Its
// deny now wins, and its allow still admits nothing the policy does not.
func TestPermissionCheckAsksDecider(t *testing.T) {
	p := access.NewRolePolicy()
	_ = p.Grant("admin", access.Wildcard)
	b := &Battery{cfg: Config{Policy: p}}
	denyManage := func(_ context.Context, _ []string, c access.Permission, _ access.Ref) access.Decision {
		if c == modulesManage {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	ctx := access.WithDecider(handler.SetUser(context.Background(), theAdmin), denyManage)
	if b.callerHoldsPermission(ctx, modulesManage) {
		t.Error("SECURITY: a Decider's deny of modules:manage left the lever open")
	}
	if !b.callerHoldsPermission(ctx, "posts:write") {
		t.Error("a deny of one permission refused another")
	}

	allow := func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return access.DecisionAllow
	}
	nobody := &Battery{cfg: Config{Policy: access.NewRolePolicy()}}
	if nobody.callerHoldsPermission(access.WithDecider(handler.SetUser(context.Background(), theAdmin), allow), modulesManage) {
		t.Error("a Decider's allow granted a permission the policy does not")
	}
}
