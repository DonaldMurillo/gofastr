package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ComponentsIndexScreen lists every core-ui component package the website
// dogfoods. Each entry links to a live demo + explainer page.
type ComponentsIndexScreen struct{}

func (s *ComponentsIndexScreen) ScreenTitle() string        { return "Components" }
func (s *ComponentsIndexScreen) ScreenDescription() string  { return "Live, dogfooded core-ui components." }
func (s *ComponentsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

type componentEntry struct {
	Slug  string
	Name  string
	Tag   string
	Intro string
}

var componentEntries = []componentEntry{
	{
		Slug:  "accordion",
		Name:  "Accordion",
		Tag:   "Group · Stack",
		Intro: "Disclosure widgets built on native <details>/<summary>. Two variants: an exclusive group (single-open via the name= attribute) and an independent stack. Pure server-rendered, modern-CSS animation, zero JavaScript.",
	},
	{
		Slug:  "tabs",
		Name:  "Tabs",
		Tag:   "Tab strip",
		Intro: "Tabbed-content layout built from native <details> elements arranged with CSS Grid. Zero JavaScript, native keyboard accessibility, full mutual exclusivity via the name= attribute.",
	},
	{
		Slug:  "progress",
		Name:  "Progress",
		Tag:   "Determinate · Indeterminate",
		Intro: "Native <progress> wrapped with theme-aware styling. Determinate when Value is set, animated indeterminate when Value < 0. Drive live updates via signal binding.",
	},
	{
		Slug:  "skeleton",
		Name:  "Skeleton",
		Tag:   "Line · Block · Circle",
		Intro: "Pure-CSS shimmer placeholders for loading states. Three variants cover paragraphs, blocks, and avatars. Aria-hidden so screen readers announce the parent's loading state once, not every shimmer.",
	},
	{
		Slug:  "breadcrumbs",
		Name:  "Breadcrumbs",
		Tag:   "Trail nav",
		Intro: "Ordered-list trail with aria-current=\"page\" on the leaf. CSS-driven slash separators (no DOM noise). One <nav aria-label=\"Breadcrumb\"> wrapper.",
	},
	{
		Slug:  "pagination",
		Name:  "Pagination",
		Tag:   "Numeric nav",
		Intro: "Numeric page navigation with first/last anchors and ellipses for gaps. ARIA-correct (<nav aria-label=\"Pagination\">, aria-current=\"page\"), prev/next disabled at boundaries.",
	},
	{
		Slug:  "modal",
		Name:  "Modal",
		Tag:   "Dialog · Deeplink",
		Intro: "Center-mounted surface with backdrop, focus trap, scroll lock, return-focus on close. Optional DeepLink wiring pushes ?modal=name onto the URL so refresh / share / back-button preserve the open state — and per-row data passed via data-fui-deeplink.",
	},
	{
		Slug:  "drawer",
		Name:  "Drawer",
		Tag:   "Edge panel · Deeplink",
		Intro: "Edge-mounted sliding panel. Same dismiss affordances as Modal plus URL deeplinking. Good for filter forms, settings, detail views you want to bookmark.",
	},
	{
		Slug:  "toast",
		Name:  "Toast",
		Tag:   "Stack · SSE-pushed",
		Intro: "Server-side ToastBus queues notifications and broadcasts via SSE. The client renders a slide-in stack with hover-pause TTL, click-to-dismiss × buttons, and theme-driven animation. No URL state by design.",
	},
	{
		Slug:  "menu",
		Name:  "Menu",
		Tag:   "Dropdown · Keyboard",
		Intro: "Dropdown menu built on <details>. Arrow keys / Home / End / type-ahead navigate, Esc returns focus to the trigger, Tab closes + escapes. Items support icons, separators, danger styling, and RPC hooks.",
	},
	{
		Slug:  "sidebar",
		Name:  "Sidebar",
		Tag:   "Responsive nav",
		Intro: "Primary-nav column: inline ≥ md, hamburger + drawer < md, single content tree. Active-route highlighting from the current URL, nested groups via <details> that auto-open when a descendant matches.",
	},
	{
		Slug:  "layout",
		Name:  "Layout",
		Tag:   "Stack · Cluster · Grid",
		Intro: "Six small spatial primitives — Stack (column), Cluster (row), Grid (auto-fit), Center, Spacer, Box — that cover the boring layout decisions every page makes. One stylesheet, six wrappers.",
	},
	{
		Slug:  "card",
		Name:  "Card",
		Tag:   "Surface · Interactive",
		Intro: "Labelled <section> shell with header / body / footer regions. Three variants (elevated, outlined, flat) plus an interactive (linked) form whose entire surface activates.",
	},
	{
		Slug:  "image",
		Name:  "Optimized Image",
		Tag:   "Responsive · Lazy · CLS-safe",
		Intro: "Responsive <picture> with srcset, lazy loading, and mandatory Width/Height to eliminate layout shift. Decorative images opt in explicitly — no silent CLS regressions.",
	},
	{
		Slug:  "toggle",
		Name:  "Toggle controls",
		Tag:   "Checkbox · Radio · Switch",
		Intro: "Three labelled form controls wrapping native <input> elements. FieldErrors-aware, keyboard/screen-reader/form-POST ready without JavaScript.",
	},
	{
		Slug:  "tooltip",
		Name:  "Tooltip",
		Tag:   "Hover · Focus",
		Intro: "A CSS-only hover/focus tooltip with aria-describedby wiring. No JavaScript, no runtime callouts. Four placements (top default, bottom, left, right).",
	},
	{
		Slug:  "popover",
		Name:  "Popover",
		Tag:   "Anchored · Dismissible",
		Intro: "Click-triggered floating surface — like Modal without the backdrop dim or focus trap. Closes on Escape and click-outside. Use for help panels, share menus, per-row expanders.",
	},
	{
		Slug:  "tag",
		Name:  "Tag / Chip",
		Tag:   "Filter · Removable",
		Intro: "Interactive pill — linked for filter chips, removable for applied filters, status-variant coded to match StatusBadge. Distinct from StatusBadge: tags can be removed or linked.",
	},
	{
		Slug:  "spinner",
		Name:  "Spinner",
		Tag:   "Ring · Dots",
		Intro: "Inline CSS loading indicator. role=\"status\" + aria-busy announces 'loading' once; prefers-reduced-motion slows the spin rather than stopping it.",
	},
	{
		Slug:  "divider",
		Name:  "Divider",
		Tag:   "Horizontal · Vertical · Labelled",
		Intro: "Semantic separator. Plain horizontal dividers emit a native <hr>; vertical and labelled variants use role=\"separator\" so orientation / label gets announced.",
	},
	{
		Slug:  "fileupload",
		Name:  "File Upload",
		Tag:   "Drag-drop · Native",
		Intro: "Drag-and-drop file picker over a native <input type=\"file\">. Keyboard, screen-reader, and form-POST flows work without JavaScript; drag zone is progressive enhancement via data-fui-fileupload.",
	},
}

func (s *ComponentsIndexScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(componentEntries))
	for _, c := range componentEntries {
		cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
			Href: "/components/" + c.Slug,
			Content: render.Join(
				html.Strong(html.TextConfig{}, render.Text(c.Name)),
				html.Span(html.TextConfig{Class: "component-tag"}, render.Text(c.Tag)),
				html.Span(html.TextConfig{}, render.Text(c.Intro)),
			),
		}))
	}
	return render.Tag("div", nil,
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "core-ui",
			Title:   "Components",
			Subtitle: "Building blocks shipped in core-ui/. Every component on this site is rendered with itself — what you see is the dogfood.",
		}),
		ui.Section(ui.SectionConfig{
			Heading: "Available components",
		}, render.Tag("div", map[string]string{"class": "doc-list"}, cards...)),
	)
}
