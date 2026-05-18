package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Screens for the 10 new primitives. Each screen is the live demo
// AND the dogfooded reference page — the demo block is rendered with
// the same component it's documenting.

// ─── Layout ─────────────────────────────────────────────────────────

type LayoutScreen struct{}

func (s *LayoutScreen) ScreenTitle() string        { return "Layout" }
func (s *LayoutScreen) ScreenDescription() string  { return "Stack, Cluster, Grid, Center, Spacer, Box — six small spatial primitives." }
func (s *LayoutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *LayoutScreen) Render() render.HTML {
	demoBox := func(label string) render.HTML {
		return ui.Box(ui.BoxConfig{Pad: ui.BoxPadMD, Surface: true, Outlined: true},
			render.Text(label))
	}
	stack := ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		demoBox("Item A"), demoBox("Item B"), demoBox("Item C"))
	cluster := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Wrap: true},
		demoBox("design"), demoBox("system"), demoBox("primitive"),
		demoBox("layout"), demoBox("token"))
	grid := ui.Grid(ui.GridConfig{Min: "10rem", Gap: ui.GapMD},
		demoBox("1"), demoBox("2"), demoBox("3"), demoBox("4"))

	src := `ui.Stack(ui.StackConfig{Gap: ui.GapSM},
    box("Item A"), box("Item B"), box("Item C"))

ui.Cluster(ui.ClusterConfig{Wrap: true},
    box("design"), box("system"), box("primitive"))

ui.Grid(ui.GridConfig{Min: "10rem"},
    box("1"), box("2"), box("3"), box("4"))

ui.Center(ui.CenterConfig{MinHeight: "viewport"},
    body)

ui.Stack(ui.StackConfig{},
    label, ui.Spacer(), button)  // pushes button to bottom

ui.Box(ui.BoxConfig{Pad: ui.BoxPadLG, Surface: true},
    children...)`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Layout",
			"Six tiny wrappers — Stack, Cluster, Grid, Center, Spacer, Box — that cover the boring spatial decisions every page makes. All emit one shared stylesheet so the cost is one CSS link, not six."),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Stack — vertical column")),
		demoFrame(stack, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Cluster — horizontal row that wraps")),
		demoFrame(cluster, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Grid — auto-fit responsive")),
		demoFrame(grid, src),
	)
}

// ─── Card ───────────────────────────────────────────────────────────

type CardScreen struct{}

func (s *CardScreen) ScreenTitle() string        { return "Card" }
func (s *CardScreen) ScreenDescription() string  { return "Labelled content shell with header/body/footer and optional interactive variant." }
func (s *CardScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CardScreen) Render() render.HTML {
	plain := ui.Card(ui.CardConfig{
		Heading:     "Recent activity",
		Description: "Last 7 days of changes",
	},
		html.Paragraph(html.TextConfig{}, render.Text("Karen updated the pricing copy 3 hours ago.")),
		html.Paragraph(html.TextConfig{}, render.Text("Marco merged the homepage redesign yesterday.")),
	)

	outlined := ui.Card(ui.CardConfig{
		Variant: ui.CardOutlined,
		Heading: "Outlined variant",
		Footer:  ui.Button(ui.ButtonConfig{Label: "Open", Variant: ui.ButtonSecondary}),
	},
		html.Paragraph(html.TextConfig{}, render.Text("Same shape, hairline border instead of shadow.")),
	)

	linked := ui.Card(ui.CardConfig{
		Heading:     "Interactive card",
		Description: "Click the whole surface — the wrapper is an <a>.",
		Href:        "/components/",
	})

	grid := ui.Grid(ui.GridConfig{Min: "16rem", Gap: ui.GapMD},
		plain, outlined, linked)

	src := `ui.Card(ui.CardConfig{
    Heading: "Recent activity",
    Description: "Last 7 days of changes",
}, body...)

ui.Card(ui.CardConfig{
    Variant: ui.CardOutlined,
    Heading: "Outlined",
    Footer:  ui.Button(ui.ButtonConfig{Label: "Open"}),
}, body...)

ui.Card(ui.CardConfig{
    Heading: "Interactive",
    Href:    "/somewhere",
})`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Card",
			"A labelled <section> shell with header/body/footer regions. Three variants — elevated (default), outlined, flat — plus an interactive (linked) form that wraps the whole surface in an <a>."),
		// Each Card's heading is h3; an h2 between the page h1 and the
		// h3 stack keeps WCAG 1.3.1 heading order monotonic.
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Variants")),
		demoFrame(grid, src),
	)
}

