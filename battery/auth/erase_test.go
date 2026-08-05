package auth

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

// TestAuthErasersRegistered verifies the auth-owned tables are registered for
// data erasure when this package is imported — the erase-plane mirror of
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
