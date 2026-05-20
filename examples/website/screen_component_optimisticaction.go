package main

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// CSRF recorder for e2e: the POST endpoint stores the incoming
// X-CSRF-Token header; the GET endpoint returns the latest stored
// value. Demo-only — real apps verify the token server-side and
// reject the request on mismatch.
var lastCSRFToken atomic.Pointer[string]

// OptimisticDemoCSRFRecord captures the X-CSRF-Token header from
// every incoming optimistic-action request and stores it for the
// e2e test to read back.
func OptimisticDemoCSRFRecord(w http.ResponseWriter, r *http.Request) {
	tok := r.Header.Get("X-CSRF-Token")
	lastCSRFToken.Store(&tok)
	w.WriteHeader(http.StatusNoContent)
}

// OptimisticDemoCSRFLast returns the most recent captured X-CSRF-Token
// header. Empty string when nothing has been recorded yet.
func OptimisticDemoCSRFLast(w http.ResponseWriter, _ *http.Request) {
	if p := lastCSRFToken.Load(); p != nil {
		w.Write([]byte(*p))
		return
	}
}

// OptimisticDemoSuccess returns 204 so the optimistic-action demo's
// success-path button latches into its committed state.
func OptimisticDemoSuccess(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// OptimisticDemoFailure returns 500 so the failure-path button rolls
// back after the optimistic flip.
func OptimisticDemoFailure(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "simulated failure", http.StatusInternalServerError)
}

// OptimisticDemoSlow waits ~400ms before returning 204 so tests can
// observe the pending-state DOM (aria-busy / disabled) while the RPC
// is in flight.
func OptimisticDemoSlow(w http.ResponseWriter, _ *http.Request) {
	time.Sleep(400 * time.Millisecond)
	w.WriteHeader(http.StatusNoContent)
}

type OptimisticActionScreen struct{}

func (*OptimisticActionScreen) ScreenTitle() string {
	return "OptimisticAction"
}
func (*OptimisticActionScreen) ScreenDescription() string {
	return "Button that flips to its success state immediately on click; the RPC fires underneath and rolls back on non-2xx."
}
func (*OptimisticActionScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OptimisticActionScreen) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("OptimisticAction")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Optimistic UI: the success state is declared in SSR markup and flipped on click before the RPC returns. On non-2xx the button rolls back with a brief shake.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Success path")),
		render.Tag("p", nil, render.Text(
			"Click to follow — the endpoint responds 204 and the committed state sticks.")),
		demoFrame(
			ui.OptimisticAction(ui.OptimisticActionConfig{
				Endpoint:     "/demo/optimistic-success",
				IdleLabel:    "Follow",
				SuccessLabel: "Following ✓",
			}),
			`ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/follow",
    IdleLabel:    "Follow",
    SuccessLabel: "Following ✓",
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Failure path (rollback)")),
		render.Tag("p", nil, render.Text(
			"Click to subscribe — this endpoint responds 500. The button flips optimistically, then rolls back with a small shake.")),
		demoFrame(
			ui.OptimisticAction(ui.OptimisticActionConfig{
				Endpoint:     "/demo/optimistic-failure",
				IdleLabel:    "Subscribe",
				SuccessLabel: "Subscribed ✓",
				Variant:      ui.ButtonSecondary,
			}),
			`ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/checkout",
    IdleLabel:    "Subscribe",
    SuccessLabel: "Subscribed ✓",
    Variant:      ui.ButtonSecondary,
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("CSRF protection")),
		render.Tag("p", nil, render.Text(
			"When a <meta name=\"csrf-token\" content=\"…\"> tag is on the page, the runtime adds the token as X-CSRF-Token on every state-changing fetch. Click below to record the header at /demo/csrf-record; the test fixture asserts it round-trips.")),
		demoFrame(
			ui.OptimisticAction(ui.OptimisticActionConfig{
				Endpoint:     "/demo/csrf-record",
				IdleLabel:    "Save with token",
				SuccessLabel: "Saved ✓",
			}),
			`ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/api/save",
    IdleLabel:    "Save with token",
    SuccessLabel: "Saved ✓",
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Slow endpoint (visible pending state)")),
		render.Tag("p", nil, render.Text(
			"The endpoint waits ~400ms before responding 204. While the RPC is in flight the button carries aria-busy=true and disabled so screen readers announce the change and a second click can't fire a duplicate.")),
		demoFrame(
			ui.OptimisticAction(ui.OptimisticActionConfig{
				Endpoint:     "/demo/optimistic-slow",
				IdleLabel:    "Save",
				SuccessLabel: "Saved ✓",
			}),
			`ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/api/save",
    IdleLabel:    "Save",
    SuccessLabel: "Saved ✓",
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Custom method")),
		render.Tag("p", nil, render.Text(
			"DELETE requests are equally cheap — pair with confirmation if the action is destructive.")),
		demoFrame(
			ui.OptimisticAction(ui.OptimisticActionConfig{
				Endpoint:     "/demo/optimistic-success",
				Method:       "DELETE",
				IdleLabel:    "Remove",
				SuccessLabel: "Removed ✓",
				Variant:      ui.ButtonDanger,
			}),
			`ui.OptimisticAction(ui.OptimisticActionConfig{
    Endpoint:     "/items/42",
    Method:       "DELETE",
    IdleLabel:    "Remove",
    SuccessLabel: "Removed ✓",
    Variant:      ui.ButtonDanger,
})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("How it works")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("Both labels ship in SSR — the runtime only toggles a `hidden` attribute.")),
			render.Tag("li", nil, render.Text("`data-state` cycles idle → pending → committed (success) or idle → pending → error → idle (rollback).")),
			render.Tag("li", nil, render.Text("DOM events `optimistic-action:start` / `committed` / `rolled-back` bubble so apps can update adjacent counters or icons.")),
		),
	)
}

