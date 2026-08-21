package auth

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

// TestAuthErasersRegistered verifies the auth-owned tables are registered for
// data erasure when this package is imported, the erase-plane mirror of
// TestAuthExportersRegistered.
func TestAuthErasersRegistered(t *testing.T) {
	want := map[string]bool{"auth_users": false, "auth_sessions": false}
	for _, e := range datexport.AllErasers() {
		if _, ok := want[e.Name]; ok && e.Source == "auth" {
			want[e.Name] = true
			if e.Mode != datexport.EraseDelete {
				t.Errorf("%s mode = %v, want EraseDelete", e.Name, e.Mode)
			}
			if e.Column == "" {
				t.Errorf("%s has no match column", e.Name)
			}
		}
	}
	wantCol := map[string]string{"auth_sessions": "user_id", "auth_users": "id"}
	for _, e := range datexport.AllErasers() {
		if c, ok := wantCol[e.Name]; ok && e.Source == "auth" && e.Column != c {
			t.Errorf("%s column = %q, want %q", e.Name, e.Column, c)
		}
	}
	for name, saw := range want {
		if !saw {
			t.Errorf("%s eraser not registered", name)
		}
	}
}

// Every auth table that holds per-user state must be reachable by
// App.EraseUserData. A table left unregistered survives an erasure,
// 2FA secrets and OAuth links are credential material, not metadata.
func TestAuthErasersCoverCredentialTables(t *testing.T) {
	want := map[string]string{
		"auth_sessions":     "user_id",
		"auth_users":        "id",
		"auth_twofa":        "user_id",
		"users_oauth_links": "user_id",
	}
	got := map[string]string{}
	for _, e := range datexport.AllErasers() {
		if e.Source == "auth" {
			got[e.Table] = e.Column
		}
	}
	for table, col := range want {
		if got[table] != col {
			t.Errorf("auth eraser for %q: got column %q, want %q", table, got[table], col)
		}
	}
}

// magic_link_tokens is keyed by EMAIL, not user id, so a plain user-id eraser
// cannot reach it. battery/auth closes the gap declaratively: it registers an
// IdentityEmail resolver (auth_users.id → email) and a magic_link_tokens eraser
// that declares IdentityEmail. App.EraseUserData then resolves the email at
// erase time and deletes the user's outstanding tokens, so a pre-erasure magic
// link can no longer re-create the erased account.
func TestAuthErasersCoverMagicLinkTokens(t *testing.T) {
	// The email identity resolver is registered against the user table.
	r, ok := datexport.ResolveIdentity(datexport.IdentityEmail)
	if !ok {
		t.Fatal("IdentityEmail resolver not registered")
	}
	if r.Table != "auth_users" || r.IDColumn != "id" || r.ValueColumn != "email" {
		t.Errorf("IdentityEmail resolver = %+v, want {Table:auth_users, IDColumn:id, ValueColumn:email}", r)
	}

	// The magic-link token eraser declares the email identity (email column).
	var ml *datexport.DataEraser
	for _, e := range datexport.AllErasers() {
		if e.Name == "magic_link_tokens" && e.Source == "auth" {
			cp := e
			ml = &cp
		}
	}
	if ml == nil {
		t.Fatal("magic_link_tokens eraser not registered")
	}
	if ml.Table != "magic_link_tokens" || ml.Column != "email" {
		t.Errorf("magic_link_tokens = {Table:%q, Column:%q}, want {magic_link_tokens, email}", ml.Table, ml.Column)
	}
	if ml.Mode != datexport.EraseDelete {
		t.Errorf("magic_link_tokens mode = %v, want EraseDelete", ml.Mode)
	}
	if ml.Identity != datexport.IdentityEmail {
		t.Errorf("magic_link_tokens Identity = %v, want IdentityEmail", ml.Identity)
	}
}
