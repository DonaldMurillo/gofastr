package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ContentRowConfig configures a ContentRow.
type ContentRowConfig struct {
	// Ctx carries the request context the default labels are written in; nil is English.
	Ctx context.Context

	// Sidebar is the start column, usually a ui.Sidebar; it stacks above main below the breakpoint.
	Sidebar render.HTML
	// NavLabel labels the start column's nav landmark. Defaults to "Sidebar".
	NavLabel string
	// Toolbar is an optional row above main, beside the sidebar.
	Toolbar render.HTML
	// ToolbarLabel names the toolbar row's section landmark. Defaults to "Toolbar".
	ToolbarLabel string
	// Aside is the optional end column. It stacks below main, and takes no width
	// while it holds only an empty outlet.
	Aside render.HTML
	// AsideLabel labels the aside landmark. Defaults to "Context".
	AsideLabel string
	// Breakpoint is the width below which the row stacks: md (48rem, the default)
	// or lg (64rem). Match it to SidebarConfig.DrawerBreakpoint.
	Breakpoint StackBreakpoint
	// Viewport fills the viewport below a --size-header-height header at and above
	// the breakpoint, each column scrolling on its own. Put the footer inside main.
	Viewport bool
	// Sticky keeps the window as the scroller and sticks the nav column and the
	// Toolbar at --ui-sticky-top. It panics beside Viewport.
	Sticky bool
	// PhoneNavFlush drops the stacked nav column's rule on phones, for a sidebar
	// whose phone navigation lives outside the column.
	PhoneNavFlush bool
	// PhoneAsideFlush drops the stacked aside's rule and padding on phones, for an
	// aside whose phone form floats outside it (a TableOfContents PhoneSheet).
	PhoneAsideFlush bool
	// Dense gives the page the compact control density on a fine pointer; a touch
	// screen keeps the theme's values. For operator consoles.
	Dense bool
	// Class appends to the row root's class list.
	Class string
	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// ContentRow renders the content row around main. Placed in ui.Stack{Screen: true,
// Gap: ui.GapNone} between the header and the footer, it fills the page.
func ContentRow(cfg ContentRowConfig, main ...render.HTML) render.HTML {
	checkStackBreakpoint(cfg.Breakpoint)
	if cfg.Sticky && cfg.Viewport {
		panic("ui: ContentRow Sticky and Viewport are two scroll models; set one")
	}
	cls := "fui-content-row"
	if cfg.Sidebar != "" {
		cls += " fui-content-row--has-nav"
	}
	if cfg.Viewport {
		cls += " fui-content-row--viewport"
	}
	if cfg.Sticky {
		cls += " fui-content-row--sticky"
	}
	if cfg.PhoneNavFlush {
		cls += " fui-content-row--phone-nav-flush"
	}
	if cfg.PhoneAsideFlush {
		cls += " fui-content-row--phone-aside-flush"
	}
	if cfg.Dense {
		cls += " fui-content-row--dense"
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
			label = i18nui.ContentRowSidebar.Text(cfg.Ctx)
		}
		body = append(body, html.Nav(html.NavConfig{Label: label, Class: "fui-content-row__nav"}, cfg.Sidebar))
	}
	if cfg.Toolbar != "" {
		label := cfg.ToolbarLabel
		if label == "" {
			label = i18nui.ContentRowToolbar.Text(cfg.Ctx)
		}
		body = append(body, html.Div(html.DivConfig{Class: "fui-content-row__workspace"},
			html.Section(html.SectionConfig{Label: label, Class: "fui-content-row__toolbar"}, cfg.Toolbar),
			render.Join(main...)))
	} else {
		body = append(body, main...)
	}
	if cfg.Aside != "" {
		label := cfg.AsideLabel
		if label == "" {
			label = i18nui.ContentRowAside.Text(cfg.Ctx)
		}
		var attrs html.Attrs
		if cfg.Viewport {
			// A Viewport aside scrolls on its own, and its content (an
			// activity feed) may hold nothing focusable: the region
			// takes focus itself so a keyboard can scroll it.
			attrs = html.Attrs{"tabindex": "0"}
		}
		body = append(body, html.Aside(html.AsideConfig{Label: label, Class: "fui-content-row__aside", ExtraAttrs: attrs}, cfg.Aside))
	}

	return contentRowStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, body...))
}

var contentRowStyle = registry.RegisterStyle("ui-content-row", contentRowCSS)

// rowBreakpointCSS is one posture of the sheet's responsive half, md or lg.
type rowBreakpointCSS struct {
	scope string
	maxw  string
	minw  string
}

