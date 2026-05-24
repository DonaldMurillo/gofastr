package auth

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// TestAuthInitRegistersOwnerExtractor pins the load-bearing wiring fix:
// importing battery/auth MUST register an owner extractor in
// framework/owner that returns the current user's GetID() value. Without
// this test, every owner-scope test in framework/crud uses its own
// in-test extractor (installOwnerExtractor) — a regression that changes
// the extractor to return the wrong field (e.g. SessionID instead of
// ID) would silently break OwnerField scoping in every production app
// while every unit test continues to pass.
func TestAuthInitRegistersOwnerExtractor(t *testing.T) {
	// Plain context: no user. The extractor should report "no owner."
	id, ok := owner.Get(context.Background())
	if ok {
		t.Fatalf("extractor reported owner on empty ctx (got id=%v)", id)
	}

	// With a real auth.User in ctx, the extractor must return the
	// user's GetID() value.
	user := &BasicUser{ID: "real-user-id-42", Email: "u@example.com", Roles: []string{"user"}}
	ctx := handler.SetUser(context.Background(), user)

	gotID, ok := owner.Get(ctx)
	if !ok {
		t.Fatal("extractor returned ok=false when ctx has a real User — battery/auth's init() registration broken")
	}
	if gotID != "real-user-id-42" {
		t.Errorf("extractor returned %v, want %q (must echo User.GetID(), not a different field)",
			gotID, "real-user-id-42")
	}
}

// TestAuthExtractorIgnoresUnknownCtxType confirms the extractor
// gracefully reports "no owner" when ctx contains something that
// isn't an auth.User — e.g. a raw map placed by a custom middleware.
func TestAuthExtractorIgnoresUnknownCtxType(t *testing.T) {
	ctx := handler.SetUser(context.Background(), map[string]string{"id": "fake"})
	_, ok := owner.Get(ctx)
	if ok {
		t.Errorf("extractor returned ok=true for non-User ctx value — type-assertion missing")
	}
}

// TestAuthExtractorRefusesEmptyID — a User implementation that returns
// "" from GetID() must NOT count as a valid owner. Otherwise an
// attacker-controlled user model returning "" leaks all "" rows in the
// DB (or breaks the scope query entirely).
func TestAuthExtractorRefusesEmptyID(t *testing.T) {
	empty := &BasicUser{ID: "", Email: "noid@example.com"}
	ctx := handler.SetUser(context.Background(), empty)
	_, ok := owner.Get(ctx)
	if ok {
		t.Errorf("extractor accepted empty GetID() — should return ok=false")
	}
}
