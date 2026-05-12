package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// Dark is a registered theme override the framework-ui themed-demo
// page uses to scope its right column to a dark palette. Real apps
// would register their app-specific overrides the same way at
// package init.
var Dark = style.RegisterThemeOverride(darkTheme())

func darkTheme() style.Theme {
	t := style.DefaultTheme()
	t.Colors.Background = style.Color{Name: "background", Value: "#0a0a0a"}
	t.Colors.Surface = style.Color{Name: "surface", Value: "#18181b"}
	t.Colors.SurfaceSoft = style.Color{Name: "surface-soft", Value: "#27272a"}
	t.Colors.Text = style.Color{Name: "text", Value: "#f4f4f5"}
	t.Colors.TextMuted = style.Color{Name: "text-muted", Value: "#a1a1aa"}
	t.Colors.TextSubtle = style.Color{Name: "text-subtle", Value: "#71717a"}
	t.Colors.Border = style.Color{Name: "border", Value: "#3f3f46"}
	t.Colors.BorderStrong = style.Color{Name: "border-strong", Value: "#52525b"}
	t.Colors.Primary = style.Color{Name: "primary", Value: "#a78bfa"}
	t.Colors.PrimaryFg = style.Color{Name: "primary-fg", Value: "#1e1b4b"}
	return t
}

// ThemedDemoScreen renders two side-by-side panels — the default
// (light) theme on the left, a Dark-overridden subtree on the right.
// Every component inside ui.Themed(Dark, ...) reads var(--color-*)
// from the .fui-theme-<hash> block in app.css, so the same component
// code paints with different colors on each side.
type ThemedDemoScreen struct{}

func (s *ThemedDemoScreen) ScreenTitle() string        { return "Section-level theme overrides" }
func (s *ThemedDemoScreen) ScreenDescription() string  { return "Wrap any subtree with ui.Themed(ref, …) for a scoped dark mode, brand sub-skin, or per-tenant variant." }
func (s *ThemedDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ThemedDemoScreen) Render() render.HTML {
	header := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow:  "framework/ui",
		Title:    "Section-level theme overrides",
		Subtitle: "ui.Themed(ref, children…) wraps a subtree with a .fui-theme-<hash> class. Inside, every component reads from the override's var(--*) values instead of the canonical :root. No component code changes.",
		Actions: html.LinkHTML(html.LinkHTMLConfig{
			Href:    "/framework-ui/",
			Class:   "ui-button",
			Content: render.Text("← Framework UI"),
		}),
	})

	panel := func(label string, contents ...render.HTML) render.HTML {
		return render.Tag("section",
			map[string]string{"class": "themed-demo__panel"},
			append([]render.HTML{
				render.Tag("p", map[string]string{"class": "themed-demo__label"},
					render.Text(label)),
			}, contents...)...)
	}

	// The same Render output appears on both sides. The DOM
	// difference is exactly one wrapping <div class="fui-theme-…">
	// on the right.
	sample := func() render.HTML {
		return render.Tag("div", map[string]string{"class": "themed-demo__sample"},
			ui.Section(ui.SectionConfig{
				Heading:     "Settings",
				Description: "All fields update live.",
			},
				ui.FormField(ui.FormFieldConfig{
					Label: "Display name", For: "themed-name",
					Input: html.Input(html.InputConfig{
						Type: "text", Name: "themed-name", ID: "themed-name",
					}),
				}),
				render.Tag("div", map[string]string{"class": "themed-demo__actions"},
					ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
					ui.Button(ui.ButtonConfig{Label: "Cancel", Variant: ui.ButtonSecondary}),
				),
			),
			ui.Callout(ui.CalloutConfig{Title: "Heads up", Variant: ui.StatusInfo},
				render.Text("Callouts and buttons inherit the active theme automatically.")),
		)
	}

	demo := render.Tag("div", map[string]string{"class": "themed-demo__grid"},
		panel("Default theme", sample()),
		panel("ui.Themed(Dark, …) — same components, dark cascade",
			ui.Themed(Dark, sample()),
		),
	)

	how := ui.Section(ui.SectionConfig{
		Heading:     "How it works",
		Description: "The right panel is wrapped with a single div whose class re-declares the theme's var(--*) values. The CSS cascade rebinds every var() reference inside, no component code knows or cares.",
		ID:          "how-it-works",
	},
		ui.CodeBlock(ui.CodeBlockConfig{
			Language: "go",
			Code: `// app/theme/dark.go (or wherever your app lives)
var Dark = style.RegisterThemeOverride(func() style.Theme {
    t := style.DefaultTheme()
    t.Colors.Background = style.Color{Name: "background", Value: "#0a0a0a"}
    t.Colors.Text       = style.Color{Name: "text",       Value: "#f4f4f5"}
    // …override whatever else you need.
    return t
}())

// At any render site:
ui.Themed(Dark,
    ui.Section(ui.SectionConfig{Heading: "Settings"},
        ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
    ),
)`,
		}),
	)

	return render.Join(header, demo, how)
}