func contentRowCSS(_ style.Theme) string {
	md := rowBreakpointCSS{scope: ":not(.fui-content-row--stack-below-lg)", maxw: "47.99rem", minw: "48rem"}
	lg := rowBreakpointCSS{scope: ".fui-content-row--stack-below-lg", maxw: "63.99rem", minw: "64rem"}
	return `.fui-content-row { display: flex; align-items: stretch; flex: 1 0 auto; }
.fui-content-row > main, .fui-content-row > .layout-content { flex: 1 1 auto; min-width: 0; }
.fui-content-row__nav { flex: 0 0 auto; background-color: var(--color-surface, #fff); border-inline-end: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7); }
.fui-content-row__workspace { flex: 1 1 auto; min-inline-size: 0; display: flex; flex-direction: column; }
.fui-content-row__workspace > main, .fui-content-row__workspace > .layout-content { flex: 1 1 auto; min-inline-size: 0; }
.fui-content-row__toolbar { flex: 0 0 auto; min-inline-size: 0; padding: var(--spacing-sm) var(--spacing-lg); border-block-end: var(--stroke-thin, 1px) solid var(--color-border); }
.fui-content-row__aside { flex: 0 0 var(--ui-content-row-aside-width, 18rem); min-inline-size: 0; padding: var(--spacing-lg); border-inline-start: var(--stroke-thin, 1px) solid var(--color-border); }
.fui-content-row__aside:has(> [data-cui-outlet]:empty) { display: none; }
/* An outlet filling the aside is as tall as the column, so a sticky
   part inside it (a contents list) has the column's height to stick
   in rather than its own. Stacked, the height resolves to auto. */
.fui-content-row__aside > [data-cui-outlet] { block-size: 100%; }
/* Dense: on a fine pointer the document takes the compact option
   values, declared at the root the way a theme boundary declares them,
   so the app's overlays (a drawer, a dialog, a floating menu) mounted
   outside the row tighten with it; a Themed scope inside redeclares
   its own. A mouse needs no 44px touch target, so the root lowers that
   token too: every control sized from it (search fields, checkboxes,
   menus, icon buttons) tightens with the density-sized ones. The
   control padding knob drops to the small step so a button's 10px
   default inset cannot push it past the control height. */
@media (pointer: fine) {
  :root:has(.fui-content-row--dense) {
    --spacing-touch-target: ` + compactControlHeight + `;
    --fui-density-control-h: ` + compactControlHeight + `;
    --fui-density-gap: ` + compactGap + `;
    --ui-control-padding-y: var(--spacing-sm, 4px);
  }
}
/* Sticky: the toolbar row stays at the top of the window at every
   width, painted over the content that scrolls beneath it. */
.fui-content-row--sticky .fui-content-row__toolbar { position: sticky; inset-block-start: var(--fui-content-row-top); z-index: var(--z-sticky, 200); background-color: var(--color-background, #fff); }
/* The frame sticks where the page says sticky parts start: the
   --ui-sticky-top a StickyHeader page stack publishes, or the window's
   top. The sticky toolbar is one control row tall (a control, its
   padding and its rule), and the workspace under it publishes the
   offset grown by that height as --ui-sticky-top: a sticky panel
   inside it (a FormFrame's side rail) stops under the toolbar instead
   of behind it. The root only reads the inherited value; redefining
   it there would make the property refer to itself. */
.fui-content-row--sticky { --fui-content-row-top: var(--ui-sticky-top, 0px); --ui-content-row-toolbar-block: var(--ui-content-row-toolbar-height, calc(var(--spacing-touch-target, 44px) + 2 * var(--spacing-sm, 4px) + var(--stroke-thin, 1px))); }
.fui-content-row--sticky > .fui-content-row__workspace { --ui-sticky-top: calc(var(--fui-content-row-top) + var(--ui-content-row-toolbar-block)); }
/* A focused control in the workspace scrolls clear of the toolbar and
   anything above it (WCAG 2.4.11): the workspace's --ui-sticky-top
   already holds both. */
:where(.fui-content-row--sticky > .fui-content-row__workspace :focus) {
  scroll-margin-block-start: calc(var(--ui-sticky-top, 0px) + var(--ui-sheet-bar-offset, 0px) + var(--spacing-sm, 4px));
}
.fui-content-row--sticky .fui-content-row__toolbar { box-sizing: border-box; min-block-size: var(--ui-content-row-toolbar-block); display: flex; flex-direction: column; justify-content: center; }
/* Viewport aside: the tighter padding applies below the breakpoint
   too, matching the shell (the phone column is denser everywhere). */
.fui-content-row--viewport .fui-content-row__aside { flex-basis: var(--ui-content-row-aside-width, 18rem); padding: var(--spacing-md); }
.fui-content-row--viewport { min-block-size: 0; flex: 1 0 auto; }
/* App frame: a padded content area beside the nav column. The padding
   is the page header's top inset too, so the header adds none. */
.fui-content-row--has-nav main, .fui-content-row--has-nav .layout-content {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xl, 24px);
  padding: clamp(var(--spacing-xl, 24px), 3vw, calc(var(--spacing-sm, 4px) * 10));
  --ui-page-header-inset: 0;
}
/* One padded content area per frame: a nested layer's layout-content
   inside it (a screen group's layer under the app layout) pads and
   gaps nothing more. */
.fui-content-row--has-nav main .layout-content,
.fui-content-row--has-nav .layout-content .layout-content {
  padding: 0;
  gap: 0;
}
` + md.viewportCSS() + md.stickyCSS() + md.stackCSS() + lg.viewportCSS() + lg.stickyCSS() + lg.stackCSS()
}

