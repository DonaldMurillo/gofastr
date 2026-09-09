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
	// Required: a dialog without an accessible name is announced as
	// just "dialog".
	Title string

	// Footer renders at the bottom, the action row (buttons).
	Footer render.HTML
}

// Sheet is the sheet variant on the thick glass: a labelled dialog
// surface for confirmations and short forms. It composes Glass (Thick)
// as the surface; everything the glass stylesheet bakes in (opaque
// under Reduce Transparency, flattened fill and dimmed accent when the
// window is inactive) applies here too.
//
// The preset widget chrome (preset.Modal, preset.Drawer) paints its
// own opaque panel around slot content, so for a glass sheet pass this
// surface as the widget's Skeleton instead of slot content under the
// default chrome:
//
//	stack := preset.Modal("confirm").Skeleton(desktopui.SheetSkeleton(
//		desktopui.SheetConfig{Title: "Discard draft?"},
//	)).Build()
//
// Direct inline use (an intercept overlay's content, a static page
// region) works the same way.
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

// SheetSkeleton adapts Sheet to the widget Definition's Skeleton slot:
// the standard slots (header, body, footer) land in the sheet's title,
// body, and footer regions.
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
	// Title names the dialog (aria-label) and renders as its compact
	// header. Optional; an unlabeled popover is fine when its trigger
	// names it.
	Title string
}

// Popover is the popover variant on the thick glass: a compact
// non-modal surface for quick actions and short detail, capped at
// --desktop-popover-width (default 320px, measured, unverified).
//
// Same composition note as Sheet: preset.Popover's default chrome
// paints an opaque panel, so pass this surface as the widget Skeleton
// or render it inline. role="dialog" without aria-modal: a popover
// does not trap focus, Tab moves out of it naturally.
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
	return `[data-fui-comp="desktopui-sheet"] {
  inline-size: 100%;
  max-inline-size: var(--desktop-sheet-width, 420px);
}
[data-fui-comp="desktopui-sheet"] .desktopui-sheet {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
}
[data-fui-comp="desktopui-sheet"] .desktopui-sheet__title {
  font-size: var(--text-lg, 1.125rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
  margin: 0;
}
[data-fui-comp="desktopui-sheet"] .desktopui-sheet__footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--spacing-sm, 4px);
  padding-top: var(--spacing-md, 8px);
  border-top: 1px solid var(--color-border, #E4E4E7);
}
`
}

func popoverCSS(_ style.Theme) string {
	return `[data-fui-comp="desktopui-popover"] {
  inline-size: 100%;
  max-inline-size: var(--desktop-popover-width, 320px);
}
[data-fui-comp="desktopui-popover"] .desktopui-popover {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
}
[data-fui-comp="desktopui-popover"] .desktopui-popover__title {
  font-size: var(--text-sm, 0.875rem);
  font-weight: 600;
  color: var(--color-text-muted, #52525B);
  margin: 0;
}
`
}
