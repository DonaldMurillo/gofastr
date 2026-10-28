package auth

import (
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

// TestAuthExportersRegistered verifies the auth-owned tables are registered for
// data export when this package is imported.
func TestAuthExportersRegistered(t *testing.T) {
	want := map[string]bool{"auth_users": false, "auth_sessions": false, "auth_passkeys": false}
	for _, ex := range datexport.All() {
		if _, ok := want[ex.Name]; ok && ex.Source == "auth" {
			want[ex.Name] = true
			if ex.PrimaryKey != "id" {
				t.Errorf("%s primary key = %q, want id", ex.Name, ex.PrimaryKey)
			}
			if len(ex.Columns) == 0 {
				t.Errorf("%s has no columns", ex.Name)
			}
		}
	}
	for name, saw := range want {
		if !saw {
			t.Errorf("%s exporter not registered", name)
		}
	}
}

// The passkey exporter must name exactly the columns the store writes.
func TestPasskeyExportMatchesStore(t *testing.T) {
	want := strings.Split(passkeyColumns, ", ")
	for _, ex := range datexport.All() {
		if ex.Name == "auth_passkeys" && ex.Source == "auth" {
			if !slices.Equal(ex.Columns, want) {
				t.Errorf("auth_passkeys exporter columns = %v, want %v", ex.Columns, want)
			}
			return
		}
	}
	t.Fatal("auth_passkeys exporter not registered")
}