func (b rowBreakpointCSS) stickyCSS() string {
	css := fmt.Sprintf(`@media (min-width: %s) {
  .fui-content-row--sticky:SCOPE: .fui-content-row__nav { position: sticky; inset-block-start: var(--fui-content-row-top); align-self: flex-start; block-size: calc(100dvh - var(--fui-content-row-top)); overflow-y: auto; overscroll-behavior: contain; --ui-sticky-top: 0px; }
}
`, b.minw)
	return strings.ReplaceAll(css, ":SCOPE:", b.scope)
}

func (b rowBreakpointCSS) viewportCSS() string {
	css := fmt.Sprintf(`@media (min-width: %s) {
  :where(.fui-content-row--viewport):SCOPE: { block-size: calc(100dvh - var(--size-header-height, 56px)); min-block-size: 0; flex: 1 1 auto; overflow: hidden; }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace { min-block-size: 0; }
  .fui-content-row--viewport:SCOPE: .fui-content-row__nav,
  .fui-content-row--viewport:SCOPE: .fui-content-row__aside,
  .fui-content-row--viewport:SCOPE: > main,
  .fui-content-row--viewport:SCOPE: > .layout-content,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > main,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > .layout-content { min-block-size: 0; overflow-y: auto; --ui-sticky-top: 0px; }
  .fui-content-row--viewport:SCOPE: > main,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > main,
  .fui-content-row--viewport:SCOPE: > .layout-content,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > .layout-content { padding: var(--spacing-lg); }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace .layout-content .layout-content { padding: 0; gap: 0; }
}
`, b.minw)
	return strings.ReplaceAll(css, ":SCOPE:", b.scope)
}

func (b rowBreakpointCSS) stackCSS() string {
	css := fmt.Sprintf(`@media (max-width: %s) {
  :where(.fui-content-row):SCOPE: { display: block; }
  .fui-content-row:SCOPE: .fui-content-row__nav { border-inline-end: none; border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7); }
  /* The stacked nav band holds the menu trigger. It takes main's inline
     gutter, so the trigger lines up with the page content instead of
     sitting flush in the viewport corner. */
  .fui-content-row--has-nav:SCOPE: .fui-content-row__nav { padding: var(--spacing-sm, 4px) clamp(var(--spacing-xl, 24px), 3vw, calc(var(--spacing-sm, 4px) * 10)); }
  .fui-content-row--viewport.fui-content-row--has-nav:SCOPE: .fui-content-row__nav { padding: var(--spacing-sm, 4px) var(--spacing-md, 8px); }
  .fui-content-row:SCOPE: .fui-content-row__aside { border-inline-start: none; border-block-start: var(--stroke-thin, 1px) solid var(--color-border); }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > main,
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace > .layout-content { padding: var(--spacing-md); }
  .fui-content-row--viewport:SCOPE: .fui-content-row__workspace .layout-content .layout-content { padding: 0; gap: 0; }
  /* PhoneNavFlush: the caller says the sidebar's phone navigation lives
     outside the column (a SectionMenu's floating pill, a NativeMobile
     sidebar whose trigger the page header hosts), so the stacked band
     renders empty and must not draw its separator. */
  .fui-content-row--phone-nav-flush:SCOPE: .fui-content-row__nav { border: 0; padding: 0; }
  /* PhoneAsideFlush: the same for the aside, whose phone form (a
     contents list's pill and sheet) floats over the page. */
  .fui-content-row--phone-aside-flush:SCOPE: .fui-content-row__aside { border: 0; padding: 0; }
  /* An aside whose phone form is a bar pinned under the header (a
     contents list's PhoneSheetTop) moves above the main column and
     sticks there itself: the bar alone is as tall as the aside, so it
     has no room to stick in, and stacked last it would trail the
     article. The row turns to a column so the aside can move. */
  :where(.fui-content-row:has(> .fui-content-row__aside .cui-sheet-pill--bar)):SCOPE: { display: flex; flex-direction: column; }
  .fui-content-row:SCOPE: .fui-content-row__nav { order: -2; }
  .fui-content-row:SCOPE: .fui-content-row__aside:has(.cui-sheet-pill--bar) { order: -1; position: sticky; inset-block-start: var(--ui-sticky-top, 0px); z-index: calc(var(--z-sticky, 200) - 1); border: 0; padding: 0; }
}
`, b.maxw)
	return strings.ReplaceAll(css, ":SCOPE:", b.scope)
}
