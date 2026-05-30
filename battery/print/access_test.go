package print

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

func TestDefaultRequiresAuth(t *testing.T) {
	built := false
	b := New(Config{}).Document(Document{ // DefaultAccess defaults to RequireAuth
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) {
			built = true
			return stubDoc{html: "x"}, nil
		},
	})
	rec := get(t, mount(t, b), "/print/doc") // no user injected
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if built {
		t.Errorf("Build must not run for unauthorized request")
	}
}

func TestPublicAllowsAnon(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("<p>ok</p>"),
	})
	rec := get(t, mount(t, b), "/print/doc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestOwnerMismatchForbidden(t *testing.T) {
	built := false
	b := New(Config{}).Document(Document{
		Name: "doc", Path: "/doc",
		Access: RequireOwner(func(*http.Request, any) bool { return false }),
		Build: func(*http.Request) (component.Component, error) {
			built = true
			return stubDoc{html: "x"}, nil
		},
	})
	// authed (so we pass the auth check) but ownership fails.
	rec := get(t, authed(mount(t, b)), "/print/doc")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if built {
		t.Errorf("Build must not run when ownership denied")
	}
}
