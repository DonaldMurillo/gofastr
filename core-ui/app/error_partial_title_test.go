package app_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type plainBody struct{}

func (plainBody) Render() render.HTML { return "oops" }

// The error partial left Title empty, so the host fell back to the
// failed route's registered title ("Billing") over the error body,
// while the not-found twin carried its own.
func TestErrorPartialCarriesErrorTitle(t *testing.T) {
	a := app.NewApp("Luna")
	a.Register("/", plainBody{}, nil)
	a.RegisterScreen(app.NewScreen("/billing", plainBody{}).WithTitle("Billing"), nil)
	res, err := a.RenderErrorFromResult(context.Background(), "/billing", "/", plainBody{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "Something went wrong" {
		t.Errorf("error partial Title = %q, want the error page's title", res.Title)
	}
}
