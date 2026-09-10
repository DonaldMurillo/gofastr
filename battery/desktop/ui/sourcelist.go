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
	// aria-hidden span (it decorates; the label names the row). It
	// keeps its own color while selected, the native read.
	Icon string

	// Count is the optional right-aligned trailing number (a mailbox's
	// unread count, a folder's item count). Empty renders nothing.
	Count string
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

// SourceList is the macOS source-list sidebar: rows at the native
// metrics (28 px rows, the theme's HIG body size, an optional leading
// icon and trailing count, gray section headers at the caption size),
// a soft rounded-rect selection whose text keeps its normal color,
// and a keyboard focus ring in the accent. The surface itself paints
// NOTHING (background: transparent): the native sidebar material the
// shell places under the sidebar zone shows through, so the page never
// fakes the material in CSS.
//
// It is not a twin of ui.Sidebar: that component owns the responsive
// web-sidebar machinery (hamburger drawer, collapse rail, localStorage
// state) and its fixed square-corner look cannot take the translucent
// selection treatment through its config. A desktop window is never
// narrow, so the drawer machinery is dead weight there. Use ui.Sidebar
// for web apps, SourceList for the desktop layout's sidebar slot.
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
			// The current row carries the runtime's own active class
			// beside aria-current. core-ui's activelink module clears
			// a stale marker only from links carrying the class it
			// stamps, so a server-rendered marker without the class
			// survives the module's first sweep: a navigation that
			// lands before the module idle-loads would leave two rows
			// reading as current. The class is the documented
			// reconciliation contract; nothing styles it here.
			class := "desktopui-sourcelist__item"
			current := cfg.CurrentPath != "" && cfg.CurrentPath == it.Href
			if current {
				class += " active"
			}
			b.WriteString(`<a class="` + class + `" href="` + render.Escape(href) + `"`)
			if current {
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
			b.WriteString(`</span>`)
			if it.Count != "" {
				b.WriteString(`<span class="desktopui-sourcelist__count">`)
				b.WriteString(render.Escape(it.Count))
				b.WriteString(`</span>`)
			}
			b.WriteString(`</a></li>`)
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
  /* The layout's sidebar zone owns the top inset (the traffic-light
     reservation), so the list adds no top padding of its own. */
  padding: 0 var(--spacing-sm, 4px) var(--spacing-md, 8px);
  min-block-size: 100%;
  /* The native material under the sidebar zone shows through. The page
     never paints it: CSS cannot reach the real blur, and a fake one is
     the thing the plan refuses to imitate. */
  background: transparent;
  --desktop-sourcelist-row: 28px;
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
  /* Row height measured against the native Notes capture (31.5pt
     pitch on a 28pt row, unverified); the type is the theme's body
     token, which the desktop theme resolves to the HIG 13px body
     size. The fallback states the canonical token value (the rule:
     a fallback teaches what the token means, and an unthemed page
     renders the canonical scale). */
  min-height: var(--desktop-sourcelist-row, 28px);
  padding: var(--spacing-xs, 2px) var(--spacing-md, 8px);
  border-radius: var(--radii-md, 8px);
  color: var(--color-text, #18181B);
  font-size: var(--text-base, 1rem);
  text-decoration: none;
  cursor: pointer;
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__label {
  flex: 1 1 auto;
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__count {
  margin-inline-start: auto;
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-subtle, #71717A);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item:hover {
  background: color-mix(in srgb, var(--color-text, #18181B) 3%, transparent);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item:focus-visible {
  /* Keyboard focus ring in the accent, the Mac focus-ring read. */
  outline: 2px solid var(--color-accent, #7C3AED);
  outline-offset: var(--spacing-xs, 2px);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item[aria-current="page"] {
  /* Soft selection, measured on the native capture: #EFEFEF over a
     #F9F9F9 light sidebar and #2F2F2F over a #212121 dark one, both
     within a couple of points of a 5% mix of the text color. The text
     and the icon keep their own colors; the accent belongs to the
     focus ring, not the row. */
  background: color-mix(in srgb, var(--color-text, #18181B) 5%, transparent);
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__icon {
  display: inline-flex;
  align-items: center;
  flex: 0 0 auto;
}
[data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__footer {
  margin-block-start: auto;
  padding: var(--spacing-md, 8px);
}
/* Inactive window: the selection washes out, the read a native source
   list gives when its window resigns key. The runtime module sets the
   class; the page never guesses. */
html.desktop-inactive [data-fui-comp="desktopui-sourcelist"] .desktopui-sourcelist__item[aria-current="page"] {
  background: color-mix(in srgb, var(--color-text-muted, #52525B) 3%, transparent);
}
`
}
