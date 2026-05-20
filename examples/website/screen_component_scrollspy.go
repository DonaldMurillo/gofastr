package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/nestedlist"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/scrollspy"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// demoScrollspyStyle registers the demo's two-column layout CSS via
// the proper pipeline (strict CSP blocks inline style="" on the page).
var demoScrollspyStyle = registry.RegisterStyle("demo-scrollspy-layout", func(_ style.Theme) string {
	return `.demo-spy-layout {
  display: grid;
  grid-template-columns: 200px 1fr;
  gap: var(--spacing-lg, 16px);
  align-items: start;
}
.demo-spy-layout > [data-fui-scrollspy] {
  position: sticky;
  top: var(--spacing-md, 12px);
  align-self: start;
  max-block-size: calc(100vh - 4rem);
  overflow-y: auto;
  padding: var(--spacing-sm, 6px);
  border: 1px solid var(--color-border, #E5E7EB);
  border-radius: var(--radii-sm, 4px);
}
.demo-spy-body {
  display: grid;
  gap: var(--spacing-xl, 24px);
}
.demo-spy-section {
  min-block-size: 24rem;
  padding: var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E5E7EB);
  border-radius: var(--radii-sm, 4px);
  background: var(--color-surface, #FFFFFF);
}
.demo-spy-section h2 {
  margin: 0 0 var(--spacing-md, 12px);
  scroll-margin-block-start: var(--spacing-lg, 16px);
}
`
})

type ScrollSpyScreen struct{}

func (*ScrollSpyScreen) ScreenTitle() string {
	return "ScrollSpy"
}
func (*ScrollSpyScreen) ScreenDescription() string {
	return "IntersectionObserver-based active-section tracking for any nav of in-page anchors."
}
func (*ScrollSpyScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ScrollSpyScreen) Render() render.HTML {
	// Demo nav targets the "[data-scrollspy-demo]" container below.
	nav := nestedlist.Render(nestedlist.Config{
		AriaLabel: "On this page",
		Items: []nestedlist.Item{
			{Label: "Intro", Href: "#spy-intro"},
			{Label: "How it works", Href: "#spy-how"},
			{Label: "Configuration", Href: "#spy-config"},
			{Label: "Accessibility", Href: "#spy-a11y"},
			{Label: "Conclusion", Href: "#spy-conclusion"},
		},
	})

	sidebar := scrollspy.Wrap(
		scrollspy.Config{ObserveSelector: "[data-scrollspy-demo]"},
		nav,
	)

	// Big scrollable body with anchor targets. Each section is tall
	// enough that only one is in the viewport at a time.
	section := func(id, title, body string) render.HTML {
		return render.Tag("section", map[string]string{"id": id, "class": "demo-spy-section"},
			html.Heading(html.HeadingConfig{Level: 2}, render.Text(title)),
			render.Tag("p", nil, render.Text(body)),
		)
	}

	body := render.Tag("div", map[string]string{
		"data-scrollspy-demo": "true",
		"class":               "demo-spy-body",
	},
		section("spy-intro", "Intro",
			"ScrollSpy turns any nav of in-page anchors into a live indicator of scroll position. Open this page, scroll the section column on the right, and watch the matching link in the left rail pick up the .is-active class and aria-current=true."),
		section("spy-how", "How it works",
			"On first appearance, the runtime walks every anchor inside [data-fui-scrollspy], maps each href=\"#id\" to an element under the observed region, and registers an IntersectionObserver with rootMargin: '0px 0px -70% 0px'. The intersecting target whose top is closest to the rootMargin wins."),
		section("spy-config", "Configuration",
			"ObserveSelector picks the region (e.g. \"main\", \".doc-body\"). TargetSelector overrides the default \"h2[id], h3[id]\" for non-heading section anchors. ID and Class give you wrapper-level hooks for theming."),
		section("spy-a11y", "Accessibility",
			"Active state is dual-coded: .is-active for CSS hooks AND aria-current=\"true\" so assistive tech announces which section the user is in. No JS-only state — keyboard scroll works the same as mouse."),
		section("spy-conclusion", "Conclusion",
			"Pair with nestedlist for a polished docs sidebar, or wrap any sticky nav. Pure pattern, no per-page wiring — the runtime auto-loads scrollspy.js on first appearance."),
	)

	// Layout: 2 columns (sidebar + scrollable body)
	layout := render.Tag("div", map[string]string{"class": "demo-spy-layout"},
		sidebar,
		body,
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("ScrollSpy")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"IntersectionObserver-based active-section tracking. Wraps any nav list of in-page anchors and sets aria-current + .is-active on the link whose target is in view.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Live demo")),
		render.Tag("p", nil, render.Text(
			"Scroll the section column on the right — the left rail updates in real time. Click a link in the rail to jump to that section.")),
		demoScrollspyStyle.WrapHTML(layout),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Reversed-nav test")),
		render.Tag("p", nil, render.Text(
			"Nav items in reverse order vs DOM. Bootstrap must still pick the DOM-topmost section (Intro), not whatever the nav listed first (Conclusion).")),
		scrollspy.Wrap(
			scrollspy.Config{ObserveSelector: "[data-scrollspy-reversed]"},
			nestedlist.Render(nestedlist.Config{
				AriaLabel: "Reversed nav",
				Items: []nestedlist.Item{
					{Label: "Conclusion", Href: "#rev-spy-conclusion"},
					{Label: "Accessibility", Href: "#rev-spy-a11y"},
					// id starts with a digit — exercises the
					// cssEscape polyfill's leading-digit branch.
					{Label: "Quirky id", Href: "#42-quirky"},
					{Label: "Configuration", Href: "#rev-spy-config"},
					{Label: "How it works", Href: "#rev-spy-how"},
					{Label: "Intro", Href: "#rev-spy-intro"},
				},
			}),
		),
		render.Tag("div", map[string]string{
			"data-scrollspy-reversed": "true",
			"class":                   "demo-spy-body",
		},
			section("rev-spy-intro", "Intro", "Topmost in DOM."),
			section("rev-spy-how", "How", "Second."),
			section("rev-spy-config", "Config", "Third."),
			section("42-quirky", "Quirky id", "id starts with a digit — legal HTML5 but illegal as a bare CSS selector; the polyfill must escape it."),
			section("rev-spy-a11y", "A11y", "Fifth."),
			section("rev-spy-conclusion", "Conclusion", "Bottom in DOM."),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Wiring")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`scrollspy.Wrap(
    scrollspy.Config{ObserveSelector: "main"},
    nestedlist.Render(nestedlist.Config{
        Items: []nestedlist.Item{
            {Label: "Intro",    Href: "#intro"},
            {Label: "Setup",    Href: "#setup"},
        },
    }),
)`))),
	)
}
