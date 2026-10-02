package ui

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ContentRow ────────────────────────────────────────────────────
//
// The page's content row: a start column (a ui.Sidebar), the main
// column, and an optional end column (a context aside) side by side,
// stacked below a breakpoint. This is the one thing a layout recipe
// cannot compose from existing pieces: no other component places three
// columns in a row and owns the frame chrome between them (the nav
// column's surface and rule, the aside's width and rule, the app
// padding around main). Workbench and ListDetail are pane splits
// inside a column; this is the column arrangement itself.
//
// The row owns only the frame. The start column is the nav landmark
// around the sidebar: headless.Sidebar's own <nav> names the links
// region alone, so its title, prepend and footer sit outside it — the
// column landmark is what keeps every part of the sidebar inside a
// landmark (axe region; the retired page shell wrapped the column the
// same way). The aside is an <aside> labelled by AsideLabel. Nothing
// inside main is styled from here: screens bring their own
// ui.PageHeader, Stack gaps and sections. Compose it inside an
// app.NewLayout build around l.Primary():
//
//	nav, _ := component.SafeRenderCtx(ctx, ui.Sidebar(cfg))
//	return ui.ContentRow(ui.ContentRowConfig{Sidebar: nav}, l.Primary())

// ContentRowConfig configures a ContentRow.
type ContentRowConfig struct {
	// Sidebar is the start column. Give it a ui.Sidebar; the column
	// takes its width from the sidebar's content, sits on the surface
	// color and draws the inline-end rule. Below the breakpoint the
	// sidebar's own drawer serves navigation and the column stacks
	// above main with a block-end rule instead.
	Sidebar render.HTML
	// NavLabel labels the start column's navigation landmark — the
	// landmark that wraps the sidebar's title, links and footer (the
	// sidebar's own inner nav names only the links list). Defaults to
	// "Sidebar".
	NavLabel string
	// Toolbar is an optional row above main, beside the sidebar —
	// the workspace the shell's Toolbar slot renders. Compose a
	// ui.Toolbar (or any row) here; the row owns only its placement
	// and its block-end rule.
	Toolbar render.HTML
	// Aside is the optional end column after main: a context aside.
	// It stacks below main under the breakpoint, and it releases its
	// width when it holds only an empty outlet, so an unfilled
	// context column takes no space.
	Aside render.HTML
	// AsideLabel labels the aside landmark. Defaults to "Context".
	AsideLabel string
	// Breakpoint picks the viewport width below which the row stacks:
	// below md (48rem, the default) or below lg (64rem). Match it to
	// SidebarConfig.DrawerBreakpoint so the sidebar becomes a drawer
	// at the same width the row collapses.
	Breakpoint StackBreakpoint
	// Viewport confines desktop scrolling to main, the nav column and
	// the aside: at and above the breakpoint the row fills the rest of
	// the viewport below the page header and each column scrolls on
	// its own; below it the page scrolls normally. The row cannot
	// style its parent, so it reads the header's height from the
	// --size-header-height token (56px default) — the same token
	// the app-shell header band fixes its own height with — and
	// assumes the page column above it is at least one screen tall.
	// A recipe arranges both: ui.Stack{Screen: true} above, a header
	// carrying the fixed band. The row assumes no footer band below it
	// in this mode; give viewport pages their footer inside main.
	Viewport bool
	// PhoneNavFlush drops the stacked nav column's block-end rule
	// below the breakpoint. Set it when the sidebar's phone navigation
	// lives outside the column (a NativeMobile sidebar whose drawer
	// trigger the page header hosts): the stacked column renders empty
	// on phones and an empty band must not draw a line.
	PhoneNavFlush bool
	// Class appends to the row root's class list.
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the row root. Keys the
	// component owns are dropped: class (use Class), and data-fui-*.
	ExtraAttrs html.Attrs
}

// ContentRow renders the content row around the main region. With no
// Sidebar it still owns the row (the flex row, main's growth) — the
// leanest page shape. The row is as tall as its content; place it in
// ui.Stack{Screen: true, Gap: ui.GapNone} between the header and the
// footer and it grows to fill the page, so a short page keeps its
// footer at the viewport bottom with no dead scroll.
func ContentRow(cfg ContentRowConfig, main ...render.HTML) render.HTML {
	checkStackBreakpoint(cfg.Breakpoint)
	cls := "fui-content-row"
	if cfg.Sidebar != "" {
		cls += " fui-content-row--has-nav"
	}
	if cfg.Viewport {
		cls += " fui-content-row--viewport"
	}
	if cfg.PhoneNavFlush {
		cls += " fui-content-row--phone-nav-flush"
	}
	if cfg.Breakpoint == StackBelowLG {
		cls += " fui-content-row--stack-below-lg"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	var body []render.HTML
	if cfg.Sidebar != "" {
		label := cfg.NavLabel
		if label == "" {
			label = "Sidebar"
		}
		// The nav landmark around the sidebar: headless.Sidebar's own
		// nav names only the links list, so the column landmark is what
		// keeps the sidebar's title and footer inside a landmark too.
		body = append(body, html.Nav(html.NavConfig{Label: label, Class: "fui-content-row__nav"}, cfg.Sidebar))
	}
	if cfg.Toolbar != "" {
		body = append(body, html.Div(html.DivConfig{Class: "fui-content-row__workspace"},
			html.Div(html.DivConfig{Class: "fui-content-row__toolbar"}, cfg.Toolbar),
			render.Join(main...)))
	} else {
		body = append(body, main...)
	}
	if cfg.Aside != "" {
		label := cfg.AsideLabel
		if label == "" {
			label = "Context"
		}
		body = append(body, html.Aside(html.AsideConfig{Label: label, Class: "fui-content-row__aside"}, cfg.Aside))
	}

	return contentRowStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, body...))
}

