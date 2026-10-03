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
// The panel is content, not layout: InspectorSplit puts it in the
// trailing column beside the content, the native inspector position.
// It does not position itself.
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

// InspectorSplit places content and an inspector side by side: the
// content takes the remaining width, the inspector keeps the trailing
// column at --desktop-inspector-width (default 260px, measured,
// unverified). When the window is too narrow for both (content under
// 20rem), the inspector wraps below the content instead of squeezing
// it.
//
//	desktopui.InspectorSplit(body, desktopui.Inspector(cfg))
func InspectorSplit(content, inspector render.HTML) render.HTML {
	return inspectorSplitStyle.WrapHTML(render.Tag("div", html.Attrs{
		"class": "desktopui-inspector-split",
	},
		render.Tag("div", html.Attrs{"class": "desktopui-inspector-split__content"}, content),
		render.Tag("div", html.Attrs{"class": "desktopui-inspector-split__inspector"}, inspector),
	))
}

var inspectorSplitStyle = registry.RegisterStyle("desktopui-inspector-split", inspectorSplitCSS)

func inspectorSplitCSS(_ style.Theme) string {
	return `[data-fui-comp="desktopui-inspector-split"] {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: var(--spacing-lg, 16px);
}
[data-fui-comp="desktopui-inspector-split"] > .desktopui-inspector-split__content {
  flex: 1 1 20rem;
  min-inline-size: 0;
  display: grid;
  gap: var(--spacing-lg, 16px);
}
[data-fui-comp="desktopui-inspector-split"] > .desktopui-inspector-split__inspector {
  flex: 0 0 var(--desktop-inspector-width, 260px);
}
`
}

var inspectorStyle = registry.RegisterStyle("desktopui-inspector", inspectorCSS)

func inspectorCSS(_ style.Theme) string {
	return `[data-fui-comp="desktopui-inspector"] {
  display: block;
  inline-size: 100%;
  /* A 260px panel cannot give the label column the page default
     (up to 13rem): cap it so values keep one line. */
  --ui-detail-list-label-track: minmax(4rem, 6rem);
}
/* The marker lands on the <aside> itself (WrapHTML stamps the
   outermost tag), so the panel rule is compound, not a descendant. */
[data-fui-comp="desktopui-inspector"].desktopui-inspector {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  /* The title keeps the panel a named region even when the glass
     surface is the visible chrome. */
  min-inline-size: var(--desktop-inspector-width, 260px);
  box-sizing: border-box;
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
