package admin

import (
	"context"
	"github.com/DonaldMurillo/gofastr/framework/embed"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// roleUser is a stand-in authenticated user carrying roles, matching the
// structural interface (GetRoles) the admin default-authorizer checks.
type roleUser struct{ roles []string }

func (u roleUser) GetRoles() []string { return u.roles }

func adminReq(h http.Handler, user any) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), user))
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// TestAdmin_NonAdminUserForbidden pins the secure-by-default contract: an
// authenticated user WITHOUT the admin role is refused with 403. Before B3 the
// default authorizer accepted any non-nil user, so a freshly-registered reader
// got full admin CRUD over every entity.
func TestAdmin_NonAdminUserForbidden(t *testing.T) {
	h := mountAdminBare(t, Config{})
	rr := adminReq(h, roleUser{roles: []string{"reader"}})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin /admin = %d, want 403", rr.Code)
	}
}

// TestAdmin_RolelessUserForbidden confirms a user that can't prove a role
// (no GetRoles) is denied — fail closed.
func TestAdmin_RolelessUserForbidden(t *testing.T) {
	h := mountAdminBare(t, Config{})
	rr := adminReq(h, struct{}{})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("roleless /admin = %d, want 403", rr.Code)
	}
}

// TestAdmin_AdminRoleAllowed confirms the happy path: a user with the admin
// role reaches the admin overview.
func TestAdmin_AdminRoleAllowed(t *testing.T) {
	h := mountAdminBare(t, Config{})
	rr := adminReq(h, roleUser{roles: []string{"admin"}})
	if rr.Code != http.StatusOK {
		t.Fatalf("admin /admin = %d, want 200. body=%s", rr.Code, rr.Body.String())
	}
}

// Round 4 added an embed-grant refusal to authorized() and shipped it without a
// test; round 5 then found it sat below the custom-Authorize early return. Both
// directions are pinned here.
func TestAuthorizedRefusesAnEmbedGrant(t *testing.T) {
	admin := roleUser{roles: []string{"admin"}}
	grantCtx := embed.WithGrant(handler.SetUser(context.Background(), admin), embed.Grant{
		Surface: "reports", Subject: "root", Scopes: []string{"reports:read"},
		Origin: "https://acme.com",
	})

	b := &Battery{}
	if b.authorized(grantCtx) {
		t.Error("an embed grant reached the admin back office; past this gate every " +
			"route runs under a wildcard access policy")
	}
	// And still below a custom Authorize hook, which is where it was skipped.
	b2 := &Battery{cfg: Config{Authorize: func(context.Context) bool { return true }}}
	if b2.authorized(grantCtx) {
		t.Error("a custom Authorize hook bypassed the embed refusal")
	}
	// An ordinary admin session still gets in.
	if !(&Battery{}).authorized(handler.SetUser(context.Background(), admin)) {
		t.Error("an ordinary admin session was refused")
	}
}
