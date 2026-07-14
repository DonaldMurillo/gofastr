package pluginhost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// The HIGH-3 fix: an unscoped session caller must NOT inherit every
// capability. With an empty plugin grant set, the gate default-denies.
func TestAllow_NilGrantDeniesEvenUnscopedSession(t *testing.T) {
	ctx := context.Background() // no token → session/JWT, unscoped
	for _, cap := range []string{"document:write", "upload:images", "theme:read"} {
		if Allow(ctx, nil, cap) {
			t.Errorf("empty plugin grant must default-deny %q even for an unscoped session", cap)
		}
	}
}

// A capability the plugin WAS granted passes for an unscoped session (the
// plugin grant is the ceiling and the user is authenticated).
func TestAllow_GrantedCapabilityPassesUnscopedSession(t *testing.T) {
	ctx := context.Background()
	if !Allow(ctx, []string{"document:write"}, "document:write") {
		t.Error("a granted capability should pass for an unscoped session")
	}
	// …but a sibling the plugin was NOT granted is denied.
	if Allow(ctx, []string{"document:write"}, "upload:images") {
		t.Error("an ungranted capability must be denied even for an unscoped session")
	}
}

// Wildcard grant is the explicit dev/trusted "everything" form.
func TestAllow_WildcardGrant(t *testing.T) {
	ctx := context.Background()
	if !Allow(ctx, []string{"*:*"}, "anything:goes") {
		t.Error("*:* grant should allow any capability for an unscoped session")
	}
}

// A scoped API token restricts BELOW the plugin grant (intersection).
func TestAllow_ScopedTokenIntersectsGrant(t *testing.T) {
	ctx := auth.WithTokenScopes(context.Background(), []string{"document:write"})
	granted := []string{"document:write", "upload:images"}
	if !Allow(ctx, granted, "document:write") {
		t.Error("granted ∩ token both allow document:write")
	}
	if Allow(ctx, granted, "upload:images") {
		t.Error("token lacks upload:images → denied despite plugin grant")
	}
}

// The plugin grant is the CEILING: even a *:* token cannot exceed it.
func TestAllow_PluginGrantIsCeiling(t *testing.T) {
	ctx := auth.WithTokenScopes(context.Background(), []string{"*:*"})
	if Allow(ctx, []string{"document:read"}, "document:write") {
		t.Error("plugin granted only document:read → document:write must be denied even with a *:* token")
	}
}

// Guard is the fail-closed chokepoint: denied → 403 E_CAPABILITY_DENIED, next
// never runs; allowed → next runs.
func TestGuard_BlocksDeniedRunsAllowed(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(200) })

	// Denied (empty grant).
	rec := httptest.NewRecorder()
	Guard(nil, "document:write", next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if called {
		t.Fatal("Guard must not call next when denied")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied → 403, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["error"] != "E_CAPABILITY_DENIED" {
		t.Fatalf("denied body must carry E_CAPABILITY_DENIED, got %v", body)
	}

	// Allowed (granted).
	called = false
	rec = httptest.NewRecorder()
	Guard([]string{"document:write"}, "document:write", next).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("allowed → next runs (200); called=%v code=%d", called, rec.Code)
	}
}
