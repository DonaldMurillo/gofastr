package auth

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// Found by the 2026-11-01 battery/auth bug bash: no eraser reached the
// in-memory stores, so after App.EraseUserData a single-node deployment on
// AllowInMemoryStores kept the user's sessions, TOTP seed, backup codes
// and outstanding reset links in memory until a restart.
func TestMemoryStoresReachedByErasure(t *testing.T) {
	ctx := context.Background()
	store := newMemoryUserStore()
	u, err := store.CreateUser(ctx, "erase-me@example.com", "h", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := store.CreateUser(ctx, "keep-me@example.com", "h", []string{"user"})
	resetTokens := NewMemoryMagicLinkTokenStore()
	twofa := NewTwoFAPlugin(TwoFAConfig{})
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: store, AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	mgr.Use(twofa)
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{BaseURL: "http://localhost", TokenStore: resetTokens, EmailSender: &stubEmailSender{}}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	for _, id := range []string{u.GetID(), other.GetID()} {
		if _, err := mgr.SessionStore().Create(ctx, id, time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := twofa.store.SetTwoFA(ctx, id, &TwoFAState{Secret: "S", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := createPurposeToken(ctx, resetTokens, purposeReset, id, time.Hour); err != nil {
			t.Fatal(err)
		}
	}

	erase := func(userID string, dryRun bool) int {
		n := 0
		for _, e := range datexport.AllStoreErasers() {
			if e.Name == mgr.memoryEraseName {
				got, err := e.Erase(ctx, userID, dryRun)
				if err != nil {
					t.Fatal(err)
				}
				n += got
			}
		}
		return n
	}
	if got := erase(u.GetID(), true); got != 3 {
		t.Fatalf("dry run counted %d items, want 3 (session, 2FA state, reset token)", got)
	}
	if got := erase(u.GetID(), false); got != 3 {
		t.Fatalf("erased %d items, want 3", got)
	}
	if got := erase(u.GetID(), false); got != 0 {
		t.Errorf("a re-run erased %d more", got)
	}
	if st, _ := twofa.store.GetTwoFA(ctx, u.GetID()); st != nil {
		t.Error("SECURITY: the erased user's 2FA seed survives")
	}
	// The other user is untouched.
	if got := erase(other.GetID(), true); got != 3 {
		t.Errorf("another user's items: %d, want 3 untouched", got)
	}
	// A stopped manager's stores leave the process-wide registry, so
	// another app's erasure of the same id never reaches them.
	name := mgr.memoryEraseName
	if err := mgr.OnStop(ctx); err != nil {
		t.Fatal(err)
	}
	for _, e := range datexport.AllStoreErasers() {
		if e.Name == name {
			t.Errorf("store eraser %q still registered after OnStop", name)
		}
	}
}
