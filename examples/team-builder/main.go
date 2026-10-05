// Package main is the dogfood example for core-ui/localdb and
// framework/localentity: a Pokémon team builder whose team lives in
// the visitor's browser. There is no database, no account, and no
// request per change: the server renders the page once, and the
// localentity behaviour saves the form into IndexedDB and renders the
// team from it. Open a second tab and both stay in step.
package main

import (
	"context"
	"log"
	"net/http"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// defaultAddr is the fallback when $PORT is unset.
const defaultAddr = ":8093"

func main() {
	fwApp := buildApp()
	addr, err := isolation.ListenAddr(".", defaultAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("team-builder listening on %s", addr)
	if err := fwApp.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// buildApp wires the site and the host without binding a port, so
// tests drive fwApp.Router() directly.
func buildApp() *framework.App {
	site := uiapp.NewApp("Team builder")
	site.WithTheme(theme.Default())
	layout := uiapp.NewLayout("main", uiapp.LayoutSpec{}, func(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
		return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			ui.ContentRow(ui.ContentRowConfig{},
				ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())))
	})
	site.SetDefaultLayout(layout)
	site.RegisterScreen(uiapp.NewScreen("/", &TeamScreen{}).WithTitle("Your team"), nil)

	fwApp := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "team-builder"}))
	fwApp.Mount(uihost.New(site))
	return fwApp
}
