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
)

// NavGroup is the sidebar group the two pages sit in.
const NavGroup = "telemetry"

// Pages returns the Requests and Logs pages for tel. adminPrefix is the
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
	return []admin.Page{
		{Path: "/telemetry/requests", Title: i18nui.TelemetryRequests,
			Nav: &entity.EntityNav{Group: NavGroup, Icon: "activity", Order: 10}, Build: build(screens.Requests), Part: build(screens.RequestsRows)},
		{Path: "/telemetry/logs", Title: i18nui.TelemetryLogs,
			Nav: &entity.EntityNav{Group: NavGroup, Icon: "list", Order: 20}, Build: build(screens.Logs), Part: build(screens.LogsRows)},
	}
}

type page struct {
	component.ContextOnly
	draw func(context.Context) render.HTML
}

func (p page) RenderCtx(ctx context.Context) render.HTML { return p.draw(ctx) }
