package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SheetConfig configures a Sheet.
type SheetConfig struct {
	// Title names the dialog (aria-label) and renders as its header.
	Title string

	// Footer renders at the bottom, the action row (buttons).
	Footer render.HTML
}

// Sheet is the sheet variant on the thick glass: a labelled dialog surface for confirmations and short forms.
func Sheet(cfg SheetConfig, body ...render.HTML) render.HTML {
	if cfg.Title == "" {
		panic("desktopui: Sheet requires Title")
	}
	children := make([]render.HTML, 0, 3)
	children = append(children, render.Tag("h2", map[string]string{
		"class": "desktopui-sheet__title",
	}, render.Text(cfg.Title)))
	children = append(children, body...)
	if cfg.Footer != "" {
		children = append(children, render.Tag("div", map[string]string{
			"class": "desktopui-sheet__footer",
		}, cfg.Footer))
	}
	dialog := render.Tag("div", html.Attrs{
		"class":      "desktopui-sheet",
		"role":       "dialog",
		"aria-modal": "true",
		"aria-label": cfg.Title,
	}, children...)
	return Glass(GlassConfig{Thick: true}, sheetStyle.WrapHTML(dialog))
}

// SheetSkeleton adapts Sheet to a widget Definition's Skeleton slot.
func SheetSkeleton(cfg SheetConfig) func(slots map[string]render.HTML) render.HTML {
	return func(slots map[string]render.HTML) render.HTML {
		var body []render.HTML
		if v, ok := slots["body"]; ok {
			body = append(body, v)
		}
		scfg := cfg
		if f, ok := slots["footer"]; ok {
			scfg.Footer = f
		}
		return Sheet(scfg, body...)
	}
}

// PopoverConfig configures a Popover.
type PopoverConfig struct {
	// Title names the dialog (aria-label) and renders as its compact header.
	Title string
}

// Popover is a compact non-modal dialog surface on the thick glass.
func Popover(cfg PopoverConfig, body ...render.HTML) render.HTML {
	children := make([]render.HTML, 0, 2)
	if cfg.Title != "" {
		children = append(children, render.Tag("h2", map[string]string{
			"class": "desktopui-popover__title",
		}, render.Text(cfg.Title)))
	}
	children = append(children, body...)
	attrs := html.Attrs{
		"class": "desktopui-popover",
		"role":  "dialog",
	}
	if cfg.Title != "" {
		attrs["aria-label"] = cfg.Title
	}
	dialog := render.Tag("div", attrs, children...)
	return Glass(GlassConfig{Thick: true}, popoverStyle.WrapHTML(dialog))
}

var (
	sheetStyle   = registry.RegisterStyle("desktopui-sheet", sheetCSS)
	popoverStyle = registry.RegisterStyle("desktopui-popover", popoverCSS)
)

func sheetCSS(_ style.Theme) string {
	return `[data-cui-comp="desktopui-sheet"] {
  inline-size: 100%;
  max-inline-size: var(--desktop-sheet-width, 420px);
}
[data-cui-comp="desktopui-sheet"].desktopui-sheet {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
}
[data-cui-comp="desktopui-sheet"] .desktopui-sheet__title {
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #000000);
  margin: 0;
}
[data-cui-comp="desktopui-sheet"] .desktopui-sheet__footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--spacing-sm, 4px);
  padding-top: var(--spacing-md, 8px);
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
`
}

func popoverCSS(_ style.Theme) string {
	return `[data-cui-comp="desktopui-popover"] {
  inline-size: 100%;
  max-inline-size: var(--desktop-popover-width, 320px);
}
[data-cui-comp="desktopui-popover"].desktopui-popover {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
}
[data-cui-comp="desktopui-popover"] .desktopui-popover__title {
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text-muted, #52525B);
  margin: 0;
}
`
}
