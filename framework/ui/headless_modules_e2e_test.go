package ui_test

// Browser coverage for the headless behaviour modules as framework/ui
// renders them: the real component, the real runtime and modules
// (served by themeToggleTestPage's harness), a click or a signal, and
// the DOM or ARIA outcome a user meets.

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// moduleTestCtx opens the page body renders on and waits for #ready.
func moduleTestCtx(t *testing.T, body string, opts ...chromedptest.Option) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	srv := themeToggleTestPage(t, body)
	ctx := chromedptest.Context(t, opts...)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return ctx
}

// pollJS evaluates a boolean expression until it is true or about
// four seconds pass.
func pollJS(ctx context.Context, js string) bool {
	for range 40 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// evalString reads a string expression, empty on error.
func evalString(ctx context.Context, js string) string {
	var s string
	_ = chromedp.Run(ctx, chromedp.Evaluate(js, &s))
	return s
}

func moduleLoaded(name string) string {
	return `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['` + name + `'])`
}

// A CopyButton with ToastOnCopy shows a toast when clicked: the
// config rides the button, and the feedback module must find it there.
func TestCopyButtonToastOnCopyShowsAToast(t *testing.T) {
	body := `<pre id="snippet">go get gofastr</pre>` +
		string(ui.CopyButton(ui.CopyButtonConfig{Target: "snippet", ToastOnCopy: true, ToastTitle: "Copied it"})) +
		string(preset.ToastSlotHTML(context.Background(), "toasts"))
	ctx := moduleTestCtx(t, body)
	if !pollJS(ctx, moduleLoaded("headless-feedback")) {
		t.Fatal("the copy marker never loaded headless-feedback")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-copy] button').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `(function(){var t=document.querySelector('[data-cui-toast-stack] [data-hui-toast-id] [data-hui-toast-title]');`+
		`return !!t && t.textContent==='Copied it';})()`) {
		t.Fatalf("no toast after a ToastOnCopy click; the stack holds:\n%s",
			evalString(ctx, `(document.querySelector('[data-cui-toast-stack]')||{}).innerHTML||''`))
	}
}