var contentRowStyle = registry.RegisterStyle("ui-content-row", contentRowCSS)

// rowBreakpointCSS is the responsive half of the sheet, generated per
// posture so the md default and the lg option share one spelling: scope
// selects the rows (":not(.fui-content-row--stack-below-lg)" for the md
// default, ".fui-content-row--stack-below-lg" for lg) and maxw/minw are
// the paired media widths around the breakpoint.
type rowBreakpointCSS struct {
	scope string
	maxw  string
	minw  string
}

func contentRowCSS(_ style.Theme) string {
	md := rowBreakpointCSS{scope: ":not(.fui-content-row--stack-below-lg)", maxw: "47.99rem", minw: "48rem"}
	lg := rowBreakpointCSS{scope: ".fui-content-row--stack-below-lg", maxw: "63.99rem", minw: "64rem"}
	// The row claims no viewport height of its own: a bare 100vh minimum
	// under a header scrolls a short page by the header's height. It
	// grows instead, so inside ui.Stack{Screen: true} it takes whatever
	// the header and footer leave and the nav column reaches the footer.
	return `.fui-content-row { display: flex; align-items: stretch; flex: 1 0 auto; }
.fui-content-row > main, .fui-content-row > .layout-content { flex: 1 1 auto; min-width: 0; }
.fui-content-row__nav { flex: 0 0 auto; background-color: var(--color-surface, #fff); border-right: 1px solid var(--color-border, #e4e4e7); }
.fui-content-row__workspace { flex: 1 1 auto; min-inline-size: 0; display: flex; flex-direction: column; }
.fui-content-row__workspace > main, .fui-content-row__workspace > .layout-content { flex: 1 1 auto; min-inline-size: 0; }
.fui-content-row__toolbar { flex: 0 0 auto; min-inline-size: 0; padding: var(--spacing-sm) var(--spacing-lg); border-block-end: 1px solid var(--color-border); }
.fui-content-row__aside { flex: 0 0 var(--ui-content-row-aside-width, 18rem); min-inline-size: 0; padding: var(--spacing-lg); border-inline-start: 1px solid var(--color-border); }
.fui-content-row__aside:has(> [data-fui-outlet]:empty) { display: none; }
/* Viewport aside: the tighter padding applies below the breakpoint
   too, matching the shell (the phone column is denser everywhere). */
.fui-content-row--viewport .fui-content-row__aside { flex-basis: var(--ui-content-row-aside-width, 18rem); padding: var(--spacing-md); }
.fui-content-row--viewport { min-block-size: 0; flex: 1 0 auto; }
/* App frame: a padded content area beside the nav column. */
.fui-content-row--has-nav main, .fui-content-row--has-nav .layout-content {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xl, 24px);
  padding: clamp(24px, 3vw, 40px);
}
` + md.viewportCSS() + md.stackCSS() + lg.viewportCSS() + lg.stackCSS()
}

// viewportCSS is the desktop half of Viewport mode: the row fills the
// rest of the viewport below the header and each column scrolls on its
// own. Below the breakpoint the stackCSS half returns the page to
// normal document flow. The :SCOPE: token stands where the posture's
// scoping selector goes (":not(…)" for the md default, the lg class
// for the option); scoped this way the two postures never both apply.
func (b rowBreakpointCSS) viewportCSS() string {
	css := fmt.Sprintf(`@media (min-width: %s) {
  :where(.fui-content-row--viewport):SCOPE: { block-size: calc(100dvh - var(--size-header-height, 56px)); min-block-size: 0; flex: 1 1 auto; overflow: hidden; }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace { min-block-size: 0; }
  .fui-content-row--viewport:SCOPE: .fui-content-row__nav,
  .fui-content-row--viewport:SCOPE: .fui-content-row__aside,
  .fui-content-row--viewport:SCOPE: > main,
  .fui-content-row--viewport:SCOPE: > .layout-content,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > main,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > .layout-content { min-block-size: 0; overflow-y: auto; }
  .fui-content-row--viewport:SCOPE: > main,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > main,
  .fui-content-row--viewport:SCOPE: > .layout-content,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > .layout-content { padding: var(--spacing-lg); }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace .layout-content .layout-content { padding: 0; gap: 0; }
}
`, b.minw)
	return strings.ReplaceAll(css, ":SCOPE:", b.scope)
}

// stackCSS is the below-breakpoint half: the columns stack, the nav
// column trades its inline-end rule for a block-end one, the aside
// draws its block-start rule, and a viewport row's main takes the
// tighter phone padding.
func (b rowBreakpointCSS) stackCSS() string {
	css := fmt.Sprintf(`@media (max-width: %s) {
  :where(.fui-content-row):SCOPE: { display: block; }
  .fui-content-row:SCOPE: .fui-content-row__nav { border-right: none; border-bottom: 1px solid var(--color-border, #e4e4e7); }
  .fui-content-row:SCOPE: .fui-content-row__aside { border-inline-start: none; border-block-start: 1px solid var(--color-border); }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > main,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > .layout-content { padding: var(--spacing-md); }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace .layout-content .layout-content { padding: 0; gap: 0; }
  /* PhoneNavFlush: the caller says the sidebar's phone navigation lives
     outside the column (a NativeMobile sidebar whose trigger the page
     header hosts), so the stacked band renders empty and must not draw
     its separator. */
  .fui-content-row--phone-nav-flush:SCOPE: .fui-content-row__nav { border: 0; }
}
`, b.maxw)
	return strings.ReplaceAll(css, ":SCOPE:", b.scope)
}
