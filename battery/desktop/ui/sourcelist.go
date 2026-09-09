package desktopui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type SourceItem struct {
	// Label is the visible text. Required.
	Label string

	// Href is the link target. Cleaned the way every framework link
	// is (urlsafe.CleanAnchor); a rejected value degrades to "#".
	Href string

	// Icon is optional inline HTML rendered before the label inside an
	// aria-hidden span (it decorates; the label names the row).
	Icon string
}

// SourceSection is one labelled group of rows. A source list is flat:
// sections group rows, rows do not nest. Deeper trees belong in the
// content column.
type SourceSection struct {
	// Title is the section header. Empty renders the rows with no
	// header, the unlabeled leading group macOS apps use.
	Title string

	Items []SourceItem
}

// SourceListConfig configures a SourceList.
type SourceListConfig struct {
	// Label names the navigation for assistive tech when the list is
	// used OUTSIDE the desktop layout (which already wraps its sidebar
	// slot in <nav aria-label="Sidebar">). Rendered as aria-label.
	Label string

	// Sections is the list content, rendered in order.
	Sections []SourceSection

	// CurrentPath marks the matching row aria-current="page" in the
	// server-rendered bytes. After hydration the runtime stamps
	// aria-current on any matching <a> itself, the same fallback
	// ui.Sidebar relies on.
	CurrentPath string

	// Footer renders at the bottom (account row, settings link).
	Footer render.HTML
}

// SourceList is the macOS source-list sidebar: section headers at the
// HIG caption size, rows with a capsule selection in Accent, and a
// keyboard focus ring. The surface itself paints NOTHING (background:
// transparent): the native sidebar material the shell places under the
// sidebar zone shows through, so the page never fakes the material in
// CSS.
//
// It is not a twin of ui.Sidebar: that component owns the responsive
// web-sidebar machinery (hamburger drawer, collapse rail, localStorage
// state) and its fixed square-corner look cannot take the translucent
// pill treatment through its config. A desktop window is never narrow,
// so the drawer machinery is dead weight there. Use ui.Sidebar for web
// apps, SourceList for the desktop layout's sidebar slot.
//
// Compose it with the desktop layout:
//
//	layout := desktopui.Layout().WithSidebar(
//		app.NewStaticComponent(desktopui.SourceList(cfg)))
func SourceList(cfg SourceListConfig) render.HTML {
	var b strings.Builder
	b.WriteString(`<div class="desktopui-sourcelist"`)
	if cfg.Label != "" {
		b.WriteString(` aria-label="` + render.Escape(cfg.Label) + `"`)
	}
	b.WriteString(`>`)
	for _, sec := range cfg.Sections {
		b.WriteString(`<div class="desktopui-sourcelist__section">`)
		if sec.Title != "" {
			b.WriteString(`<h2 class="desktopui-sourcelist__header">`)
			b.WriteString(render.Escape(sec.Title))
			b.WriteString(`</h2>`)
		}
		b.WriteString(`<ul class="desktopui-sourcelist__list">`)
		for _, it := range sec.Items {
			b.WriteString(`<li>`)
			href := urlsafe.CleanAnchor(it.Href)
			if href == "" {
				href = "#"
			}
			b.WriteString(`<a class="desktopui-sourcelist__item" href="` + render.Escape(href) + `"`)
			if cfg.CurrentPath != "" && cfg.CurrentPath == it.Href {
				b.WriteString(` aria-current="page"`)
			}
			b.WriteString(`>`)
			if it.Icon != "" {
				b.WriteString(`<span class="desktopui-sourcelist__icon" aria-hidden="true">`)
				b.WriteString(it.Icon)
				b.WriteString(`</span>`)
			}
			b.WriteString(`<span class="desktopui-sourcelist__label">`)
			b.WriteString(render.Escape(it.Label))
			b.WriteString(`</span></a></li>`)
		}
		b.WriteString(`</ul></div>`)
	}
	if cfg.Footer != "" {
		b.WriteString(`<div class="desktopui-sourcelist__footer">`)
		b.WriteString(string(cfg.Footer))
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return sourceListStyle.WrapHTML(render.HTML(b.String()))
}

var sourceListStyle = registry.RegisterStyle("desktopui-sourcelist", sourceListCSS)

func sourceListCSS(_ style.Theme) string {
	return `[data-fui-comp="desktopui-sourcelist"] {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-lg, 16px);
  padding: var(--spacing-md, 8px) var(--spacing-sm, 4px);
  min-block-size: 100%;
  /* The native material under the sidebar zone shows through. The page
     never paints it: CSS cannot reach the real blur, and a fake one is
     the thing the plan refuses to imitate. */
  background: transparent;
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__section {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__header {
  /* HIG caption size (Caption 1/2 are 10pt), uppercase is not the Mac
     idiom; the size and the muted color carry the hierarchy. */
  font-size: var(--text-xs, 0.75rem);
  font-weight: 500;
  color: var(--color-text-subtle, #71717A);
  margin: 0;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  /* Capsule selection: the concentric rule's capsule case, radius
     half the row height. Row height measured against native captures,
     unverified; 26px keeps every row at or above the 24px 2.5.8 AA
     minimum. */
  min-height: var(--desktop-sourcelist-row, 26px);
  padding: var(--spacing-xs, 2px) var(--spacing-md, 8px);
  border-radius: var(--radii-full, 9999px);
  color: var(--color-text, #18181B);
  font-size: var(--text-base, 1rem);
  text-decoration: none;
  cursor: pointer;
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item:hover {
  background: color-mix(in srgb, var(--color-text, #18181B) 8%, transparent);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item:focus-visible {
  /* Keyboard focus ring in the accent, the Mac focus-ring read. */
  outline: 2px solid var(--color-accent, #7C3AED);
  outline-offset: var(--spacing-xs, 2px);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item[aria-current="page"] {
  /* Pill selection in Accent: white label on the system blue, the
     native selected-row pair. */
  background: var(--color-accent, #7C3AED);
  color: var(--color-primary-fg, #FFFFFF);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__icon {
  display: inline-flex;
  align-items: center;
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__footer {
  margin-block-start: auto;
  padding: var(--spacing-md, 8px);
}
/* Inactive window: the selected row grays out, the read a native
   source list gives when its window resigns key. The runtime module
   sets the class; the page never guesses. */
html.desktop-inactive [data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item[aria-current="page"] {
  background: var(--color-text-muted, #52525B);
  color: var(--color-primary-fg, #FFFFFF);
}
`
}
