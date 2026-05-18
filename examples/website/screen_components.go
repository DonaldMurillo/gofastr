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
	{
		Slug:  "slider",
		Name:  "Slider",
		Tag:   "Range · Live value",
		Intro: "Styled <input type=\"range\"> with optional live value display + Min/Max edge labels. Native keyboard support (Arrow/PageUp-Down/Home/End); ShowValue uses a small runtime hook to mirror the live value into <output>.",
	},
	{
		Slug:  "numberinput",
		Name:  "Number Input",
		Tag:   "Stepper · Min/Max",
		Intro: "Native <input type=\"number\"> flanked by explicit +/- buttons. The runtime increments by Step, clamps to Min/Max, and dispatches input + change events so form-RPC pipelines see the new value.",
	},
	{
		Slug:  "textarea",
		Name:  "Text Area",
		Tag:   "Multi-line · Autogrow",
		Intro: "Labelled multi-line text input. Help / Error states, error styling, and the typed Autogrow toggle that hooks into the runtime's data-fui-autogrow primitive to resize the height to fit content.",
	},
	{
		Slug:  "multiselect",
		Name:  "Multi-Select",
		Tag:   "Checkbox group · Chips",
		Intro: "Checkbox-group inside a <details> disclosure with chip rendering above. Native form-submit semantics (repeated Name); the runtime rebuilds the chip strip on every change and ×-removes unchecked options.",
	},
	{
		Slug:  "dropzone",
		Name:  "File Dropzone",
		Tag:   "Drag-drop · Preview",
		Intro: "Hero file-drop surface with the existing data-fui-fileupload drag-drop hook. Adds filename display after pick + an optional image preview strip via FileReader.",
	},
	{
		Slug:  "container",
		Name:  "Container",
		Tag:   "Layout · Max-width",
		Intro: "Max-width page wrapper with breakpoint-aware horizontal padding. Pairs with Stack/Cluster/Grid (internal spacing) — Container manages the outer bounds: gutter against the viewport.",
	},
	{
		Slug:  "disclosure",
		Name:  "Disclosure",
		Tag:   "Single details · Primitive",
		Intro: "Single styled <details>/<summary>. Native semantics; keyboard, screen reader, browser find-in-page expansion all work without JavaScript. The primitive Accordion composes groups of these.",
	},
	{
		Slug:  "timepicker",
		Name:  "Time Picker",
		Tag:   "Native · Themed chrome",
		Intro: "Native <input type=\"time\"> with theme chrome. Browser handles the actual picker; we own the label, 44×44 touch target, focus ring, and error state. Twin of ColorPicker.",
	},
	{
		Slug:  "rangeslider",
		Name:  "Range Slider",
		Tag:   "Dual thumb · Cross-clamp",
		Intro: "Two overlaid <input type=\"range\"> elements representing low + high bounds. Runtime cross-clamps min ≤ max on every input event. Form submits Name+\"-min\" and Name+\"-max\" so the server gets explicit lo/hi values.",
	},
	{
		Slug:  "taginput",
		Name:  "Tag Input",
		Tag:   "Free-form · Chips",
		Intro: "Free-form chips. Enter or comma commits; Backspace on empty removes the last. Each chip becomes its own <input type=hidden> sharing the same Name — standard repeated-key form submit.",
	},
	{
		Slug:  "toolbar",
		Name:  "Toolbar",
		Tag:   "role=toolbar · Grouped",
		Intro: "role=\"toolbar\" wrapper with optional logical groups. Groups render side-by-side with a thin separator; labeled groups become role=group + aria-label.",
	},
	{
		Slug:  "sparkline",
		Name:  "Sparkline",
		Tag:   "Inline SVG · Trend",
		Intro: "Inline SVG trend chart. Pure render — no JS, no hydration. Normalizes the y-axis to its own min/max so the silhouette is what matters. Pairs with StatCard.",
	},
	{
		Slug:  "piechart",
		Name:  "Pie / Donut Chart",
		Tag:   "SVG · Ratio",
		Intro: "Pure-SVG ratio chart. Slice colors cycle through the theme palette; InnerRadius (0–1) cuts the center out for a donut variant with optional center label.",
	},
	{
		Slug:  "barchart",
		Name:  "Bar Chart",
		Tag:   "SVG · Categorical",
		Intro: "Categorical SVG bar chart. Per-bar Color overrides apply. ShowAxis adds min/max value labels; ShowLabels adds x-axis category labels. Each bar's Label becomes a <title> for AT.",
	},
	{
		Slug:  "linechart",
		Name:  "Line Chart",
		Tag:   "SVG · Multi-series",
		Intro: "Multi-series SVG line chart. Each series can opt into Area fill. Palette cycles through theme tokens; per-series Color overrides apply. Optional Labels + ShowLegend.",
	},
	{
		Slug:  "jsonviewer",
		Name:  "JSON Viewer",
		Tag:   "Tree · Collapsible",
		Intro: "Collapsible tree for arbitrary Go values. Native <details>/<summary> — keyboard, find-in-page, and screen reader all work. OpenDepth controls initial expansion.",
	},
	{
		Slug:  "diffviewer",
		Name:  "Diff Viewer",
		Tag:   "Unified · Split",
		Intro: "Renders unified-diff text. Two modes — Unified (single column, +/− gutter) and Split (two-column with old/new side-by-side). Hunk + file headers styled distinctly.",
	},
	{
		Slug:  "markdown",
		Name:  "Markdown",
		Tag:   "Themed · core/markdown",
		Intro: "Themed wrapper over core/markdown. Wraps rendered HTML in a prose container so headings, lists, links, code blocks, blockquotes, and tables all get theme-token styling.",
	},
	{
		Slug:  "animatedcounter",
		Name:  "Animated Counter",
		Tag:   "IntersectionObserver · Reduced-motion",
		Intro: "Number that ticks from From to To over DurationMs. SSR renders the final value (no-JS + reduced-motion users see target immediately); the runtime hooks IntersectionObserver so animation fires exactly once when scrolled into view.",
	},
	{
		Slug:  "toc",
		Name:  "Table of Contents",
		Tag:   "Auto-built · Sticky · IntersectionObserver",
		Intro: "Auto-built from h2/h3 inside the Target selector. Renders a sticky nav with active-section tracking. No-JS users see in-document headings as the navigation primitive.",
	},
	{
		Slug:  "lightbox",
		Name:  "Lightbox",
		Tag:   "preset.Modal · Signal binding",
		Intro: "Click-to-zoom image gallery built on top of preset.Modal — ESC, click-outside, focus-trap all free. Each thumb's data-fui-deeplink mirrors src/alt onto the modal's signals on open. No bespoke runtime module.",
	},
	{
		Slug:  "notificationbell",
		Name:  "Notification Bell",
		Tag:   "preset.Popover · Live signals",
		Intro: "Bell button + unread-count badge + popover dropdown of recent items. Composes preset.Popover. Optional SignalUnread + SignalList bind the badge and list HTML to runtime signals for SSE-driven live updates.",
	},
	{
		Slug:  "sortablelist",
		Name:  "Sortable List",
		Tag:   "HTML5 drag · Keyboard reorder",
		Intro: "Drag-and-drop reorderable list with keyboard fallback (Space grab, Arrow Up/Down move, Esc cancel). After a successful reorder the runtime POSTs the new key sequence to RPCPath; non-2xx reverts the DOM.",
	},
	{
		Slug:  "globalsearch",
		Name:  "Global Search",
		Tag:   "/-shortcut · Combobox · Sticky",
		Intro: "Sticky inline search bar with /-shortcut focus + a Combobox-driven results dropdown. Distinct from CommandPalette (⌘K focus-trapped modal) — GlobalSearch is persistent and inline.",
	},
	{
		Slug:  "bottomsheet",
		Name:  "Bottom Sheet",
		Tag:   "Drawer variant · Mobile-friendly",
		Intro: "preset.BottomSheet — bottom-anchored sibling of Drawer. Same dismiss affordances (backdrop, ESC, click-outside, focus-trap), mounted on the bottom edge with slide-from-bottom animation. Ideal for mobile detail panels.",
	},
	{
		Slug:  "gallery",
		Name:  "Gallery",
		Tag:   "Grid / Strip / Masonry",
		Intro: "Standalone thumbnail surface — Grid (configurable Columns + Gap), Strip (scroll-snap row), Masonry (CSS-columns flow). Pairs with Lightbox (set Lightbox: \"<name>\" and items become triggers); works as plain links otherwise.",
	},
	{
		Slug:  "carousel",
		Name:  "Carousel",
		Tag:   "Scroll-snap · Prev/Next · AutoRotate",
		Intro: "Horizontal scroll-snap slider with Prev/Next + pagination dots + ArrowLeft/Right keyboard nav. Opt-in AutoRotate pauses on hover, focus, reduced-motion, and background tabs. Users can drag/swipe natively too.",
	},
	{
		Slug:  "skiplink",
		Name:  "SkipLink",
		Tag:   "Accessibility · WCAG 2.4.1",
		Intro: "Visually-hidden skip-navigation link that appears on keyboard focus, letting users jump past repetitive navigation to main content. Required for WCAG 2.1 Level A (Bypass Blocks).",
	},
	{
		Slug:  "themetoggle",
		Name:  "ThemeToggle",
		Tag:   "Dark mode · Color scheme",
		Intro: "Dark/light/auto toggle that persists to localStorage and applies immediately via colorscheme.js. Three variants: icon (sun/moon), label (Light/Dark text), pill (segmented Light/Auto/Dark).",
	},
	{
		Slug:  "sticky",
		Name:  "Sticky",
		Tag:   "Layout · position:sticky",
		Intro: "position:sticky wrapper with theme-token z-index and named offset presets. Sticks to top or bottom edge on scroll. Pure CSS, no runtime.",
	},
	{
		Slug:  "select",
		Name:  "Select",
		Tag:   "Form · <select>",
		Intro: "Labelled native <select> dropdown with FormField-style label, help, error, placeholder, and required marker. Custom chevron arrow via CSS.",
	},
	{
		Slug:  "aspectratio",
		Name:  "AspectRatio",
		Tag:   "Layout · aspect-ratio",
		Intro: "Pure-CSS aspect-ratio wrapper that prevents layout shift for images, videos, and embeds. Child is absolutely positioned to fill the box.",
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
