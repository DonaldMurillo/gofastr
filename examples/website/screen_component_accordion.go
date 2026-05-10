package main

import (
	"github.com/gofastr/gofastr/core-ui/accordion"
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// AccordionScreen documents the core-ui/accordion package with live demos
// alongside the source they were rendered with.
type AccordionScreen struct{}

func (s *AccordionScreen) ScreenTitle() string        { return "Accordion" }
func (s *AccordionScreen) ScreenDescription() string  { return "Native <details> disclosures with modern CSS." }
func (s *AccordionScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AccordionScreen) Render() render.HTML {
	groupDemo := accordion.Group(
		accordion.GroupConfig{Name: "faq", AriaLabel: "Frequently asked questions"},
		accordion.Item{
			Summary: "Why native <details> instead of a custom widget?",
			Content: render.Tag("p", nil, render.Text(
				"Native <details> gives keyboard accessibility, focus management, and the open/closed state for free. The browser also gives us mutual exclusivity via the name= attribute — no JavaScript, no state machine, no drift.")),
			Open: true,
		},
		accordion.Item{
			Summary: "How does the smooth open/close animation work?",
			Content: render.Tag("p", nil, render.Text(
				"Three modern features: interpolate-size: allow-keywords lets the browser transition to and from auto height; ::details-content gives us a stylable slot for the revealed content; transition-behavior: allow-discrete keeps content-visibility in sync with the height transition.")),
		},
		accordion.Item{
			Summary: "What happens in older browsers?",
			Content: render.Tag("p", nil, render.Text(
				"They get instant open/close — no animation, but every other behavior still works. Progressive enhancement, not graceful degradation.")),
		},
	)

	stackDemo := accordion.Stack(
		accordion.StackConfig{AriaLabel: "Configuration sections"},
		accordion.Item{
			Summary: "Shipping address",
			Content: render.Tag("p", nil, render.Text(
				"This is a Stack — every section opens and closes independently. There is no name= attribute, so opening Billing does not close Shipping.")),
		},
		accordion.Item{
			Summary: "Billing",
			Content: render.Tag("p", nil, render.Text(
				"Useful for long-form configuration where the user is iterating across multiple sections. Click both summaries to confirm.")),
		},
		accordion.Item{
			Summary: "Notifications",
			Content: render.Tag("p", nil, render.Text(
				"Each <details> manages its own open state. The server can pre-open any subset by setting Open: true on those Items.")),
		},
	)

	groupSource := `accordion.Group(
    accordion.GroupConfig{Name: "faq"},
    accordion.Item{
        Summary: "Why native <details>?",
        Content: render.Tag("p", nil, render.Text("…")),
        Open:    true,
    },
    accordion.Item{
        Summary: "How does animation work?",
        Content: render.Tag("p", nil, render.Text("…")),
    },
)`

	stackSource := `accordion.Stack(
    accordion.StackConfig{},
    accordion.Item{Summary: "Shipping",      Content: shipping},
    accordion.Item{Summary: "Billing",       Content: billing},
    accordion.Item{Summary: "Notifications", Content: notifications},
)`

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),

		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("Accordion")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Two disclosure widgets built on the native <details>/<summary> elements: an exclusive Group (one open at a time) and an independent Stack.")),

		// --- Group ---
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Group — exclusive (single-open)")),
		render.Tag("p", nil, render.Text(
			"Use a Group when only one item should be open at a time — FAQs, settings panels, navigation drawers. The browser enforces exclusivity via the shared name= attribute. Try it: opening the second item closes the first automatically, with no JavaScript.")),
		demoFrame(groupDemo, groupSource),

		// --- Stack ---
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Stack — independent (multi-open)")),
		render.Tag("p", nil, render.Text(
			"Use a Stack when items should open and close independently — long forms, multi-step configuration, content-heavy pages with progressive disclosure.")),
		demoFrame(stackDemo, stackSource),

		// --- How it works ---
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("How the animation works")),
		render.Tag("p", nil, render.Text(
			"The accordion uses three modern CSS features. Together they let a native disclosure widget animate open and closed without JS:")),
		render.Tag("ul", nil,
			render.Tag("li", nil,
				render.Tag("strong", nil, render.Text("interpolate-size: allow-keywords")),
				render.Text(" — lets the browser transition to and from auto height.")),
			render.Tag("li", nil,
				render.Tag("strong", nil, render.Text("::details-content")),
				render.Text(" — a stylable pseudo-element representing the slot the browser hides and reveals.")),
			render.Tag("li", nil,
				render.Tag("strong", nil, render.Text("transition-behavior: allow-discrete")),
				render.Text(" — keeps content-visibility and display toggles in sync with the height transition.")),
		),
		render.Tag("p", nil, render.Text(
			"Browsers without these features get instant open/close — intentional progressive enhancement. The component also respects prefers-reduced-motion.")),

		// --- Server-rendered initial state ---
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Pre-opening on the server")),
		render.Tag("p", nil, render.Text(
			"Set Open: true on any Item to render with that item already open. The first item in the Group above demonstrates this. Useful for deep links, error states, or onboarding flows.")),

		// --- API summary ---
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("API")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`type GroupConfig struct {
    Name      string // required → <details name="…">
    Class     string
    ID        string
    AriaLabel string
}

type StackConfig struct {
    Class     string
    ID        string
    AriaLabel string
}

type Item struct {
    Summary string      // required
    Content render.HTML // required
    Open    bool
    ID      string
    Class   string
}

func Group(cfg GroupConfig, items ...Item) render.HTML
func Stack(cfg StackConfig, items ...Item) render.HTML
func BaseCSS() string`,
		))),
	)
}

// demoFrame wraps a live render of a component next to its source code.
func demoFrame(demo render.HTML, source string) render.HTML {
	return render.Tag("div", map[string]string{"class": "demo-frame"},
		render.Tag("div", map[string]string{"class": "demo-live"},
			render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Live")),
			demo,
		),
		render.Tag("div", map[string]string{"class": "demo-source"},
			render.Tag("div", map[string]string{"class": "demo-label"}, render.Text("Source")),
			render.Tag("pre", nil, render.Tag("code", nil, render.Text(source))),
		),
	)
}
