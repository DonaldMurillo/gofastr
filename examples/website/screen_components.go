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
	{
		Slug:  "kbd",
		Name:  "Kbd",
		Tag:   "Primitive · <kbd>",
		Intro: "Semantic <kbd> primitive in core-ui/html. Pairs with ShortcutHint for styled chord chips, or use inline for documentation prose.",
	},
	{
		Slug:  "avatargroup",
		Name:  "Avatar Group",
		Tag:   "Stack · Overflow",
		Intro: "Overlapping stack of avatars with a +N overflow chip when there are more people than Max. role=group + aria-label; Size propagates to children.",
	},
	{
		Slug:  "copybutton",
		Name:  "Copy Button",
		Tag:   "Clipboard · SR-announce",
		Intro: "Clipboard button bound by selector. Visible label swaps to \"Copied\" briefly; a role=\"status\" sibling announces the success via aria-live so AT users don't lose focus.",
	},
	{
		Slug:  "shortcuthint",
		Name:  "Shortcut Hint",
		Tag:   "Chord · OS-aware",
		Intro: "Keyboard chord as styled <kbd> chips. The Mod key resolves to ⌘ on Mac / Ctrl elsewhere via <html data-fui-os>. Hidden on touch-only devices.",
	},
	{
		Slug:  "segmented",
		Name:  "Segmented Control",
		Tag:   "Radiogroup · Sliding pill",
		Intro: "Native radio inputs styled as a pill toggle bar with a CSS-only sliding indicator. Browser handles arrow keys + Space/Enter; form-submittable without JS.",
	},
	{
		Slug:  "confirmaction",
		Name:  "Confirm Action",
		Tag:   "Alertdialog · Safe-default",
		Intro: "Trigger + alertdialog Modal preset for destructive confirmations. Cancel autofocuses by default; opt into AutofocusConfirm for non-destructive flows.",
	},
	{
		Slug:  "filterchipbar",
		Name:  "Filter Chip Bar",
		Tag:   "Toolbar · Removable",
		Intro: "role=toolbar wrapper of removable Tag chips above a table or search result. Each × dismiss + optional \"Clear all\" wire through the existing RPC + signal swap.",
	},
	{
		Slug:  "infinitescroll",
		Name:  "Infinite Scroll",
		Tag:   "Sentinel · noscript fallback",
		Intro: "role=feed container with IntersectionObserver-driven lazy fetch and an X-Gofastr-Infinite-Cursor end-signal. Ships a <noscript> Load more form for non-JS users.",
	},
	{
		Slug:  "combobox",
		Name:  "Combobox",
		Tag:   "Typeahead · RPC",
		Intro: "Debounced input bound to an RPC dropdown listbox. role=combobox + aria-activedescendant + arrow-key/Enter/Esc nav per WAI-ARIA Combobox 1.2.",
	},
	{
		Slug:  "tree",
		Name:  "Tree View",
		Tag:   "Recursive · Lazy-load",
		Intro: "WAI-ARIA tree with roving tabindex and optional RPC lazy-load on expand. Arrow keys, Home/End, Enter/Space, and type-ahead all handled at the runtime layer.",
	},
	{
		Slug:  "commandpalette",
		Name:  "Command Palette",
		Tag:   "⌘K · Modal+combobox",
		Intro: "Ctrl/Cmd+K overlay combining a focus-trapped dialog with an always-open combobox. Server returns ranked options; selected options can fire RPCs or push URL state.",
	},
	{
		Slug:  "banner",
		Name:  "Banner",
		Tag:   "InlineAlert · Dismissible",
		Intro: "Persistent in-page status strip — maintenance notices, billing alerts, deprecation warnings. Warn/Danger emit role=\"alert\"; Info/Success use role=\"status\". Optional Dismissible + localStorage-backed DismissID.",
	},
	{
		Slug:  "timeline",
		Name:  "Timeline",
		Tag:   "Event rail · Audit log",
		Intro: "Vertical event list on a rail. Each event: dot, label, optional time/meta, optional body. Renders as a semantic <ol>; rail and dots are CSS pseudo-elements so screen readers hear a clean ordered list.",
	},
	{
		Slug:  "steps",
		Name:  "Progress Steps",
		Tag:   "Step indicator · Horizontal/Vertical",
		Intro: "Linear step indicator showing completed, current, upcoming. aria-current=\"step\" on current; completed steps with Href become clickable back-navigation links. Horizontal default + vertical orientation.",
	},
	{
		Slug:  "rating",
		Name:  "Rating Input",
		Tag:   "1-N stars/hearts · No-JS",
		Intro: "Keyboard-accessible star (or heart) rating bound to a hidden radio group. Native browser radio semantics handle arrow keys + Space/Enter; hover preview is pure CSS via sibling selectors.",
	},
	{
		Slug:  "colorpicker",
		Name:  "Color Picker",
		Tag:   "Native · Themed chrome",
		Intro: "Styled wrapper around <input type=\"color\">. The browser handles the actual color UI; we own the label, the 44×44 swatch size, and the focus ring. Preset swatches are on the roadmap.",
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
