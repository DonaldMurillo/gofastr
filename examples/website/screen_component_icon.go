package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Register the demo's custom icon at package init so it's available on
// the first request. Calling RegisterIcon from inside Render() means
// the icon is missing on the very first page load — Icon("custom-square")
// runs before RegisterIcon does within the same Render call — and only
// appears once the global registry has been mutated by a prior request.
func init() {
	ui.RegisterIcon("custom-square",
		`<rect x="3" y="3" width="18" height="18" rx="3"/>`)
}

type IconScreen struct{}

func (s *IconScreen) ScreenTitle() string {
	return "Icon"
}
func (s *IconScreen) ScreenDescription() string {
	return "Inline SVG icon primitive with a registry-backed name → markup lookup."
}
func (s *IconScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *IconScreen) Render() render.HTML {
	// Built-in gallery.
	builtIn := []string{
		"check", "close",
		"chevron-up", "chevron-down", "chevron-left", "chevron-right",
		"info", "warning", "danger", "success",
	}
	gallery := make([]render.HTML, 0, len(builtIn))
	for _, name := range builtIn {
		cell := render.Tag("div", map[string]string{"class": "demo-icon-cell"},
			ui.Icon(name, ui.IconConfig{Size: "24"}),
			render.Tag("code", nil, render.Text(name)),
		)
		gallery = append(gallery, cell)
	}
	galleryRow := render.Tag("div", map[string]string{"class": "demo-icon-gallery"}, gallery...)

	galleryStyle := render.Tag("style", nil, render.HTML(`
.demo-icon-gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(8rem, 1fr));
  gap: var(--spacing-md, 12px);
}
.demo-icon-cell {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--spacing-xs, 6px);
  padding: var(--spacing-md, 12px);
  border: 1px solid var(--color-border, #E5E7EB);
  border-radius: var(--radii-sm, 4px);
}
.demo-icon-cell code {
  font-size: 0.75rem;
  color: var(--color-text-muted, #6B7280);
}
`))

	sized := render.Tag("div", map[string]string{"class": "demo-row-tight"},
		ui.Icon("check", ui.IconConfig{Size: "16"}),
		ui.Icon("check", ui.IconConfig{Size: "20"}),
		ui.Icon("check", ui.IconConfig{Size: "32"}),
		ui.Icon("check", ui.IconConfig{Size: "48"}),
	)
	srcSized := `ui.Icon("check", ui.IconConfig{Size: "16"})
ui.Icon("check", ui.IconConfig{Size: "20"})
ui.Icon("check", ui.IconConfig{Size: "32"})
ui.Icon("check", ui.IconConfig{Size: "48"})`

	labeled := ui.Icon("warning", ui.IconConfig{Size: "24", AriaLabel: "Warning"})
	srcLabeled := `ui.Icon("warning", ui.IconConfig{Size: "24", AriaLabel: "Warning"})`

	custom := render.Tag("div", nil,
		ui.Icon("custom-square", ui.IconConfig{Size: "32"}),
	)
	srcCustom := `ui.RegisterIcon("custom-square",
    ` + "`<rect x=\"3\" y=\"3\" width=\"18\" height=\"18\" rx=\"3\"/>`" + `)
ui.Icon("custom-square", ui.IconConfig{Size: "32"})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Icon")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Inline SVG icon primitive backed by a registry. Renders viewBox=\"0 0 24 24\" with stroke=\"currentColor\" so icons inherit the theme color. Decorative by default (aria-hidden); set AriaLabel to mark it meaningful.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Built-in icons")),
		galleryStyle,
		galleryRow,

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Sizes")),
		demoFrame(sized, srcSized),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Labeled (meaningful)")),
		render.Tag("p", nil, render.Text(
			"When the icon conveys meaning that isn't repeated in adjacent text, set AriaLabel — the SVG renders with role=\"img\" and the label.")),
		demoFrame(labeled, srcLabeled),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Registering custom icons")),
		render.Tag("p", nil, render.Text(
			"RegisterIcon(name, body) adds a new icon to the registry. Body is the inner SVG markup — not the <svg> wrapper. Re-registering the same name replaces the existing body.")),
		demoFrame(custom, srcCustom),
	)
}
