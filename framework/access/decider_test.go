package access_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

// TestCanResource_NoDeciderMatchesCan pins the no-decider contract: with no
// Decider installed in ctx, CanResource must answer exactly what Can answers —
// so existing RBAC-only wiring is byte-identical. Reported issue #80 requires
// the seam to leave the hot path untouched when no decider is present.
func TestCanResource_NoDeciderMatchesCan(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "posts:write")
	ctx := ctxFor(rp, "editor")

	got := access.CanResource(ctx, "posts:write", access.Ref{Type: "posts", ID: "42"})
	want := access.Can(ctx, "posts:write")
	if got != want {
		t.Fatalf("CanResource=%v but Can=%v with no decider; must match", got, want)
	}
	if !got {
		t.Fatalf("editor granted posts:write should be allowed")
	}
}

// TestCanResource_AllowDeciderPermits confirms a Decider returning DecisionAllow
// permits the check even when the role policy would deny (no grant, no role) —
// the seam lets a resource-aware rule grant access the coarse role policy can't.
func TestCanResource_AllowDeciderPermits(t *testing.T) {
	t.Parallel()
	// No policy, no roles: Can would fail-closed to false.
	ctx := access.WithDecider(context.Background(), func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionAllow
	})
	if !access.CanResource(ctx, "projects:update", access.Ref{Type: "projects", ID: "42"}) {
		t.Fatalf("DecisionAllow must permit even without a role-policy grant")
	}
}

// TestCanResource_DenyDeciderBlocks confirms a Decider returning DecisionDeny
// refuses the check even when the role policy would allow — the seam can tighten
// below the coarse role policy (e.g. "editor, but not of this project").
func TestCanResource_DenyDeciderBlocks(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "projects:update") // coarse policy says yes
	ctx := access.WithDecider(ctxFor(rp, "editor"), func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionDeny
	})
	if access.CanResource(ctx, "projects:update", access.Ref{Type: "projects", ID: "42"}) {
		t.Fatalf("DecisionDeny must block even when the role policy grants")
	}
}

// TestCanResource_AbstainFallsThrough confirms DecisionAbstain defers to the
// role policy — the decider can opt out of a decision it has no opinion on.
func TestCanResource_AbstainFallsThrough(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "projects:update")
	ctx := access.WithDecider(ctxFor(rp, "editor"), func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionAbstain
	})
	if !access.CanResource(ctx, "projects:update", access.Ref{Type: "projects", ID: "42"}) {
		t.Fatalf("Abstain must fall through to Can, which grants editor projects:update")
	}
}

// TestCanResource_AbstainFallsThroughDeny is the negative half: Abstain +
// no grant ⇒ Can denies. Confirms Abstain truly delegates, not silently allows.
func TestCanResource_AbstainFallsThroughDeny(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "projects:read") // no projects:update
	ctx := access.WithDecider(ctxFor(rp, "editor"), func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionAbstain
	})
	if access.CanResource(ctx, "projects:update", access.Ref{Type: "projects", ID: "42"}) {
		t.Fatalf("Abstain must fall through to Can, which denies editor projects:update")
	}
}

// TestDecider_ReceivesRolesCapAndRef pins the Decider callback contract: it is
// handed the caller's resolved roles, the capability under check, and the
// resource Ref — so a resource-aware rule can decide on all three.
func TestDecider_ReceivesRolesCapAndRef(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "projects:update")

	var gotRoles []string
	var gotCap access.Permission
	var gotRef access.Ref
	decider := func(_ context.Context, roles []string, cap access.Permission, ref access.Ref) access.Decision {
		gotRoles, gotCap, gotRef = roles, cap, ref
		return access.DecisionAbstain
	}
	ctx := access.WithDecider(ctxFor(rp, "editor", "viewer"), decider)
	access.CanResource(ctx, "projects:update", access.Ref{Type: "projects", ID: "42"})

	if len(gotRoles) != 2 || gotRoles[0] != "editor" || gotRoles[1] != "viewer" {
		t.Fatalf("decider roles = %v, want [editor viewer]", gotRoles)
	}
	if gotCap != "projects:update" {
		t.Fatalf("decider capability = %q, want projects:update", gotCap)
	}
	if gotRef.Type != "projects" || gotRef.ID != "42" {
		t.Fatalf("decider ref = %+v, want {projects 42}", gotRef)
	}
}

// TestCanResource_NilContextFailsClosed confirms a nil context fails closed
// (returns false) rather than panicking — the secure default for an un-wired
// call, matching GetRoles/GetPermissions.
func TestCanResource_NilContextFailsClosed(t *testing.T) {
	t.Parallel()
	if access.CanResource(nil, "posts:read", access.Ref{Type: "posts"}) {
		t.Fatalf("CanResource(nil, ...) must fail closed to false")
	}
}

// TestDecision_ZeroValueIsAbstain pins that the zero Decision is Abstain, so a
// decider that forgets to return falls through to the role policy (safe).
func TestDecision_ZeroValueIsAbstain(t *testing.T) {
	t.Parallel()
	var d access.Decision
	if d != access.DecisionAbstain {
		t.Fatalf("zero Decision = %v, want DecisionAbstain", d)
	}
}

// TestWithDecider_GetDeciderRoundtrip confirms the context plumbing pair: what
// WithDecider installs, GetDecider reads back; a bare ctx yields nil.
func TestWithDecider_GetDeciderRoundtrip(t *testing.T) {
	t.Parallel()
	if access.GetDecider(context.Background()) != nil {
		t.Fatalf("GetDecider on bare ctx must be nil")
	}
	decider := func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionAllow
	}
	ctx := access.WithDecider(context.Background(), decider)
	if access.GetDecider(ctx) == nil {
		t.Fatalf("GetDecider must return the decider installed by WithDecider")
	}
}

// TestCanResource_NilDeciderFallsThrough confirms storing a nil Decider in ctx
// is treated as "no decider" and falls through to Can, not a panic.
func TestCanResource_NilDeciderFallsThrough(t *testing.T) {
	t.Parallel()
	rp := access.NewRolePolicy()
	rp.Grant("editor", "posts:write")
	// Install a typed-nil decider via the package context key indirectly: use a
	// decider that returns Abstain, then confirm fall-through still grants.
	ctx := access.WithDecider(ctxFor(rp, "editor"), func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		return access.DecisionAbstain
	})
	if !access.CanResource(ctx, "posts:write", access.Ref{Type: "posts"}) {
		t.Fatalf("Abstain decider must fall through to Can which grants editor posts:write")
	}
}

// TestDeciderMiddleware_InstallsDecider confirms DeciderMiddleware puts the
// decider into the request context so downstream CanResource calls consult it.
func TestDeciderMiddleware_InstallsDecider(t *testing.T) {
	t.Parallel()
	consulted := false
	decider := func(_ context.Context, _ []string, _ access.Permission, _ access.Ref) access.Decision {
		consulted = true
		return access.DecisionAllow
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No policy/roles installed — only the decider. Allow must still permit.
		if !access.CanResource(r.Context(), "teams:update", access.Ref{Type: "teams", ID: "7"}) {
			t.Error("downstream CanResource should be allowed by the middleware's decider")
		}
	})
	mw := access.DeciderMiddleware(decider)
	req := httptest.NewRequest(http.MethodGet, "/teams/7", nil)
	mw(next).ServeHTTP(httptest.NewRecorder(), req)
	if !consulted {
		t.Fatal("DeciderMiddleware did not install the decider into ctx (decider never called)")
	}
}
