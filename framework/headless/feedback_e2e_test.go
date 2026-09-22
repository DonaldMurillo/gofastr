package headless

// Browser coverage for headless-feedback's retry: a 2xx from the
// health endpoint reports recovery and hides the offline banner; a
// failed probe leaves it shown. Same harness as behavior_e2e_test.go.

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/chromedp/chromedp"
)

func offlineBannerPage() string {
	return `<div id="wrap" data-hui-system="" data-hui-system-id="net"
		data-hui-system-offline="" hidden="" role="alert" aria-live="assertive">
	<p>Connection lost</p>
	<div><a href="/__hui/health" data-hui-network-retry="">Retry now</a></div>
</div>`
}

func TestE2E_NetworkRetry2xxHidesTheBanner(t *testing.T) {
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	b := startBehaviorServer(t, offlineBannerPage(), extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("the offline banner marker never loaded headless-feedback")
	}
	// Show the banner (the module's own offline visibility follows the
	// connection report; the manual API is the test's path to "down").
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__gofastr.networkStatus.reportFailure()`, nil),
		chromedp.Evaluate(`document.getElementById('wrap').hidden = false`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-network-retry]').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('wrap').hidden`) {
		t.Fatal("a 2xx from the health endpoint did not hide the banner")
	}
}

func TestE2E_NetworkRetryFailedProbeLeavesItShown(t *testing.T) {
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	b := startBehaviorServer(t, offlineBannerPage(), extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("the offline banner marker never loaded headless-feedback")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('wrap').hidden = false; document.querySelector('[data-hui-network-retry]').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	var hidden bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('wrap').hidden`, &hidden)); err != nil {
		t.Fatal(err)
	}
	if hidden {
		t.Fatal("a failed health probe hid the banner — the connection is still down")
	}
}

// A toast the module builds from a response header says its tone the
// way a server-rendered one does: the word comes from the stack's
// Strings, read and not shown, before the icon and the title.
func TestE2E_RuntimeToastSaysItsTone(t *testing.T) {
	b := startBehaviorServer(t, string(ToastStack(ToastStackProps{ID: "stack", Label: "Notifications"}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("the stack marker never loaded headless-feedback")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.toast({variant:'success', title:'Saved', body:'Your changes are persisted.'})`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(function(){var s=document.querySelector('[data-hui-toast-stack] .fui-visually-hidden');`+
		`return !!s && s.textContent==='Success: ' && s.nextElementSibling.className==='fui-notification__icon';})()`) {
		t.Fatal("a runtime toast did not say its tone before its icon and title")
	}
}

// A server-rendered toast with a lifetime leaves when it is over: the
// module arms the row it did not build.
func TestE2E_ServerToastWithATTLLeaves(t *testing.T) {
	row := Toast(ToastProps{Tone: "info", Title: "Build started", TTLMS: 300}, nil)
	b := startBehaviorServer(t, string(ToastStack(ToastStackProps{ID: "stack", Label: "Notifications",
		Toasts: []render.HTML{row}}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("the stack marker never loaded headless-feedback")
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-toast]') === null`) {
		t.Fatal("a server-rendered toast with a lifetime never left")
	}
}
