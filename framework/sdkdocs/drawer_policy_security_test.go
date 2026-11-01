package sdkdocs

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The section-menu drawer lists every included entity. Its /chrome and
// /state routes sat outside Config.Policy, so an anonymous GET under a
// denying policy read the entity list the screens refuse.
func TestPolicyGatesSectionDrawer(t *testing.T) {
	reg := testRegistry()
	deny := app.PolicyFunc(func(context.Context) app.Decision {
		return decide.Block(http.StatusForbidden)
	})
	srv := mountedServer(t, Config{Registry: reg, IncludeGated: true, Policy: deny})
	for _, path := range []string{
		"/core-ui/widget/" + drawerName + "/chrome",
		"/core-ui/widget/" + drawerName + "/state",
	} {
		resp, body := get(t, srv, path)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", path, resp.StatusCode)
		}
		if strings.Contains(body, "invoices") || strings.Contains(body, "/entities/") {
			t.Errorf("%s leaked the entity list under a denying policy:\n%s", path, body)
		}
	}
}

// A redirecting policy refuses the drawer's fetches instead of sending
// the runtime to the sign-in page, which it would paste into the sheet.
func TestRedirectPolicyRefusesDrawer(t *testing.T) {
	login := app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionRedirect, URL: "/login"}
	})
	srv := mountedServer(t, Config{Registry: testRegistry(), IncludeGated: true, Policy: login})
	srv.Client().CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, body := get(t, srv, "/core-ui/widget/"+drawerName+"/chrome")
	if resp.StatusCode != http.StatusForbidden || strings.Contains(body, "invoices") {
		t.Fatalf("status %d; want 403 with no menu:\n%s", resp.StatusCode, body)
	}
	resp, _ = get(t, srv, "/docs/api/sdk/go.zip")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("download status %d; want 303 to sign-in", resp.StatusCode)
	}
}

// The drawer built its menu once, at Mount, so an entity registered after
// Mount was on the screens' menu and never in the drawer.
func TestDrawerListsLateEntities(t *testing.T) {
	reg := testRegistry()
	srv := mountedServer(t, Config{Registry: reg, IncludeGated: true})
	reg.entities = append(reg.entities, entity.Define("refunds", entity.EntityConfig{
		Fields: []schema.Field{{Name: "amount", Type: schema.Decimal}},
	}))
	_, page := get(t, srv, "/docs/api")
	if !strings.Contains(page, "/entities/refunds") {
		t.Fatalf("setup broken: the screen menu does not list the late entity")
	}
	resp, body := get(t, srv, "/core-ui/widget/"+drawerName+"/chrome")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "/entities/refunds") {
		t.Fatalf("drawer (status %d) misses an entity registered after Mount:\n%s", resp.StatusCode, body)
	}
}

// Without a Policy the drawer still serves its menu.
func TestDrawerServesWithoutPolicy(t *testing.T) {
	srv := mountedServer(t, Config{Registry: testRegistry(), IncludeGated: true})
	resp, body := get(t, srv, "/core-ui/widget/"+drawerName+"/chrome")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "/entities/invoices") {
		t.Fatalf("status %d; want 200 with the menu:\n%s", resp.StatusCode, body)
	}
}