// ─── OptimizedImage ─────────────────────────────────────────────────

type OptimizedImageScreen struct{}

func (s *OptimizedImageScreen) ScreenTitle() string        { return "Optimized Image" }
func (s *OptimizedImageScreen) ScreenDescription() string  { return "Responsive picture with srcset, lazy loading, and CLS-safe width/height." }
func (s *OptimizedImageScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *OptimizedImageScreen) Render() render.HTML {
	// SVG placeholder, embedded data URL — keeps the demo self-contained
	// without binary assets in the repo.
	placeholder := "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 320 180'><defs><linearGradient id='g' x1='0' x2='1'><stop offset='0' stop-color='%234F46E5'/><stop offset='1' stop-color='%2316A34A'/></linearGradient></defs><rect width='320' height='180' fill='url(%23g)'/><text x='50%25' y='55%25' text-anchor='middle' font-size='28' fill='white' font-family='system-ui'>16:9</text></svg>"
	square := "data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 200 200'><rect width='200' height='200' fill='%23DC2626'/><text x='50%25' y='52%25' text-anchor='middle' font-size='28' fill='white' font-family='system-ui'>1:1</text></svg>"

	hero := ui.OptimizedImage(ui.OptimizedImageConfig{
		Src: placeholder, Alt: "Hero placeholder",
		Width: 320, Height: 180,
		Aspect: ui.ImageAspect16x9, Rounded: true,
	})
	avatar := ui.OptimizedImage(ui.OptimizedImageConfig{
		Src: square, Alt: "Square placeholder",
		Width: 200, Height: 200,
		Aspect: ui.ImageAspectSquare, Rounded: true,
	})

	demos := ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Wrap: true, Align: ui.AlignStart},
		hero, avatar)

	src := `ui.OptimizedImage(ui.OptimizedImageConfig{
    Src:    "/hero.jpg",
    Alt:    "Sunset over the ocean",
    Width:  1600, Height: 900,
    Aspect: ui.ImageAspect16x9,
    Sources: []ui.ImageSource{
        {URL: "/hero-800.jpg",  Width:  800},
        {URL: "/hero-1600.jpg", Width: 1600},
        {URL: "/hero-2400.jpg", Width: 2400},
    },
    Sizes: "(min-width: 1024px) 1024px, 100vw",
    Eager: true, HighPriority: true,
})

// Width + Height are MANDATORY — the framework will not
// silently emit a CLS-shifting image.`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Optimized Image",
			"A responsive <picture> with srcset, lazy loading, and intrinsic Width/Height to eliminate Cumulative Layout Shift. Width and Height are mandatory — the framework refuses to ship layout-shifting images."),
		demoFrame(demos, src),
	)
}

// ─── Toggle ─────────────────────────────────────────────────────────

type ToggleScreen struct{}

