package desktopui

//gofastr:allow-file(GOFASTR1808) the desktop shell renders under its own theme
// (theme.go, radii 4/8/12/16), so these radius fallbacks restate that scale, not
// style.DefaultTheme's 6/8/10/14 the rule judges them against.

import (
	"context"
	"strconv"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// LayoutName is the desktop layout's name.
const LayoutName = "desktop"

// WindowLayoutName is the sidebar-less desktop layout's name (class layout-desktop-window).
const WindowLayoutName = "desktop-window"

// WidgetLayoutName is the floating-widget layout's name (class layout-desktop-widget).
const WidgetLayoutName = "desktop-widget"

// SidebarTopInset is the top padding the sidebar column reserves for the traffic-light zone, in points.
const SidebarTopInset = 52

// DefaultSidebarWidth is the sidebar zone's default width in points.
const DefaultSidebarWidth = 220

// Layout returns the desktop layout: a sidebar zone beside an opaque content column on a transparent page.
func Layout(sidebar component.Component) *appui.Layout {
	return appui.NewLayout(LayoutName, appui.LayoutSpec{}, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		var parts []render.HTML
		if sidebar != nil {
			nav, _ := component.SafeRenderCtx(ctx, sidebar)
			parts = append(parts, html.Nav(html.NavConfig{Label: sidebarLabel.Text(ctx), Class: "desktopui-frame__sidebar"}, nav))
		}
		parts = append(parts, l.Primary())
		return html.Div(html.DivConfig{Class: "desktopui-frame"}, parts...)
	})
}

// WindowLayout returns the desktop layout for a window with no sidebar.
func WindowLayout() *appui.Layout {
	return appui.NewLayout(WindowLayoutName, appui.LayoutSpec{}, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		return html.Div(html.DivConfig{Class: "desktopui-frame desktopui-frame--window"}, l.Primary())
	})
}

// WidgetLayout returns the chrome-less, transparent layout a floating desktop widget renders under.
func WidgetLayout() *appui.Layout {
	return appui.NewLayout(WidgetLayoutName, appui.LayoutSpec{}, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		return html.Div(html.DivConfig{Class: "desktopui-frame desktopui-frame--widget"}, l.Primary())
	})
}

var _ = registry.RegisterStyle("desktopui-layout", layoutCSS,
	registry.WithLoad(registry.LoadAlways))

func layoutCSS(_ style.Theme) string {
	return `/* Desktop frames: the window is the frame. html and body paint
   nothing so the native material (vibrancy or glass) placed behind
   the webview shows through. */
html:has(.desktopui-frame), body:has(.desktopui-frame) { background-color: transparent; }

/* Control density. A desktop window is driven by a pointer, not a
   thumb: the theme drops --spacing-touch-target to the 24-point
   WCAG 2.5.8 floor, and this knob drops the block padding every
   framework control shares (buttons, text fields, selects), so a push
   button or a text field lands near the native 24 to 30 points instead
   of the web's 44. Set on html so portaled overlays (a modal's form)
   inherit it too. Measured, unverified. */
html:has(.desktopui-frame) {
  --ui-control-padding-y: 4px;
}

/* The zone knobs. --desktop-sidebar-width mirrors the shell Config's
   declared sidebar width (default measured against native sidebars,
   unverified); --desktop-sidebar-top-inset is the measured
   traffic-light zone (see SidebarTopInset); --desktop-sidebar-surface
   is transparent in light mode (the native sidebar material under the
   zone is the surface) and a faint label tint in dark mode. */
.desktopui-frame {
  --desktop-sidebar-width: ` + strconv.Itoa(DefaultSidebarWidth) + `px;
  --desktop-sidebar-top-inset: ` + strconv.Itoa(SidebarTopInset) + `px;
  --desktop-sidebar-surface: transparent;
  display: flex;
  align-items: stretch;
  min-block-size: 100vh;
}
/* The sidebar zone: at the declared width, with the measured
   traffic-light zone reserved at the top. A desktop window never
   stacks it above the content: there is no narrow-width rule. */
.desktopui-frame__sidebar {
  flex: 0 0 var(--desktop-sidebar-width, 220px);
  min-inline-size: 0;
  background: var(--desktop-sidebar-surface, transparent);
  padding-top: var(--desktop-sidebar-top-inset, 52px);
}
/* Dark mode: the dark vibrancy sidebar material lands on the same
   near-black as the content background (#1E1E1E), so the split that
   reads in light mode disappears (measured against the native Notes
   capture: its dark sidebar sits about 3/255 above its content). A 2%
   wash of the label color over the zone lands within a couple of
   points of that lift while the material still shows through. */
:root[data-color-scheme="dark"] .desktopui-frame {
  --desktop-sidebar-surface: color-mix(in oklab, var(--color-text, #F5F5F7) 2%, transparent);
}
@media (prefers-color-scheme: dark) {
  :root:not([data-color-scheme="light"]) .desktopui-frame {
    --desktop-sidebar-surface: color-mix(in oklab, var(--color-text, #F5F5F7) 2%, transparent);
  }
}
/* The content column: the one opaque region, so text sits on a solid
   page even where the window material is translucent. The start
   corners round into the window frame; measured, unverified. */
.desktopui-frame > main,
.desktopui-frame > .layout-content {
  flex: 1 1 auto;
  min-inline-size: 0;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-lg, 16px);
  padding: var(--spacing-xl, 24px);
  background-color: var(--color-background, #FFFFFF);
  border-start-start-radius: var(--radii-lg, 12px);
}

/* The sidebar-less window: no zone, no opaque column. The window
   material is the surface; the content column clears the traffic
   lights with the same measured zone the sidebar reserves. */
.desktopui-frame--window > main,
.desktopui-frame--window > .layout-content {
  background-color: transparent;
  border-radius: 0;
  padding: ` + strconv.Itoa(SidebarTopInset) + `px var(--spacing-xl, 24px) var(--spacing-xl, 24px);
}

/* The widget: no viewport-height floor and no column gutter (a
   320-point window has no room for one), and nothing painted behind
   the screen's own surface. --desktop-widget-padding is the gap
   between the window edge and that surface. */
.desktopui-frame--widget {
  min-block-size: 0;
}
.desktopui-frame--widget > main,
.desktopui-frame--widget > .layout-content {
  background-color: transparent;
  border-radius: 0;
  padding: var(--desktop-widget-padding, 8px);
}
`
}
