package crud

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// signedIn returns a context carrying user id as the owner extractor sees it.
func signedIn(id string) context.Context {
	return handler.SetUser(context.Background(), &testUser{id: id})
}

// TestCrossOwner_ListSpansOwners proves the privileged-read escape lets a
// Go-level caller read across owners, while the default stays scoped.
func TestCrossOwner_ListSpansOwners(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)
	ctx := signedIn("alice")

	scoped, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("scoped ListAll: %v", err)
	}
	if len(scoped) != 1 {
		t.Fatalf("default ListAll returned %d rows, want 1 (alice-only)", len(scoped))
	}

	all, err := ch.ListAll(owner.AllowCrossOwner(ctx), ListOptions{})
	if err != nil {
		t.Fatalf("cross-owner ListAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("cross-owner ListAll returned %d rows, want 2 (all owners)", len(all))
	}
}

// TestCrossOwner_CountSpansOwners proves CountAll honors the escape, the
// eval's "spots remaining = capacity - COUNT(all bookings)" case.
func TestCrossOwner_CountSpansOwners(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)
	ctx := signedIn("alice")

	scoped, err := ch.CountAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("scoped CountAll: %v", err)
	}
	if scoped != 1 {
		t.Fatalf("default CountAll = %d, want 1 (alice-only)", scoped)
	}

	all, err := ch.CountAll(owner.AllowCrossOwner(ctx), ListOptions{})
	if err != nil {
		t.Fatalf("cross-owner CountAll: %v", err)
	}
	if all != 2 {
		t.Fatalf("cross-owner CountAll = %d, want 2 (all owners)", all)
	}
}

// TestCrossOwner_GetOtherOwnersRow proves GetOne can read another owner's
// row under the escape, and is scoped out without it.
func TestCrossOwner_GetOtherOwnersRow(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)
	ctx := signedIn("alice") // alice asking for bob's row

	if _, err := ch.GetOne(ctx, "note-b", nil); !errors.Is(err, errNotFound) {
		t.Fatalf("scoped GetOne of another owner's row err=%v, want errNotFound", err)
	}

	row, err := ch.GetOne(owner.AllowCrossOwner(ctx), "note-b", nil)
	if err != nil {
		t.Fatalf("cross-owner GetOne: %v", err)
	}
	if row["title"] != "Beta" {
		t.Fatalf("cross-owner GetOne title=%v, want Beta", row["title"])
	}
}

// TestCrossOwner_AnonymousStillAllowed confirms the escape lifts the owner
// requirement even with no user in context (background jobs / scripts).
func TestCrossOwner_AnonymousStillAllowed(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)
	ctx := owner.AllowCrossOwner(context.Background()) // no user at all

	rows, err := ch.ListAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("cross-owner anonymous ListAll: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("cross-owner anonymous ListAll returned %d rows, want 2", len(rows))
	}
}

// TestCrossOwner_HTTPCannotBypass proves the HTTP surface has no path to the
// escape: even crafting client input, the real List() handler stays scoped
// to the authenticated owner. The marker is set only by a deliberate Go call
// (owner.AllowCrossOwner) whose context key is unexported and never derived
// from a request.
func TestCrossOwner_HTTPCannotBypass(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerReadInProcHandler(t)
	assertCrossOwnerHTTPNotGranted(t, ch, "alice", "/api/onotes",
		[][2]string{{"X-Cross-Owner", "true"}, {"X-All-Owners", "true"}},
		"/api/onotes?all_owners=true&cross_owner=1",
		"note-a", "note-b", "Beta")
}