func (s *ToggleScreen) ScreenTitle() string        { return "Toggle controls" }
func (s *ToggleScreen) ScreenDescription() string  { return "Checkbox, Radio, and Switch — labelled, FieldErrors-aware." }
func (s *ToggleScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ToggleScreen) Render() render.HTML {
	checks := ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		ui.Checkbox(ui.ToggleConfig{Name: "demo-newsletter", Label: "Send me product updates", Checked: true}),
		ui.Checkbox(ui.ToggleConfig{Name: "demo-promo", Label: "Send marketing emails", Help: "We send at most one per week."}),
		ui.Checkbox(ui.ToggleConfig{Name: "demo-tos", Label: "Agree to the terms", Error: "You must agree before continuing."}),
		ui.Checkbox(ui.ToggleConfig{Name: "demo-cb-disabled", Label: "Locked option (disabled)", Checked: true, Disabled: true}),
	)
	radios := ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		ui.Radio(ui.ToggleConfig{Name: "demo-plan", Value: "free", Label: "Free", Checked: true}),
		ui.Radio(ui.ToggleConfig{Name: "demo-plan", Value: "pro", Label: "Pro — $12/mo"}),
		ui.Radio(ui.ToggleConfig{Name: "demo-plan", Value: "team", Label: "Team — $48/mo", Help: "Includes 5 seats."}),
		ui.Radio(ui.ToggleConfig{Name: "demo-plan", Value: "enterprise", Label: "Enterprise (contact sales)", Disabled: true}),
	)
	switches := ui.Stack(ui.StackConfig{Gap: ui.GapSM},
		ui.Switch(ui.ToggleConfig{Name: "demo-wifi", Label: "Wi-Fi", Checked: true}),
		ui.Switch(ui.ToggleConfig{Name: "demo-bt", Label: "Bluetooth"}),
		ui.Switch(ui.ToggleConfig{Name: "demo-airplane", Label: "Airplane mode", Disabled: true}),
	)

	body := ui.Grid(ui.GridConfig{Min: "16rem", Gap: ui.GapLG},
		ui.Card(ui.CardConfig{Heading: "Checkbox"}, checks),
		ui.Card(ui.CardConfig{Heading: "Radio group"}, radios),
		ui.Card(ui.CardConfig{Heading: "Switch"}, switches),
	)

	src := `ui.Checkbox(ui.ToggleConfig{
    Name: "newsletter", Label: "Send me updates",
    Help: "Weekly digest. Unsubscribe any time.",
})

ui.Radio(ui.ToggleConfig{
    Name: "plan", Value: "pro", Label: "Pro — $12/mo",
})

ui.Switch(ui.ToggleConfig{
    Name: "wifi", Label: "Wi-Fi", Checked: true,
})`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Toggle controls",
			"Three labelled, FieldErrors-aware form controls — Checkbox, Radio, Switch. All wrap a native <input> with a properly associated <label>, so keyboard, screen-reader, and form-POST flows work without JavaScript."),
		// The grid below renders three Cards whose headings are h3; an
		// h2 group heading between this h1 (from primitiveLede) and the
		// h3 cards keeps the WCAG 1.3.1 heading order monotonic.
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Controls")),
		demoFrame(body, src),
	)
}

// ─── Tooltip ────────────────────────────────────────────────────────

type TooltipScreen struct{}

func (s *TooltipScreen) ScreenTitle() string        { return "Tooltip" }
func (s *TooltipScreen) ScreenDescription() string  { return "CSS-only hover/focus reveal with aria-describedby wiring." }
func (s *TooltipScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TooltipScreen) Render() render.HTML {
	row := ui.Cluster(ui.ClusterConfig{Gap: ui.GapLG, Wrap: true},
		ui.Tooltip(ui.TooltipConfig{Text: "Copy to clipboard"},
			ui.Button(ui.ButtonConfig{Label: "Copy", Variant: ui.ButtonSecondary})),
		ui.Tooltip(ui.TooltipConfig{Text: "Below the trigger", Placement: ui.TooltipBottom},
			ui.Button(ui.ButtonConfig{Label: "Below", Variant: ui.ButtonSecondary})),
		ui.Tooltip(ui.TooltipConfig{Text: "Anchored left", Placement: ui.TooltipLeft},
			ui.Button(ui.ButtonConfig{Label: "Left", Variant: ui.ButtonSecondary})),
		ui.Tooltip(ui.TooltipConfig{Text: "Anchored right", Placement: ui.TooltipRight},
			ui.Button(ui.ButtonConfig{Label: "Right", Variant: ui.ButtonSecondary})),
	)

	src := `ui.Tooltip(ui.TooltipConfig{
    Text: "Copy to clipboard",
}, ui.Button(ui.ButtonConfig{Label: "Copy"}))

ui.Tooltip(ui.TooltipConfig{
    Text:      "Below the trigger",
    Placement: ui.TooltipBottom,
}, trigger)`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Tooltip",
			"A CSS-only hover/focus tooltip. No JavaScript, no runtime callouts. Wraps the trigger and pops a small message via :hover / :focus-within. The trigger's aria-describedby is auto-wired so screen readers announce the tooltip alongside it."),
		demoFrame(row, src),
	)
}

// ─── Popover ────────────────────────────────────────────────────────

type PopoverScreen struct{}

