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
)

// The two pages sit under the admin's prefix, in one sidebar group, and
// link to each other there: a page built for "/back" must not point at
// "/admin".
func TestPagesSitUnderTheAdminPrefix(t *testing.T) {
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir()})
	pages := telemetryadmin.Pages(tel, "/back/")
	if len(pages) != 2 || pages[0].Path != "/telemetry/requests" || pages[1].Path != "/telemetry/logs" {
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
