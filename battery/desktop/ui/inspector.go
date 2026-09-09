package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// InspectorConfig configures an Inspector.
type InspectorConfig struct {
	// Label names the region for assistive tech. Required: an unnamed
	// region is a landmark nobody can navigate.
	Label string

	// Title is the panel header. Optional.
	Title string

	// Items renders as ui.DetailList (label/value rows) under the
	// header. The catalog already owns that primitive; the inspector
	// owns the surface.
	Items []ui.DetailItem

	// Footer renders at the bottom of the panel.
	Footer render.HTML
}

// Inspector is the macOS inspector pane: a labelled side panel on the
// glass surface showing details about the selection. It renders the
// surface (Glass) and the structure; ui.DetailList renders the rows,
// so the framework's detail styling (grid, collapse) applies unchanged.
//
// The panel is content, not layout: place it in its own column (a
// grid cell beside the content, or ui.PaneHost's tertiary pane). It
// does not position itself.
func Inspector(cfg InspectorConfig) render.HTML {
	if cfg.Label == "" {
		panic("desktopui: Inspector requires Label")
	}
	children := make([]render.HTML, 0, 3)
	if cfg.Title != "" {
		children = append(children, render.Tag("h2", map[string]string{
			"class": "desktopui-inspector__title",
		}, render.Text(cfg.Title)))
	}
	if len(cfg.Items) > 0 {
		children = append(children, ui.DetailList(ui.DetailListConfig{Items: cfg.Items}))
	}
	if cfg.Footer != "" {
		children = append(children, render.Tag("div", map[string]string{
			"class": "desktopui-inspector__footer",
		}, cfg.Footer))
	}
	aside := render.Tag("aside", html.Attrs{
		"class":      "desktopui-inspector",
		"role":       "region",
		"aria-label": cfg.Label,
	}, children...)
	return Glass(GlassConfig{}, inspectorStyle.WrapHTML(aside))
}

var inspectorStyle = registry.RegisterStyle("desktopui-inspector", inspectorCSS)

func inspectorCSS(_ style.Theme) string {
	return `[data-fui-comp="desktopui-inspector"] {
  display: block;
  inline-size: 100%;
}
[data-fui-comp="desktopui-inspector"] .desktopui-inspector {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  /* The title keeps the panel a named region even when the glass
     surface is the visible chrome. */
  min-inline-size: var(--desktop-inspector-width, 260px);
}
[data-fui-comp="desktopui-inspector"] .desktopui-inspector__title {
  font-size: var(--text-base, 1rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
  margin: 0;
}
[data-fui-comp="desktopui-inspector"] .desktopui-inspector__footer {
  margin-block-start: auto;
  padding-top: var(--spacing-md, 8px);
  border-top: 1px solid var(--color-border, #E4E4E7);
}
`
}
