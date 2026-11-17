package tracker

import (
	"context"
	"errors"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/examples/site/tracker/appbar"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/static"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// buildApp is the tracker on its own host, mounted at the root, without
// binding a port, so the tests drive it in-process. The site mounts the
// same Register under /examples/tracker (see the site's tracker test).
func buildApp() *framework.App {
	site := uiapp.NewApp("Acme Tracker")
	site.WithTheme(theme.Default(theme.Overrides{
		// Acme's own accent over the framework palette: a deep teal
		// that keeps 4.5:1 on the light surface, with its dark twin.
		Primary: "#0F766E",
		Dark:    &theme.Overrides{Primary: "#5EEAD4"},
		// The app bar's own tokens (the search field's width).
	}).Extend(appbar.Tokens))
	Register(site, "")
	// The 404 and error screens render through the shell like any page.
	site.SetDefaultLayout(trackerShell)
	host := uihost.New(site,
		// An unknown URL renders inside the shell like any other page.
		uihost.WithNotFoundScreen(&NotFoundScreen{}),
		// A whole-page failure (LEG-40's store) gets the tracker's own
		// screen through the shell, matching the not-found page.
		uihost.WithErrorScreen(&ErrorScreen{}),
	)
	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "tracker"}),
	)
	MountWidgets(routerMounter{fwApp.Router()})
	fwApp.Use(host.RouteMatchMiddleware())
	fwApp.Mount(host)
	return fwApp
}

// routerMounter adapts *router.Router to ui.WidgetMounter.
type routerMounter struct{ r *router.Router }

func (m routerMounter) MountWidget(def *widget.Definition) {
	widget.Mount(m.r, def)
}

// exportTo renders the app to static HTML + assets. Every dynamic route
// carries StaticPaths; the Legacy project's issue pages are deliberately
// absent (see IssueScreen.StaticPaths for why).
func exportTo(ctx context.Context, fwApp *framework.App, dir string) error {
	var host *uihost.UIHost
	for _, m := range fwApp.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			host = h
			break
		}
	}
	if host == nil {
		return errors.New("no uihost.UIHost mounted")
	}
	_, err := (&static.Builder{Host: host, OutDir: dir}).Build(ctx)
	return err
}
