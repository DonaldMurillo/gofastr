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
	// Email-keyed tokens, as the send handlers write them (canonical).
	for _, email := range []string{"erase-me@example.com", "keep-me@example.com"} {
		for _, p := range []tokenPurpose{purposeMagicLink, purposeEmailCode} {
			if _, err := createPurposeToken(ctx, resetTokens, p, email, time.Hour); err != nil {
				t.Fatal(err)
			}
		}
	}

	// The memory user store is outside the SQL erasure, so no identity
	// resolves and the eraser reads the email from the user store.
	erase := func(userID string, dryRun bool) int {
		n := 0
		for _, e := range datexport.AllStoreErasers() {
			if e.Name == mgr.memoryEraseName {
				got, err := e.EraseResolved(ctx, datexport.ErasedUser{ID: userID}, dryRun)
				if err != nil {
					t.Fatal(err)
				}
				n += got
			}
		}
		return n
	}
	if got := erase(u.GetID(), true); got != 5 {
		t.Fatalf("dry run counted %d items, want 5 (session, 2FA state, reset, magic-link and email-code tokens)", got)
	}
	if got := erase(u.GetID(), false); got != 5 {
		t.Fatalf("erased %d items, want 5", got)
	}
	if got := erase(u.GetID(), false); got != 0 {
		t.Errorf("a re-run erased %d more", got)
	}
	if st, _ := twofa.store.GetTwoFA(ctx, u.GetID()); st != nil {
		t.Error("SECURITY: the erased user's 2FA seed survives")
	}
	// The other user is untouched.
	if got := erase(other.GetID(), true); got != 5 {
		t.Errorf("another user's items: %d, want 5 untouched", got)
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

// Under App.EraseUserData the SQL part deletes the user row before store
// erasers run, so the in-memory token store takes the email the erasure
// resolved beforehand. The resolver has already canonicalized it (a row
// stored as "Mixed@..." resolves to "mixed@..."; the full path is
// TestEraseReachesEmailKeyedTokensFromLegacyRow), and it is used as is.
func TestMemoryTokenStoreErasedByResolvedEmail(t *testing.T) {
	ctx := context.Background()
	tokens := NewMemoryMagicLinkTokenStore()
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{TokenStore: tokens, DevMode: true}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	for _, email := range []string{"mixed@example.com", "keep@example.com"} {
		for _, p := range []tokenPurpose{purposeMagicLink, purposeEmailCode} {
			if _, err := createPurposeToken(ctx, tokens, p, email, time.Hour); err != nil {
				t.Fatal(err)
			}
		}
	}
	var eraser datexport.StoreEraser
	for _, e := range datexport.AllStoreErasers() {
		if e.Name == mgr.memoryEraseName {
			eraser = e
		}
	}
	gone := datexport.ErasedUser{ID: "u-gone", Identities: map[datexport.IdentityKind]string{datexport.IdentityEmail: "mixed@example.com"}}
	if n, err := eraser.EraseResolved(ctx, gone, false); err != nil || n != 2 {
		t.Fatalf("erased %d, %v; want the two tokens for mixed@example.com", n, err)
	}
	for _, p := range []tokenPurpose{purposeMagicLink, purposeEmailCode} {
		if n := countMatching(tokens.tokens, func(e *magicLinkEntry) bool { return e.email == string(p)+":keep@example.com" }); n != 1 {
			t.Errorf("another address's %s token: %d, want 1", p, n)
		}
	}
}

// The emailed-code plugin's own in-memory store is erased too, not only a
// store it happens to share with the magic-link plugin.
func TestMemoryEmailCodeStoreErased(t *testing.T) {
	ctx := context.Background()
	codes := NewMemoryMagicLinkTokenStore()
	mgr := New(AuthConfig{JWTSecret: "x", DevMode: true, UserStore: newMemoryUserStore(), AllowInMemoryStores: true})
	mgr.Use(NewEmailCodePlugin(EmailCodeConfig{TokenStore: codes, DevMode: true}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	for _, email := range []string{"gone@example.com", "keep@example.com"} {
		if _, err := createPurposeToken(ctx, codes, purposeEmailCode, email, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	var eraser *datexport.StoreEraser
	for _, e := range datexport.AllStoreErasers() {
		if e.Name == mgr.memoryEraseName {
			eraser = &e
		}
	}
	if eraser == nil {
		t.Fatal("no in-memory store eraser registered for the email-code store")
	}
	gone := datexport.ErasedUser{ID: "u-gone", Identities: map[datexport.IdentityKind]string{datexport.IdentityEmail: "gone@example.com"}}
	if n, err := eraser.EraseResolved(ctx, gone, true); err != nil || n != 1 {
		t.Fatalf("dry run counted %d, %v; want 1", n, err)
	}
	if n, err := eraser.EraseResolved(ctx, gone, false); err != nil || n != 1 {
		t.Fatalf("erased %d, %v; want 1", n, err)
	}
	if n := countMatching(codes.tokens, func(e *magicLinkEntry) bool { return e.email == "emailcode:gone@example.com" }); n != 0 {
		t.Errorf("the erased address's code survived: %d", n)
	}
	if n := countMatching(codes.tokens, func(e *magicLinkEntry) bool { return e.email == "emailcode:keep@example.com" }); n != 1 {
		t.Errorf("another address's code: %d, want 1", n)
	}
}
