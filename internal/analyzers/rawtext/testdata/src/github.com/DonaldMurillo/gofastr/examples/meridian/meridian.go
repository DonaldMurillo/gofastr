// Package meridian stands in for the flagship example, the one app held
// to the framework's rule: its words go through phrases.
package meridian

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var welcome = i18n.Define("meridian.home.welcome", "Welcome back")

func page(ctx context.Context) render.HTML {
	_ = render.Text("Start free") // want `render.Text\("Start free"\) writes English`
	//gofastr:allow(rawtext) the product's name reads the same in every language
	_ = render.Text("Meridian Pro")
	return render.Text(welcome.Text(ctx))
}

type chromeConfig struct {
	Ctx context.Context
}

func tags(ctx context.Context) {
	_ = ui.TagConfig{} // want `ui.TagConfig leaves Ctx unset while a request context is in scope.*set Ctx: ctx`
	_ = ui.TagConfig{Ctx: ctx}
	_ = ui.TagConfig{ctx, welcome.Text(ctx)}
	_ = ui.ButtonConfig{Class: "primary"}
	_ = chromeConfig{}
	bound := ui.TagConfig{}
	bound.Ctx = ctx
	_ = bound
	_ = func() {
		_ = ui.TagConfig{} // want `set Ctx: ctx`
	}
}

func tagsFromConfig(cfg chromeConfig) {
	_ = &ui.TagConfig{} // want `set Ctx: cfg.Ctx`
}

func tagsFromRequest(_ http.ResponseWriter, r *http.Request) {
	_ = ui.TagConfig{} // want `set Ctx: r.Context\(\)`
}

func tagsWithoutContext() {
	_ = ui.TagConfig{}
}
