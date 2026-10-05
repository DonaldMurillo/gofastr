package headless

// Browser coverage for headless-feedback's retry: a 2xx from the
// health endpoint reports recovery and hides the offline banner; a
// failed probe leaves it shown. Same harness as behavior_e2e_test.go.

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

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

// While the retry probe is in flight the banner root says
// data-state="checking" (the state ui.NetworkRetryBanner's sheet
// styles as busy) and the link is aria-busy; a second click meanwhile
// fires no second probe; both clear when the probe settles.
func TestE2E_NetworkRetryMarksChecking(t *testing.T) {
	release := make(chan struct{})
	var probes atomic.Int32
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/health", func(w http.ResponseWriter, r *http.Request) {
			probes.Add(1)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	b := startBehaviorServer(t, offlineBannerPage(), extra)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("the offline banner marker never loaded headless-feedback")
	}
	const link = `document.querySelector('[data-hui-network-retry]')`
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('wrap').hidden = false; `+link+`.click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.getElementById('wrap').getAttribute('data-state') === 'checking' && `+
		link+`.getAttribute('aria-busy') === 'true'`) {
		t.Fatal("the banner did not say data-state=\"checking\" (and the link aria-busy) while the probe ran")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(link+`.click()`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := probes.Load(); n != 1 {
		t.Errorf("a click while checking fired another probe: %d probes", n)
	}
	close(release)
	if !pollTrue(ctx, `!document.getElementById('wrap').hasAttribute('data-state') && !`+link+`.hasAttribute('aria-busy')`) {
		t.Fatal("data-state and aria-busy did not clear when the probe settled")
	}
	if !pollTrue(ctx, `!document.getElementById('wrap').hidden`) {
		t.Fatal("a failed probe hid the banner")
	}
}

// A toast the module builds from a response header says its tone the
// way a server-rendered one does: the word comes from the stack's
// Strings, read and not shown, before the title. With no template on
// the page the row is bare hooks and no glyph, so the icon part goes
// and the title follows the tone word.
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
	if !pollTrue(ctx, `(function(){var s=document.querySelector('[data-hui-toast-stack] [data-hui-toast-tone]');`+
		`var n=s&&s.nextElementSibling;`+
		`return !!s && s.textContent==='Success: ' && !!n && n.hasAttribute('data-hui-toast-title') && n.textContent==='Saved'`+
		` && s.parentElement.className==='' && s.parentElement.querySelector('[data-hui-toast-icon]')===null;})()`) {
		t.Fatal("a runtime toast did not say its tone before its title, bare of classes and icon")
	}
}

// probeToastClasses is a kit's class map as a rig sees it: the
// template carries these, the module copies them onto the row it
// clones, and no class literal exists in the module.
var probeToastClasses = Classes{
	PartToastItem: "item", PartRoot: "row", PartToastToneWord: "tone", PartIcon: "ico",
	PartTitle: "ttl", PartBody: "bdy", PartDismiss: "dis",
	"root--success": "row--success", "root--danger": "row--danger",
}

// toastStackWithTemplate renders a stack carrying the row template
// the way preset's slot does for a kit that registered one.
func toastStackWithTemplate() string {
	return string(ToastStack(ToastStackProps{ID: "stack", Label: "Notifications", Toasts: []render.HTML{
		ToastTemplate(ToastTemplateProps{Glyphs: map[string]string{"success": "✓", "danger": "✕"}}, probeToastClasses),
	}}, nil))
}

// A toast the module builds wears the template's skin: the item and
// root classes, the tone's variant class, the glyph, and the dismiss
// label from the stack's Strings. A part with nothing to say is gone.
func TestE2E_RuntimeToastWearsTheTemplate(t *testing.T) {
	b := startBehaviorServer(t, toastStackWithTemplate())
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(FeedbackBehaviorName)) {
		t.Fatal("the stack marker never loaded headless-feedback")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.toast({variant:'success', title:'Saved'})`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `(function(){var r=document.querySelector('[data-hui-toast-stack] [data-hui-toast-id] [data-hui-toast]');`+
		`if(!r) return false; var ico=r.querySelector('[data-hui-toast-icon]'); var d=r.querySelector('[data-hui-toast-dismiss]');`+
		`return r.className==='row row--success' && r.parentElement.className==='item' && r.getAttribute('role')==='status'`+
		` && !!ico && ico.textContent==='✓' && ico.className==='ico'`+
		` && r.querySelector('[data-hui-toast-title]').textContent==='Saved' && r.querySelector('[data-hui-toast-body]')===null`+
		` && !!d && d.getAttribute('aria-label')==='Dismiss: Saved';})()`) {
		var stack string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(document.querySelector('[data-hui-toast-stack]')||{}).outerHTML || ''`, &stack))
		t.Fatalf("a runtime toast did not wear the template's classes, glyph and dismiss label; the stack holds:\n%s", stack)
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
