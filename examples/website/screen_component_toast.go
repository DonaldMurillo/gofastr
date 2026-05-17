package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ToastScreen documents preset.ToastStack + the two push paths
// (client JS API and server response header).
type ToastScreen struct{}

// positionToastBtn renders a client-trigger button bound to a
// specific stack name. The data-fui-toast attribute carries the
// JSON cfg the runtime dispatches via __gofastr.toast().
func positionToastBtn(label, stack, variant, title, body string) render.HTML {
	cfg := `{"variant":"` + variant + `","title":"` + title + `","body":"` + body + `","ttl":4000,"stack":"` + stack + `"}`
	return render.Tag("button", map[string]string{
		"class":          "cta-button",
		"data-fui-toast": cfg,
	}, render.Text(label))
}

func (s *ToastScreen) ScreenTitle() string        { return "Toast" }
func (s *ToastScreen) ScreenDescription() string  { return "Stacked notifications, client- or server-triggered. No SSE — toast state lives in the browser." }
func (s *ToastScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ToastScreen) Render() render.HTML {
	// --- Server-pushed (header-driven) ----------------------------
	serverSuccess := render.Tag("button", map[string]string{
		"class":             "cta-button",
		"data-fui-rpc":      "/components/toast/push",
		"data-fui-rpc-body": `{"variant":"success","title":"Saved","body":"Your changes are persisted."}`,
	}, render.Text("Server: success (5s)"))

	serverWarn := render.Tag("button", map[string]string{
		"class":             "cta-button",
		"data-fui-rpc":      "/components/toast/push",
		"data-fui-rpc-body": `{"variant":"warning","title":"Slow query","body":"That report took 4.2s."}`,
	}, render.Text("Server: warning (5s)"))

	serverDanger := render.Tag("button", map[string]string{
		"class":             "cta-button",
		"data-fui-rpc":      "/components/toast/push",
		"data-fui-rpc-body": `{"variant":"danger","title":"Upload failed","body":"Retry?","persistent":true}`,
	}, render.Text("Server: persistent error"))

	serverMulti := render.Tag("button", map[string]string{
		"class":             "cta-button",
		"data-fui-rpc":      "/components/toast/push-burst",
		"data-fui-rpc-body": `{}`,
	}, render.Text("Server: burst of 3 toasts"))

	// --- Client-side trigger via __gofastr.toast() ----------------
	// data-fui-fill-input gives us a clickable that runs scripted
	// behavior, but for toast we use data-fui-action="toast" plus a
	// JSON payload in data-fui-toast-cfg — wired below via the
	// runtime's global click dispatcher (we add a small attribute
	// handler so this screen doesn't need inline JS).
	clientSuccess := render.Tag("button", map[string]string{
		"class":               "cta-button",
		"data-fui-toast":      `{"variant":"success","title":"Local success","body":"Triggered from JS without a server round-trip.","ttl":5000}`,
	}, render.Text("Client: success (no round-trip)"))

	clientNeutral := render.Tag("button", map[string]string{
		"class":               "cta-button",
		"data-fui-toast":      `{"variant":"neutral","title":"Heads up","ttl":4000}`,
	}, render.Text("Client: neutral, title-only"))

	clientInfo := render.Tag("button", map[string]string{
		"class":               "cta-button",
		"data-fui-toast":      `{"variant":"info","title":"FYI","body":"Body text and a five-second TTL.","ttl":5000}`,
	}, render.Text("Client: info"))

	headerSrc := `// Server side — any data-fui-rpc handler can attach
// X-Gofastr-Toast on its response; the runtime fires it on 2xx.
func push(w http.ResponseWriter, r *http.Request) {
    ui.AddToastSuccess(w, "Saved", "Your changes are persisted.", 5000)
    w.WriteHeader(http.StatusNoContent)
}

// HTTP wire:
// HTTP/1.1 204 No Content
// X-Gofastr-Toast: [{"variant":"success","title":"Saved","ttl":5000}]`

	clientSrc := `// Client side — any element can carry data-fui-toast="<json>";
// the runtime's click delegator parses + dispatches.
<button data-fui-toast='{"variant":"success","title":"Local"}'>...</button>

// Or directly from script:
window.__gofastr.toast({variant:"success", title:"Local", ttl:5000});`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),

		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Toast")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A stacked, ephemeral notification surface. Toasts can be triggered two ways — both converge on the same client-side stack. No SSE, no per-page connection budget, no server queue: the stack lives in the browser.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Server-triggered via response header")),
		render.Tag("p", nil, render.Text(
			"Any HTTP handler reached by data-fui-rpc can attach an X-Gofastr-Toast response header. The runtime reads it on 2xx and fires the toast — no extra request, no SSE.")),
		render.Tag("div", map[string]string{"class": "demo-frame"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
				render.Tag("div", map[string]string{"class": "demo-button-row"},
					serverSuccess, serverWarn, serverDanger, serverMulti),
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				render.Tag("pre", nil, render.Tag("code", nil, render.Text(headerSrc))),
			),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Client-triggered via JS API")),
		render.Tag("p", nil, render.Text(
			"Use data-fui-toast=\"<json>\" on any element, or call window.__gofastr.toast({...}) from script. No server involvement.")),
		render.Tag("div", map[string]string{"class": "demo-frame"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
				render.Tag("div", map[string]string{"class": "demo-button-row"},
					clientSuccess, clientInfo, clientNeutral),
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				render.Tag("pre", nil, render.Tag("code", nil, render.Text(clientSrc))),
			),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Six anchor points")),
		render.Tag("p", nil, render.Text(
			"A toast stack widget anchors at one of six points around the viewport. Each button below fires a toast into a different stack — click each to see them slide in from their respective corner / mid-edge.")),
		render.Tag("div", map[string]string{"class": "demo-frame"},
			render.Tag("div", map[string]string{"class": "demo-live"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live — six positions")),
				render.Tag("div", map[string]string{"class": "demo-toast-grid"},
					positionToastBtn("Top-left", "toasts-top-left", "info", "Top-left", "Anchored to the viewport's top-left edge."),
					positionToastBtn("Top-center", "toasts-top-center", "info", "Top-center", "Centered horizontally at the top edge."),
					positionToastBtn("Top-right", "toasts-top-right", "info", "Top-right", "The classic notification corner."),
					positionToastBtn("Bottom-left", "toasts-bottom-left", "success", "Bottom-left", "Useful for low-priority confirmations."),
					positionToastBtn("Bottom-center", "toasts-bottom-center", "warning", "Bottom-center", "A persistent mid-edge surface."),
					positionToastBtn("Bottom-right", "toasts-bottom-right", "danger", "Bottom-right", "High-visibility for critical errors."),
				),
			),
			render.Tag("div", map[string]string{"class": "demo-source"},
				render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
				render.Tag("pre", nil, render.Tag("code", nil, render.Text(
					`// Register a stack at each anchor point you want.
for _, p := range []struct{ name string; pos widget.Position }{
    {"toasts-top-left",    widget.TopLeft},
    {"toasts-top-center",  widget.TopCenter},
    {"toasts-top-right",   widget.TopRight},
    {"toasts-bottom-left",   widget.BottomLeft},
    {"toasts-bottom-center", widget.BottomCenter},
    {"toasts-bottom-right",  widget.BottomRight},
} {
    stack := preset.ToastStack(p.name).Mount(p.pos).Build()
    widget.Mount(r, &stack)
}

// Target a specific stack via cfg.stack on the client side:
__gofastr.toast({title: "Hi", stack: "toasts-bottom-center"});

// Or from the server (header path), via ToastTrigger.Stack:
ui.AddToast(w, ui.ToastTrigger{
    Title: "Saved", Stack: "toasts-bottom-center",
})`))),
			),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Behaviour")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text(
				"Each toast slides in from the top-right with theme-driven duration + easing tokens.")),
			render.Tag("li", nil, render.Text(
				"Hover or focus a toast to pause its TTL; leaving resumes from where it stopped.")),
			render.Tag("li", nil, render.Text(
				"ttl == 0 (or persistent: true) keeps the toast until the user dismisses it via the × button.")),
			render.Tag("li", nil, render.Text(
				"Polite vs assertive aria-live is picked from the variant — info/success polite, warning/danger assertive.")),
			render.Tag("li", nil, render.Text(
				"prefers-reduced-motion disables the slide animation.")),
			render.Tag("li", nil, render.Text(
				"Multiple toasts queued on one response: server sets X-Gofastr-Toast as a JSON array; the runtime fires each in order.")),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("API")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`// Server (any http.Handler reachable via data-fui-rpc):
func AddToast(w http.ResponseWriter, t ToastTrigger)
func AddToastSuccess(w http.ResponseWriter, title, body string, ttlMs int)
func AddToastError(w http.ResponseWriter, title, body string)  // persistent
func AddToastWarning(w http.ResponseWriter, title, body string, ttlMs int)

type ToastTrigger struct {
    Variant StatusVariant // info | success | warning | danger | neutral
    Title   string         // required
    Body    string
    TTL     int            // ms; 0 = persistent
    Stack   string         // optional: target a specific stack widget
}

// Client:
window.__gofastr.toast({
    variant: 'success',    // optional, defaults to 'info'
    title:   'Saved',      // required
    body:    'Your...',    // optional
    ttl:     5000,         // optional; 0 = persistent
    stack:   'site-toasts' // optional; first stack on page by default
});`,
		))),
	)
}