func (s *PopoverScreen) ScreenTitle() string        { return "Popover" }
func (s *PopoverScreen) ScreenDescription() string  { return "Click-triggered floating surface with no backdrop dim." }
func (s *PopoverScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PopoverScreen) Render() render.HTML {
	// Trigger that opens the popover at the corner anchor declared on
	// the widget (no per-trigger placement).
	cornerBtn := render.Tag("button", map[string]string{
		"class":         "cta-button",
		"data-fui-open": "components-popover",
	}, render.Text("Open at TopRight (no anchor)"))

	// Anchor triggers — each opens the same popover positioned next
	// to itself. The runtime measures the trigger + popover and auto-
	// flips when a side would overflow the viewport.
	// data-fui-deeplink="from=X" seeds the popover body's `from`
	// signal so the body can show "Opened from: X" — visual confirmation
	// of which trigger is currently active.
	anchorBtn := func(side, label, key string) render.HTML {
		attrs := map[string]string{
			"class":                   "cta-button popover-anchor-btn",
			"data-fui-open":           "components-popover",
			"data-fui-popover-anchor": side,
			"data-fui-deeplink":       "from=" + key,
		}
		return render.Tag("button", attrs, render.Text(label))
	}

	// Layout for the "all four sides" row — a centered grid so each
	// trigger has room for the popover on its requested side.
	sideRow := render.Tag("div", map[string]string{"class": "popover-demo-sides"},
		anchorBtn("top", "Anchor: top", "top"),
		anchorBtn("right", "Anchor: right", "right"),
		anchorBtn("bottom", "Anchor: bottom", "bottom"),
		anchorBtn("left", "Anchor: left", "left"),
	)

	// Edge demo — triggers pinned to the four corners of a dedicated
	// frame, so each forces a different auto-flip path even at
	// modest viewport sizes. The frame has its own min-height so the
	// "bottom-right" trigger is always near the lower edge of the
	// scroll viewport.
	edgeFrame := render.Tag("div", map[string]string{"class": "popover-demo-edges"},
		render.Tag("div", map[string]string{"class": "popover-demo-edges__inner"},
			render.Tag("div", map[string]string{"class": "popover-demo-edges__tl"},
				anchorBtn("auto", "Top-left (auto)", "edge-tl")),
			render.Tag("div", map[string]string{"class": "popover-demo-edges__tr"},
				anchorBtn("auto", "Top-right (auto)", "edge-tr")),
			render.Tag("div", map[string]string{"class": "popover-demo-edges__bl"},
				anchorBtn("auto", "Bottom-left (auto)", "edge-bl")),
			render.Tag("div", map[string]string{"class": "popover-demo-edges__br"},
				anchorBtn("auto", "Bottom-right (auto)", "edge-br")),
		),
	)

	demoBlock := ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.Section(ui.SectionConfig{Heading: "Corner-anchored (default)",
			Description: "Without data-fui-popover-anchor, the popover renders at the widget's declared Position (here, TopRight)."},
			cornerBtn,
		),
		ui.Section(ui.SectionConfig{Heading: "All four sides",
			Description: "Each button asks for a specific side. The popover positions itself next to the trigger with a small arrow pointing back; the originating button stays highlighted while the popover is open."},
			sideRow,
		),
		ui.Section(ui.SectionConfig{Heading: "Auto-flip — viewport corners",
			Description: "Four triggers pinned to the corners of a frame with auto-anchor. Top-* should flip the popover to BOTTOM (or right) since BOTTOM-edge would overflow; Bottom-* should flip to TOP. Try resizing the window — the popover snaps to a new side on every resize."},
			edgeFrame,
		),
	)

	src := `// Register once at startup.
p := preset.Popover("share-popover").
    LabelledBy("share-popover-title").
    Slot("body", &SharePopoverBody{}).
    Build()
widget.Mount(r, &p)

// Corner-anchored: opens at the widget's declared Position.
<button data-fui-open="share-popover">Share</button>

// Anchored to the trigger: positions next to THIS button,
// auto-flipping when the preferred side would overflow.
<button data-fui-open="share-popover"
        data-fui-popover-anchor="bottom">Share</button>

// Sides: "top", "bottom", "left", "right", "auto" (default).
// Auto tries bottom first, then top, right, left.`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Popover",
			"A click-triggered floating surface — like Modal but without the backdrop dim or focus trap. By default it renders at the widget's declared Position (e.g. TopRight). Add data-fui-popover-anchor to a trigger to position the popover next to it; the runtime measures both rects and auto-flips when the preferred side would overflow the viewport."),
		demoBlock,
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("API")),
		ui.CodeBlock(ui.CodeBlockConfig{Code: src, Language: "go"}),
	)
}

