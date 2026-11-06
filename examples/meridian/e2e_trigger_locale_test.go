package main

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
)

func TestAdminPaletteRequestLocale(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e builds and boots Meridian")
	}
	base := e2eBootApp(t)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	e2eLogin(t, ctx, base)
	for _, request := range []struct {
		path, label string
	}{
		{"/admin", "Open command palette"},
		{"/es/admin", "Abrir la paleta de comandos"},
	} {
		var label string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+request.path),
			chromedp.WaitVisible(`.fui-cmd-trigger`, chromedp.ByQuery),
			chromedp.AttributeValue(`.fui-cmd-trigger`, "aria-label", &label, nil, chromedp.ByQuery),
		); err != nil {
			t.Fatal(err)
		}
		if label != request.label {
			t.Errorf("%s trigger label = %q, want %q", request.path, label, request.label)
		}
	}
	captureMeridianSurface(t, browser, base, "marketing-es", "/es/")
	captureMeridianSurface(t, browser, base, "admin-es", "/es/admin")
}
