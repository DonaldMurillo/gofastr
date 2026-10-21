// Package meridian stands in for the flagship example, the one app held
// to the framework's rule: its words go through phrases.
package meridian

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

var welcome = i18n.Define("meridian.home.welcome", "Welcome back")

func page(ctx context.Context) render.HTML {
	_ = render.Text("Start free") // want `render.Text\("Start free"\) writes English`
	//gofastr:allow(rawtext) the product's name reads the same in every language
	_ = render.Text("Meridian Pro")
	return render.Text(welcome.Text(ctx))
}
