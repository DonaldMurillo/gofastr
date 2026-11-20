// Package telemetryadmin puts the telemetry screens in the admin's sidebar.
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

// Pages returns the Overview, Requests, Issues and Logs pages for tel.
func Pages(tel *telemetry.Plugin, adminPrefix string) []admin.Page {
	prefix := strings.TrimRight(adminPrefix, "/")
	screens := telemetryui.NewScreens(tel, prefix+"/telemetry", false)
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

func platformOnly(ctx context.Context) bool { return tenant.GetTenantID(ctx) == "" }

type page struct {
	component.ContextOnly
	draw func(context.Context) render.HTML
}

func (p page) RenderCtx(ctx context.Context) render.HTML { return p.draw(ctx) }
