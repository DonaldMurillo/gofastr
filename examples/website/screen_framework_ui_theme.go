package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

type ThemeSwapDemoScreen struct{}

func (s *ThemeSwapDemoScreen) ScreenTitle() string        { return "Theme swap" }
func (s *ThemeSwapDemoScreen) ScreenDescription() string  { return "One token override re-skins every component." }
func (s *ThemeSwapDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ThemeSwapDemoScreen) Render() render.HTML {
	radios := render.Tag("fieldset", map[string]string{"class": "theme-swap__picker"},
		render.Tag("legend", nil, render.Text("Pick a primary color")),
		themeOption("indigo", "Indigo (default)", true),
		themeOption("teal", "Teal", false),
		themeOption("rose", "Rose", false),
		themeOption("amber", "Amber", false),
	)

	preview := render.Tag("div", map[string]string{"class": "theme-swap__preview"},
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "preview", Title: "Customer dashboard",
			Subtitle: "Every primary-colored thing on this page is driven by a single CSS custom property.",
		}),
		render.Tag("div", map[string]string{"class": "theme-swap__row"},
			ui.StatCard(ui.StatCardConfig{Label: "Active", Value: "12,483", Trend: "+8%", Direction: ui.TrendUp}),
			ui.StatCard(ui.StatCardConfig{Label: "Errors", Value: "47", Trend: "-12%", Direction: ui.TrendDown}),
			ui.StatCard(ui.StatCardConfig{Label: "Latency p99", Value: "142ms", Trend: "stable", Direction: ui.TrendFlat}),
		),
		render.Tag("div", map[string]string{"class": "theme-swap__row"},
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Active", Variant: ui.StatusSuccess}),
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Pending", Variant: ui.StatusWarning}),
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Failed", Variant: ui.StatusDanger}),
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Info", Variant: ui.StatusInfo}),
		),
		ui.Callout(ui.CalloutConfig{Title: "Heads up", Variant: ui.StatusInfo},
			render.Text("Callouts and Buttons inherit --color-primary too — flip the radio to confirm.")),
		render.Tag("div", map[string]string{"class": "theme-swap__row"},
			elements.Button(elements.ButtonConfig{Label: "Primary action", Class: "ui-button"}),
			ui.DangerButton(ui.DangerButtonConfig{Label: "Destructive"}),
		),
	)

	return render.Tag("main", nil,
		render.Tag("a", map[string]string{"href": "/framework-ui/", "class": "doc-back"},
			render.Text("← Framework UI")),
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "framework/ui/theme", Title: "Theme swap",
			Subtitle: "Every framework/ui component reads its color from --color-primary. Pick a different color below — the whole preview re-skins instantly. Pure CSS via :has(); no JS, no recompile.",
		}),
		ui.Section(ui.SectionConfig{
			Heading:     "Try it",
			Description: "The radio buttons drive a CSS rule that overrides --color-primary on this preview's wrapper via :has().",
		},
			render.Tag("div", map[string]string{"class": "theme-swap"}, radios, preview),
		),
		ui.Section(ui.SectionConfig{
			Heading: "How it works",
			Description: "framework/ui/theme.Default(theme.Overrides{Primary: \"#14B8A6\"}) returns a style.Theme. The host emits :root custom properties; every component references them via var(). To re-skin, override one token — no component code changes.",
		}),
	)
}

func themeOption(value, label string, checked bool) render.HTML {
	id := "theme-" + value
	inputAttrs := elements.Attrs{
		"type":  "radio",
		"name":  "theme",
		"value": value,
		"id":    id,
	}
	if checked {
		inputAttrs["checked"] = "checked"
	}
	return render.Tag("div", map[string]string{"class": "theme-swap__option"},
		render.Tag("input", inputAttrs),
		render.Tag("label", map[string]string{"for": id}, render.Text(label)),
	)
}