// ─── Tag / Chip ─────────────────────────────────────────────────────

type TagScreen struct{}

func (s *TagScreen) ScreenTitle() string        { return "Tag / Chip" }
func (s *TagScreen) ScreenDescription() string  { return "Interactive pill — filter chips, applied filters, multi-select selections." }
func (s *TagScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TagScreen) Render() render.HTML {
	plain := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Wrap: true},
		ui.Tag(ui.TagConfig{Label: "design"}),
		ui.Tag(ui.TagConfig{Label: "research"}),
		ui.Tag(ui.TagConfig{Label: "shipped", Variant: ui.StatusSuccess}),
		ui.Tag(ui.TagConfig{Label: "in review", Variant: ui.StatusInfo}),
		ui.Tag(ui.TagConfig{Label: "blocked", Variant: ui.StatusDanger}),
		ui.Tag(ui.TagConfig{Label: "needs design", Variant: ui.StatusWarning}),
	)
	links := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Wrap: true},
		ui.Tag(ui.TagConfig{Label: "filter:design", Href: "/components/tag?tag=design"}),
		ui.Tag(ui.TagConfig{Label: "filter:engineering", Href: "/components/tag?tag=engineering"}),
	)
	removable := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Wrap: true},
		ui.Tag(ui.TagConfig{Label: "design", Variant: ui.StatusInfo, Dismiss: "/components/tag/remove?t=design"}),
		ui.Tag(ui.TagConfig{Label: "Q4 launch", Variant: ui.StatusNeutral, Dismiss: "/components/tag/remove?t=q4"}),
	)

	body := ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.Section(ui.SectionConfig{Heading: "Variants"}, plain),
		ui.Section(ui.SectionConfig{Heading: "Linked (filter chip)"}, links),
		ui.Section(ui.SectionConfig{Heading: "Removable (× fires an RPC)"}, removable),
	)

	src := `ui.Tag(ui.TagConfig{Label: "design"})
ui.Tag(ui.TagConfig{Label: "shipped", Variant: ui.StatusSuccess})

// Linked
ui.Tag(ui.TagConfig{Label: "filter:design", Href: "/?tag=design"})

// Removable — × button fires an RPC
ui.Tag(ui.TagConfig{
    Label:   "design",
    Variant: ui.StatusInfo,
    Dismiss: "/filters/remove?id=design",
})`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Tag / Chip",
			"An interactive pill. Distinct from StatusBadge — Tags can be linked (filter chip) or removable (× fires an RPC), and they compose the same StatusVariant set so status-coded tags match the rest of the system."),
		demoFrame(body, src),
	)
}

// ─── Spinner ────────────────────────────────────────────────────────

type SpinnerScreen struct{}

