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
	// Label names the region for assistive tech.
	Label string

	// Title is the panel header.
	Title string

	// Items renders as ui.DetailList (label/value rows) under the header.
	Items []ui.DetailItem

	// Footer renders at the bottom of the panel.
	Footer render.HTML
}

// Inspector is a labelled glass side panel of detail rows; it panics without a Label.
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

// InspectorSplit places content and an inspector side by side, the inspector in the trailing column.
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
	return `[data-cui-comp="desktopui-inspector-split"] {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  gap: var(--spacing-lg, 16px);
}
[data-cui-comp="desktopui-inspector-split"] > .desktopui-inspector-split__content {
  flex: 1 1 20rem;
  min-inline-size: 0;
  display: grid;
  gap: var(--spacing-lg, 16px);
}
[data-cui-comp="desktopui-inspector-split"] > .desktopui-inspector-split__inspector {
  flex: 0 0 var(--desktop-inspector-width, 260px);
}
`
}

var inspectorStyle = registry.RegisterStyle("desktopui-inspector", inspectorCSS)

func inspectorCSS(_ style.Theme) string {
	return `[data-cui-comp="desktopui-inspector"] {
  display: block;
  inline-size: 100%;
  /* A 260px panel cannot give the label column the page default
     (up to 13rem): cap it so values keep one line. */
  --ui-detail-list-label-track: minmax(4rem, 6rem);
}
/* The marker lands on the <aside> itself (WrapHTML stamps the
   outermost tag), so the panel rule is compound, not a descendant. */
[data-cui-comp="desktopui-inspector"].desktopui-inspector {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  /* The title keeps the panel a named region even when the glass
     surface is the visible chrome. */
  min-inline-size: var(--desktop-inspector-width, 260px);
  box-sizing: border-box;
}
[data-cui-comp="desktopui-inspector"] .desktopui-inspector__title {
  font-size: var(--text-base, 1rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #000000);
  margin: 0;
}
[data-cui-comp="desktopui-inspector"] .desktopui-inspector__footer {
  margin-block-start: auto;
  padding-top: var(--spacing-md, 8px);
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
`
}
