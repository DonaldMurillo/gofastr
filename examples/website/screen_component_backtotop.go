package main

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type BackToTopScreen struct{}

func (s *BackToTopScreen) ScreenTitle() string        { return "BackToTop" }
func (s *BackToTopScreen) ScreenDescription() string  { return "Scroll-past-threshold button with configurable icon, size, variant, and position." }
func (s *BackToTopScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *BackToTopScreen) Render() render.HTML {
	// Tall filler content so there's something to scroll past.
	filler := func() []render.HTML {
		var out []render.HTML
		for i := 0; i < 40; i++ {
			out = append(out, html.Paragraph(html.TextConfig{},
				render.Text(fmt.Sprintf("Paragraph %d — keep scrolling to see the BackToTop button appear in the bottom-right corner.", i+1))))
		}
		return out
	}

	// ── Default (primary, bottom-right, medium, chevron icon) ──
	defaultSrc := `ui.BackToTop(ui.BackToTopConfig{})`

	// ── Custom icon (arrow-up SVG) ──
	customIcon := ui.BackToTop(ui.BackToTopConfig{
		Icon: render.Raw(`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2l9 9h-6v11H9V11H3z"/></svg>`),
	})
	customIconSrc := "ui.BackToTop(ui.BackToTopConfig{\n" +
		"    Icon: render.Raw(`<svg>...arrow up...</svg>`),\n" +
		"})"

	// ── Size variants ──
	smBtn := ui.BackToTop(ui.BackToTopConfig{Size: ui.BackToTopSM})
	lgBtn := ui.BackToTop(ui.BackToTopConfig{Size: ui.BackToTopLG})
	sizeSrc := `ui.BackToTop(ui.BackToTopConfig{Size: ui.BackToTopSM})
ui.BackToTop(ui.BackToTopConfig{Size: ui.BackToTopLG})`

	// ── Variants ──
	secondaryBtn := ui.BackToTop(ui.BackToTopConfig{Variant: ui.BackToTopSecondary})
	ghostBtn := ui.BackToTop(ui.BackToTopConfig{Variant: ui.BackToTopGhost})
	variantSrc := `ui.BackToTop(ui.BackToTopConfig{Variant: ui.BackToTopSecondary})
ui.BackToTop(ui.BackToTopConfig{Variant: ui.BackToTopGhost})`

	// ── Positions ──
	blBtn := ui.BackToTop(ui.BackToTopConfig{Position: ui.BackToTopBottomLeft})
	trBtn := ui.BackToTop(ui.BackToTopConfig{Position: ui.BackToTopTopRight})
	posSrc := `ui.BackToTop(ui.BackToTopConfig{Position: ui.BackToTopBottomLeft})
ui.BackToTop(ui.BackToTopConfig{Position: ui.BackToTopTopRight})`

	// ── Threshold + offset ──
	thresholdBtn := ui.BackToTop(ui.BackToTopConfig{
		ThresholdPx: 200,
		Offset:      ui.BackToTopOffsetXL,
	})
	thresholdSrc := `ui.BackToTop(ui.BackToTopConfig{
    ThresholdPx: 200,
    Offset:      ui.BackToTopOffsetXL,
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("BackToTop")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Fixed-position button that appears after scrolling past a configurable threshold and smooth-scrolls to the top on click. Uses IntersectionObserver (no scroll listeners). Fully configurable: icon, size, color variant, corner position, edge offset, and scroll behavior.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("How to test")),
		render.Tag("p", nil, render.Text(
			"This page has tall filler content below. Scroll past the first few sections and watch the button appear. Each demo section places its own button — you'll see multiple if you scroll far enough.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Default")),
		demoFrame(ui.BackToTop(ui.BackToTopConfig{}), defaultSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Custom icon")),
		render.Tag("p", nil, render.Text("Pass any render.HTML as Icon to replace the default chevron. Works with inline SVGs, icon fonts, or text.")),
		demoFrame(customIcon, customIconSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sizes")),
		render.Tag("p", nil, render.Text("SM (2rem), MD (2.75rem, default), LG (3.5rem).")),
		demoFrame(ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Align: ui.AlignCenter}, smBtn, lgBtn), sizeSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Variants")),
		render.Tag("p", nil, render.Text("Primary (solid, default), Secondary (outlined surface), Ghost (transparent, hover only).")),
		demoFrame(ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Align: ui.AlignCenter}, secondaryBtn, ghostBtn), variantSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Positions")),
		render.Tag("p", nil, render.Text("Bottom-right (default), bottom-left, top-right, top-left.")),
		demoFrame(ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Align: ui.AlignCenter}, blBtn, trBtn), posSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Threshold + offset")),
		render.Tag("p", nil, render.Text("ThresholdPx controls when the button appears (default 400px). Offset controls distance from the viewport edge (none/sm/md/lg/xl).")),
		demoFrame(thresholdBtn, thresholdSrc),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Scroll target")),
		render.Tag("p", nil, render.Text("Set ScrollTarget to a CSS selector to scroll to a specific element instead of y=0.")),
		demoFrame(render.Text("ui.BackToTop(ui.BackToTopConfig{ScrollTarget: \"#main-content\"})"),
			`ui.BackToTop(ui.BackToTopConfig{ScrollTarget: "#main-content"})`),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Config reference")),
		render.Tag("dl", nil,
			render.Tag("dt", nil, render.Text("Position")),
			render.Tag("dd", nil, render.Text("BottomRight (default) | BottomLeft | TopRight | TopLeft")),
			render.Tag("dt", nil, render.Text("Icon")),
			render.Tag("dd", nil, render.Text("Any render.HTML — default is a chevron-up SVG")),
			render.Tag("dt", nil, render.Text("ThresholdPx")),
			render.Tag("dd", nil, render.Text("Scroll distance before showing (default: 400)")),
			render.Tag("dt", nil, render.Text("Smooth")),
			render.Tag("dd", nil, render.Text("BackToTopSmooth (default) | BackToTopInstant")),
			render.Tag("dt", nil, render.Text("Size")),
			render.Tag("dd", nil, render.Text("SM | MD (default) | LG")),
			render.Tag("dt", nil, render.Text("Variant")),
			render.Tag("dd", nil, render.Text("Primary (default) | Secondary | Ghost")),
			render.Tag("dt", nil, render.Text("Offset")),
			render.Tag("dd", nil, render.Text("None | SM | MD (default) | LG | XL — distance from viewport edge")),
			render.Tag("dt", nil, render.Text("ScrollTarget")),
			render.Tag("dd", nil, render.Text("CSS selector — scrolls element into view instead of y=0")),
			render.Tag("dt", nil, render.Text("Label")),
			render.Tag("dd", nil, render.Text("aria-label override (default: \"Back to top\")")),
		),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("↓ Scroll down to test")),
		render.Tag("div", nil, filler()...),

		// Real fixed BackToTop button (not inside a demo frame).
		ui.BackToTop(ui.BackToTopConfig{ThresholdPx: 400}),
	)
}