func (s *SpinnerScreen) ScreenTitle() string        { return "Spinner" }
func (s *SpinnerScreen) ScreenDescription() string  { return "Inline CSS loading indicator with size + variant." }
func (s *SpinnerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *SpinnerScreen) Render() render.HTML {
	rings := ui.Cluster(ui.ClusterConfig{Gap: ui.GapXL, Wrap: true, Align: ui.AlignCenter},
		ui.Spinner(ui.SpinnerConfig{Size: ui.SpinnerSm, Label: "Loading…"}),
		ui.Spinner(ui.SpinnerConfig{Label: "Loading…"}),
		ui.Spinner(ui.SpinnerConfig{Size: ui.SpinnerLg, Label: "Loading…"}),
	)
	dots := ui.Cluster(ui.ClusterConfig{Gap: ui.GapXL, Wrap: true, Align: ui.AlignCenter},
		ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerDots, Size: ui.SpinnerSm}),
		ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerDots}),
		ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerDots, Size: ui.SpinnerLg}),
	)
	grid := ui.Cluster(ui.ClusterConfig{Gap: ui.GapXL, Wrap: true, Align: ui.AlignCenter},
		ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerGrid, Size: ui.SpinnerSm, Label: "Working"}),
		ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerGrid, Label: "Working"}),
		ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerGrid, Size: ui.SpinnerLg, Label: "Working"}),
	)
	inline := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Wrap: true, Align: ui.AlignCenter},
		render.Text("Saving"),
		ui.Spinner(ui.SpinnerConfig{Size: ui.SpinnerSm, Inline: true, Label: "Saving"}),
	)

	// Reduced-motion preview — scoped to this block via .demo-spinner-reduced
	// so users without OS-level prefers-reduced-motion can still see
	// the calmer animation pace the framework applies to that audience.
	reducedDemo := render.Tag("div", map[string]string{"class": "demo-spinner-reduced"},
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapXL, Wrap: true, Align: ui.AlignCenter},
			ui.Spinner(ui.SpinnerConfig{Label: "Loading"}),
			ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerDots, Label: "Loading"}),
			ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerGrid, Label: "Loading"}),
		),
	)

	body := ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.Section(ui.SectionConfig{Heading: "Ring (default)"}, rings),
		ui.Section(ui.SectionConfig{Heading: "Dots"}, dots),
		ui.Section(ui.SectionConfig{Heading: "Grid (3×3)",
			Description: "Diagonal-ripple cells for long-running operations."},
			grid),
		ui.Section(ui.SectionConfig{Heading: "Inline with text"}, inline),
		ui.Section(ui.SectionConfig{
			Heading:     "Reduced-motion preview",
			Description: "Same three variants, but the surrounding block forces animation-duration to 3s. This previews what users with prefers-reduced-motion: reduce see — calmer, slower motion that still signals 'busy' without flicker."},
			reducedDemo),
	)

	src := `ui.Spinner(ui.SpinnerConfig{Label: "Loading…"})
ui.Spinner(ui.SpinnerConfig{Size: ui.SpinnerLg})
ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerDots})
ui.Spinner(ui.SpinnerConfig{Variant: ui.SpinnerGrid, Size: ui.SpinnerLg})

// Sits next to text via Inline=true.
ui.Spinner(ui.SpinnerConfig{Inline: true, Size: ui.SpinnerSm})`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Spinner",
			"A CSS-only loading indicator. role=\"status\" + aria-busy announces 'loading' once; prefers-reduced-motion slows the animation rather than stopping it. Three visual variants (ring, dots, grid), three sizes."),
		demoFrame(body, src),
	)
}

// ─── Divider ────────────────────────────────────────────────────────

type DividerScreen struct{}

func (s *DividerScreen) ScreenTitle() string        { return "Divider" }
func (s *DividerScreen) ScreenDescription() string  { return "Horizontal and vertical separators with optional inline label." }
func (s *DividerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DividerScreen) Render() render.HTML {
	body := ui.Stack(ui.StackConfig{Gap: ui.GapMD},
		render.Text("Section A content"),
		ui.Divider(ui.DividerConfig{}),
		render.Text("Section B content"),
		ui.Divider(ui.DividerConfig{Label: "OR"}),
		render.Text("Section C content"),
	)
	vertical := ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Align: ui.AlignCenter},
		render.Text("Left"),
		ui.Divider(ui.DividerConfig{Orientation: ui.DividerVertical}),
		render.Text("Middle"),
		ui.Divider(ui.DividerConfig{Orientation: ui.DividerVertical}),
		render.Text("Right"),
	)

	src := `ui.Divider(ui.DividerConfig{})            // <hr>
ui.Divider(ui.DividerConfig{Label: "OR"})  // labelled
ui.Divider(ui.DividerConfig{Orientation: ui.DividerVertical})`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Divider",
			"A semantic separator. Plain horizontal dividers use the native <hr> element; vertical or labelled dividers use role=\"separator\" so the orientation / label gets announced."),
		demoFrame(body, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Vertical")),
		demoFrame(vertical, `ui.Divider(ui.DividerConfig{Orientation: ui.DividerVertical})`),
	)
}

// ─── FileUpload ─────────────────────────────────────────────────────

type FileUploadScreen struct{}

