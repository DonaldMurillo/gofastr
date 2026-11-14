// Package telemetryadmin puts battery/telemetry's request and log
// screens in the admin's sidebar, behind the admin's own gate:
//
//	admin.New(admin.Config{Pages: telemetryadmin.Pages(tel, "/admin")})
//
// It is a package of its own so that neither battery links the other
// for an app that uses only one.
//
// Experimental, with battery/telemetry.
package telemetryadmin

import (
	"context"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/battery/telemetry/telemetryui"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// NavGroup is the sidebar group the pages sit in.
const NavGroup = "telemetry"

// Pages returns the Overview, Requests, Issues and Logs pages for tel. adminPrefix is the
// admin's Config.PathPrefix ("/admin"): the pages link to each other
// under it.
func Pages(tel *telemetry.Plugin, adminPrefix string) []admin.Page {
	prefix := strings.TrimRight(adminPrefix, "/")
	screens := telemetryui.NewScreens(tel, prefix+"/telemetry", false)
	// The newest page of each list re-fetches its rows from the admin's
	// page-part route, which the admin gates as it gates the page.
	screens.Live = func(list string) string { return admin.PagePartPath(prefix, "/telemetry/"+list) }
	build := func(draw func(context.Context) render.HTML) func(*http.Request) (component.Component, error) {
		return func(*http.Request) (component.Component, error) { return page{draw: draw}, nil }
	}
	pages := []admin.Page{
		{Path: "/telemetry/overview", Title: i18nui.TelemetryOverview,
			Nav: &entity.EntityNav{Group: NavGroup, Icon: "bar-chart", Order: 5}, Build: build(screens.Overview), Part: build(screens.OverviewRows)},
		{Path: "/telemetry/requests", Title: i18nui.TelemetryRequests, TabTitle: screens.RequestsTitle,
			Nav: &entity.EntityNav{Group: NavGroup, Icon: "activity", Order: 10}, Build: build(screens.Requests), Part: build(screens.RequestsRows)},
		{Path: "/telemetry/issues", Title: i18nui.TelemetryIssues,
			Nav: &entity.EntityNav{Group: NavGroup, Icon: "alert-triangle", Order: 15}, Build: build(screens.Issues)},
		{Path: "/telemetry/logs", Title: i18nui.TelemetryLogs, TabTitle: screens.LogsTitle,
			Nav: &entity.EntityNav{Group: NavGroup, Icon: "list", Order: 20}, Build: build(screens.Logs), Part: build(screens.LogsRows)},
	}
	for i := range pages {
		pages[i].Access = platformOnly
	}
	return pages
}

// platformOnly admits a caller with no tenant. The files hold every
// tenant's request paths, client addresses, errors and log lines in
// one directory, with nothing to tell one tenant's from another's: an
// admin scoped to a tenant would read all of them. The admin refuses
// the same callers on its own tenant-less pages (the queue, roles).
func platformOnly(ctx context.Context) bool { return tenant.GetTenantID(ctx) == "" }

type page struct {
	component.ContextOnly
	draw func(context.Context) render.HTML
}

func (p page) RenderCtx(ctx context.Context) render.HTML { return p.draw(ctx) }
