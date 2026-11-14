package telemetryadmin_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/battery/telemetry/telemetryadmin"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// The four pages sit under the admin's prefix, in one sidebar group, and
// link to each other there: a page built for "/back" must not point at
// "/admin".
func TestPagesSitUnderTheAdminPrefix(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir()})
	pages := telemetryadmin.Pages(tel, "/back/")
	if len(pages) != 4 || pages[0].Path != "/telemetry/overview" || pages[1].Path != "/telemetry/requests" || pages[2].Path != "/telemetry/issues" || pages[3].Path != "/telemetry/logs" {
		t.Fatalf("pages: %+v", pages)
	}
	for _, p := range pages {
		if p.Nav == nil || p.Nav.Group != telemetryadmin.NavGroup || p.Nav.Icon == "" || p.Title.Text(context.Background()) == "" {
			t.Errorf("%s: nav %+v, title %q", p.Path, p.Nav, p.Title.Text(context.Background()))
		}
		r := httptest.NewRequest("GET", "/back"+p.Path, nil)
		c, err := p.Build(r)
		if err != nil || c == nil {
			t.Fatalf("%s: Build = %v, %v", p.Path, c, err)
		}
		// Before the plugin starts, a page says so instead of failing.
		html, _ := component.SafeRenderCtx(appui.WithRequest(r.Context(), r), c)
		if !strings.Contains(string(html), "Telemetry is not running yet") {
			t.Errorf("%s before Init: %s", p.Path, html)
		}
		if strings.Contains(string(html), `href="/admin`) {
			t.Errorf("%s links outside its prefix: %s", p.Path, html)
		}
	}
}

// The files hold every tenant's requests and log lines together, so
// every page (and with it the page's part route, which asks the same
// Access) refuses a caller scoped to a tenant, as the admin's own
// tenant-less pages do.
func TestPagesRefuseATenantScopedCaller(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir()})
	for _, p := range telemetryadmin.Pages(tel, "/admin") {
		if p.Access == nil {
			t.Fatalf("SECURITY: %s admits every admin, a tenant's included", p.Path)
		}
		if p.Access(tenant.SetTenantID(context.Background(), "t1")) {
			t.Errorf("SECURITY: %s admits a caller scoped to a tenant", p.Path)
		}
		if !p.Access(context.Background()) {
			t.Errorf("%s refuses the platform operator", p.Path)
		}
	}
}

// Requests and Logs name the view for the browser tab.
func TestPagesNameTheView(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir()})
	for _, p := range telemetryadmin.Pages(tel, "/admin") {
		for query, want := range map[string]map[string]string{
			"/telemetry/requests": {"?show=failed": "Failed requests", "": "Requests"},
			"/telemetry/logs":     {"?q=card": "Logs: card", "": "Logs"},
		}[p.Path] {
			r := httptest.NewRequest("GET", "/admin"+p.Path+query, nil)
			if p.TabTitle == nil {
				t.Fatalf("%s has no TabTitle", p.Path)
			}
			if got := p.TabTitle(appui.WithRequest(r.Context(), r)); got != want {
				t.Errorf("%s%s: tab title %q, want %q", p.Path, query, got, want)
			}
		}
	}
}
