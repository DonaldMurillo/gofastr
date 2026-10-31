package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

// authPolicy checked roles itself, so an access Decider's deny never
// reached a role-gated screen (2026-10-27 G17). The generator emits the
// same function; appgen's TestAuthPolicyAsksTheDecider keeps them equal.
func TestAuthPolicyAsksDecider(t *testing.T) {
	admin := &auth.BasicUser{ID: "u1", Email: "a@example.com", Roles: []string{"admin"}}
	deny := func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return access.DecisionDeny
	}
	gate := authPolicy("/login", "admin")
	signedIn := handler.SetUser(context.Background(), admin)

	if d := gate.Decide(signedIn); d.Kind != app.DecisionAllow {
		t.Fatalf("setup: an admin with no decider = %+v, want allow", d)
	}
	if d := gate.Decide(access.WithDecider(signedIn, deny)); d.Kind == app.DecisionAllow {
		t.Error("SECURITY: a denying Decider did not bind the role-gated screen")
	}
	// A scoped API token installs its owner as the user; the screen gate
	// must still refuse it (2026-10-27 G23).
	if d := gate.Decide(auth.WithTokenScopes(signedIn, []string{"customers:read"})); d.Kind == app.DecisionAllow {
		t.Error("SECURITY: a scoped API token passed the role-gated screen on its owner's roles")
	}
	if d := authPolicy("/login", "").Decide(auth.WithTokenScopes(signedIn, []string{"customers:read"})); d.Kind == app.DecisionAllow {
		t.Error("SECURITY: a scoped API token passed the signed-in screen gate")
	}
	member := handler.SetUser(context.Background(), &auth.BasicUser{ID: "u2", Roles: []string{"member"}})
	if d := gate.Decide(member); d.Kind != app.DecisionBlock || d.Status != 403 {
		t.Errorf("a signed-in user without the role = %+v, want 403", d)
	}
	anon := app.WithRequest(context.Background(), httptest.NewRequest("GET", "/app/admin", nil))
	if d := gate.Decide(anon); d.Kind != app.DecisionRedirect || !strings.HasPrefix(d.URL, "/login?next=") {
		t.Errorf("an anonymous visitor = %+v, want a redirect to /login?next=", d)
	}
}
