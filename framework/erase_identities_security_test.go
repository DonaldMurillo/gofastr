package framework

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// A kept identity lives at most keptIdentitiesFor: an expired row must
// not outlive the next erasure just because that erasure succeeded
// (primitives app.privacy keep-identities-until-the-stores-erase).
func TestExpiredKeptIdentityIsSwept(t *testing.T) {
	app, db := newIdentityEraseApp(t)
	defer db.Close()
	createMagicLinkTables(t, db)
	ctx := context.Background()
	registerMagicLinkEraseScenario(t)
	_, _ = datexport.RegisterStoreEraser(datexport.StoreEraser{Name: "late", Source: "test",
		EraseResolved: func(context.Context, datexport.ErasedUser, bool) (int, error) { return 0, nil }})
	if err := app.keepIdentities(ctx, "", datexport.ErasedUser{ID: "u1", Identities: map[datexport.IdentityKind]string{datexport.IdentityEmail: "u1@x.test"}}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-keptIdentitiesFor - time.Hour).Unix()
	if _, err := db.ExecContext(ctx, `UPDATE `+eraseIdentitiesTable+` SET kept_at = ?`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := app.EraseUserData(ctx, "u2"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+eraseIdentitiesTable+` WHERE value = 'u1@x.test'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("an expired kept email is still stored in plaintext after a later successful erasure")
	}
}