func (s *FileUploadScreen) ScreenTitle() string        { return "File Upload" }
func (s *FileUploadScreen) ScreenDescription() string  { return "Drag-drop file picker over a native <input type=file>." }
func (s *FileUploadScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *FileUploadScreen) Render() render.HTML {
	// Live preview demo — picker reveals the picked file names + size,
	// and shows a thumbnail for the first picked image. Wrapped in a
	// <form> with data-fui-rpc so submitting fires a real POST whose
	// server response lands in the .demo-upload-result island.
	picker := ui.FileUpload(ui.FileUploadConfig{
		Name:      "files",
		Label:     "Pick or drop files",
		Accept:    "image/*,.pdf,.docx,.md,.txt",
		Multiple:  true,
		Help:      "Images preview as a thumbnail. Up to 5 MB each on this demo.",
		MaxSizeMB: 5,
	})

	pickerForm := render.Tag("form", map[string]string{
		"method":               "POST",
		"action":               "/components/fileupload/echo",
		"enctype":              "multipart/form-data",
		"data-fui-rpc":         "/components/fileupload/echo",
		"data-fui-rpc-method":  "POST",
		"data-fui-rpc-signal":  "fileupload-echo",
	},
		picker,
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Wrap: true, Align: ui.AlignCenter},
			ui.Button(ui.ButtonConfig{Label: "Upload", Type: "submit"}),
			html.Span(html.TextConfig{Class: "fileupload-hint"}, render.Text("(POSTs as multipart/form-data — the response lands below)")),
		),
	)

	// Island where the server's response renders. data-fui-signal=
	// "fileupload-echo" + mode=html means the RPC response body
	// replaces this region's innerHTML — no full reload, no extra
	// fetches.
	result := render.Tag("div", map[string]string{
		"class":                "demo-upload-result",
		"data-fui-signal":      "fileupload-echo",
		"data-fui-signal-mode": "html",
	}, render.HTML(`<p class="demo-upload-result__empty">Nothing uploaded yet. Pick or drop files above, then click <strong>Upload</strong>.</p>`))

	// Other variants showing config surface (single, error state).
	single := ui.FileUpload(ui.FileUploadConfig{
		Name:      "demo-doc",
		Label:     "Project document",
		Accept:    ".pdf,.docx,.md",
		Help:      "PDF, Word, or Markdown",
		MaxSizeMB: 10,
	})
	errored := ui.FileUpload(ui.FileUploadConfig{
		Name:  "demo-err",
		Label: "Failed upload",
		Error: "File exceeded the 5 MB limit.",
	})

	otherStates := ui.Grid(ui.GridConfig{Min: "18rem", Gap: ui.GapLG},
		single, errored,
	)

	full := ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.Section(ui.SectionConfig{
			Heading:     "Live flow — pick, preview, submit",
			Description: "Click the zone OR drag a file onto it. The picked filenames + sizes render below, with a thumbnail for the first image. Click Upload to POST the form; the server's response replaces the result island."},
			pickerForm,
			result,
		),
		ui.Section(ui.SectionConfig{
			Heading:     "Other states",
			Description: "Single-file picker with size + help; error state."},
			otherStates),
	)

	src := `// Wire the form once at app startup.
fwApp.Router.Post("/upload", http.HandlerFunc(uploadHandler))

// In your screen:
form := render.Tag("form", map[string]string{
    "data-fui-rpc":        "/upload",
    "data-fui-rpc-method": "POST",
    "data-fui-rpc-signal": "upload-result",
    "enctype":             "multipart/form-data",
},
    ui.FileUpload(ui.FileUploadConfig{
        Name: "files", Label: "Pick or drop files",
        Accept: "image/*,.pdf", Multiple: true,
        MaxSizeMB: 5,
    }),
    ui.Button(ui.ButtonConfig{Label: "Upload", Type: "submit"}),
)

// The response island the handler updates.
<div data-fui-signal="upload-result" data-fui-signal-mode="html"></div>

// data-fui-fileupload (set by ui.FileUpload itself) tells the
// runtime to wire dragover/dragleave/drop and render a filename
// + size summary into .ui-fileupload__filename — image files
// also get a 96px thumbnail.`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("File Upload",
			"A labelled file picker with a drag-and-drop hot zone. The native <input type=\"file\"> is the source of truth — keyboard, screen-reader, and form-POST all work without JavaScript. After a pick or drop, the runtime renders a filename + size summary (with a thumbnail for the first image) inside the zone, then the surrounding form can POST as multipart/form-data."),
		demoFrame(full, src),
	)
}

// ─── helpers ─────────────────────────────────────────────────────────

func backLink() render.HTML {
	return render.Tag("a", map[string]string{
		"href":  "/components/",
		"class": "doc-back",
	}, render.Text("← Components"))
}

func primitiveLede(title, body string) render.HTML {
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1}, render.Text(title)),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(body)),
	)
}
