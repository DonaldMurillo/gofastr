package main

import (
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
)

func TestAdminTriggersRequestLocale(t *testing.T) {
	if testing.Short() {
		t.Skip("browser e2e builds and boots Meridian")
	}
	base := e2eBootApp(t)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	e2eLogin(t, ctx, base)
	for _, request := range []struct {
		path, label, keys string
	}{
		{"/admin", "Open command palette", "Keyboard shortcuts"},
		{"/es/admin", "Abrir la paleta de comandos", "Atajos de teclado"},
	} {
		var label, keys string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+request.path),
			chromedp.WaitVisible(`.fui-cmd-trigger`, chromedp.ByQuery),
			chromedp.AttributeValue(`.fui-cmd-trigger`, "aria-label", &label, nil, chromedp.ByQuery),
			chromedp.AttributeValue(`[data-cui-open="admin-keys"]`, "aria-label", &keys, nil, chromedp.ByQuery),
		); err != nil {
			t.Fatal(err)
		}
		if label != request.label {
			t.Errorf("%s trigger label = %q, want %q", request.path, label, request.label)
		}
		if keys != request.keys {
			t.Errorf("%s shortcut label = %q, want %q", request.path, keys, request.keys)
		}
	}
	captureMeridianSurface(t, browser, base, "marketing-es", "/es/")
	captureMeridianSurface(t, browser, base, "admin-es", "/es/admin")
}
