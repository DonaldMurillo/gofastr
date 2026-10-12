package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A policy callback that panics fails closed and alone. Authorize
// refuses the request the way a false answer does; a page's Access hides
// that page from the sidebar and the palette and refuses its route while
// the rest of the admin draws; EffectiveRoles falls back to the direct
// roles.

func TestPanickingAuthorizeRefuses(t *testing.T) {
	refused := setup(t, nil, Config{Authorize: func(context.Context) bool { return false }}, nil)
	want := get(refused.as(theAdmin), "/admin").Code
	x := setup(t, nil, Config{Authorize: func(context.Context) bool { panic("policy store down") }}, nil)
	rr := get(x.as(theAdmin), "/admin")
	if rr.Code != want || want == http.StatusOK {
		t.Fatalf("SECURITY: a panicking Authorize answered %d, a refusing one %d", rr.Code, want)
	}
}

func TestPanickingPageAccessHidesOnlyThatPage(t *testing.T) {
	bad := page("/billing", "Billing", says("money"))
	bad.Nav = &entity.EntityNav{}
	bad.Access = func(context.Context) bool { panic("policy store down") }
	good := page("/reports", "Reports", says("report body"))
	good.Nav = &entity.EntityNav{}
	x := setup(t, nil, Config{Pages: []Page{bad, good}}, nil)
	rr := get(x.as(theAdmin), "/admin")
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, `href="/admin/reports"`) {
		t.Fatalf("one page's panicking Access broke the dashboard: %d\n%s", rr.Code, body)
	}
	if strings.Contains(body, `href="/admin/billing"`) {
		t.Error("the sidebar links a page whose Access panicked")
	}
	if rr := get(x.as(theAdmin), "/admin/billing"); rr.Code != http.StatusForbidden || strings.Contains(rr.Body.String(), "money") {
		t.Errorf("SECURITY: a page whose Access panicked answered %d", rr.Code)
	}
	pal := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {""}})
	if pal.Code != http.StatusOK || strings.Contains(pal.Body.String(), "/admin/billing") || !strings.Contains(pal.Body.String(), "/admin/reports") {
		t.Errorf("the palette = %d, with the panicking page or without the good one:\n%s", pal.Code, pal.Body.String())
	}
}

func TestPanickingEffectiveRolesShowsDirectRoles(t *testing.T) {
	r := newRBACEnv(t, Config{EffectiveRoles: func(context.Context, string) []access.RoleWithOrigin { panic("resolver down") }})
	rr := get(r.as(theAdmin), "/admin/rbac/users")
	body := rr.Body.String()
	if rr.Code != http.StatusOK || !strings.Contains(body, "editor@example.com") || !strings.Contains(body, `<span class="fui-tag__label" data-cui-internal="">editor</span>`) {
		t.Fatalf("a panicking EffectiveRoles broke the users page: %d\n%s", rr.Code, body)
	}
}
