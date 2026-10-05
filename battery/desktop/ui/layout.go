package desktopui

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

// LayoutName is the desktop layout's name. It lands in the wrapper's
// class (layout-desktop) and its data-cui-layout attribute, so it is
// part of the CSS contract, not a private string.
const LayoutName = "desktop"

// WindowLayoutName is the sidebar-less desktop layout's name
// (class layout-desktop-window).
const WindowLayoutName = "desktop-window"

// WidgetLayoutName is the floating-widget layout's name (class
// layout-desktop-widget).
const WidgetLayoutName = "desktop-widget"

// SidebarTopInset is the top padding the sidebar column reserves for
// the traffic-light zone, in points. MEASURED against the native
// Notes capture (scratchpad focus-shots-13/light-active-notes.png,
// 2026-09-09): the traffic lights occupy 19 to 32.5 pt from the
// window's top edge and the first sidebar row's box top sits 51.5 pt
// below it, so 52 pt of empty sidebar precedes the first row. The
// number comes from the capture, not from a source. A shell that
// moves the lights further down overrides --desktop-sidebar-top-inset
// to match.
const SidebarTopInset = 52

// DefaultSidebarWidth is the sidebar zone's default width in points
// (measured against Finder/Notes sidebars, unverified). A host whose
// Config declares a different width overrides --desktop-sidebar-width
// with it; the knob lives here because the canonical style.Theme
// cannot carry non-canonical tokens (the same reasoning as the admin
// battery's --admin-rail).
const DefaultSidebarWidth = 220

// Layout returns the desktop layout: the sidebar zone at the declared
// width with the measured traffic-light zone reserved at its top,
// transparent html/body so the native material shows through, and an
// opaque content column. The sidebar is usually a SourceList:
//
//	layout := desktopui.Layout(
//		app.NewStaticComponent(desktopui.SourceList(cfg)))
//	site.Register("/", screen, layout)
//
// The frame is the desktop's own, not ui.ContentRow: a desktop window
// never stacks its sidebar above the content (a narrow window keeps
// the row; a small secondary window uses WindowLayout), and the
// sidebar column paints nothing so the native sidebar material is its
// surface.
//
// The transparent page is safe in every mode: when the shell leaves
// the webview background on, the webview's own background paints
// behind the transparent page (the material-none case); when the shell
// turns it off, the native material shows. In a plain browser
// (--serve) the page falls back to the UA background.
//
// The traffic-light reservation assumes the sidebar zone holds the
// lights (ChromeHiddenTitle / hiddenInset). A nil sidebar renders the
// content column alone; a host that runs headerless without a sidebar
// uses WindowLayout, which reserves the zone above the content.
func Layout(sidebar component.Component) *appui.Layout {
	return appui.NewLayout(LayoutName, appui.LayoutSpec{}, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		var parts []render.HTML
		if sidebar != nil {
			nav, _ := component.SafeRenderCtx(ctx, sidebar)
			parts = append(parts, html.Nav(html.NavConfig{Label: "Sidebar", Class: "desktopui-frame__sidebar"}, nav))
		}
		parts = append(parts, l.Primary())
		return html.Div(html.DivConfig{Class: "desktopui-frame"}, parts...)
	})
}

// WindowLayout returns the desktop layout for a window with no sidebar:
// a settings window, an about panel, any small secondary window. html,
// body, and the content column all paint nothing, so a whole-window
// material (MaterialWindow, MaterialGlass) is the surface, and the
// content column reserves the measured traffic-light zone at its top
// for a unified or hidden-title window.
//
// A screen shown both in the main window and in a small window needs
// two registrations (or one window only): a 220-point sidebar in a
// 480-point window leaves the form a strip.
func WindowLayout() *appui.Layout {
	return appui.NewLayout(WindowLayoutName, appui.LayoutSpec{}, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		return html.Div(html.DivConfig{Class: "desktopui-frame desktopui-frame--window"}, l.Primary())
	})
}

// WidgetLayout returns the chrome-less, transparent layout a floating
// desktop widget renders under.
//
// A widget is a small borderless window (desktop.Widget): no title
// bar, a transparent window background, often pinned above other
// apps. Inside it an app layout is wrong three times over: a header
// and footer have nowhere to go, a padded content column eats a
// 320-point window, and the page's own background paints an opaque
// rectangle behind whatever the screen draws, so the "transparent"
// window shows a white slab with the theme's corners cut off. Caught
// in a screenshot of the first widget; invisible to any DOM assertion.
//
// So the widget layout has no chrome, only the <main> landmark; the
// page is transparent behind it and has no viewport-height floor, so
// the screen's own surface (a ui.Card, say) is the whole visible
// window. --desktop-widget-padding is the gap between the window edge
// and that surface (default 8px).
func WidgetLayout() *appui.Layout {
	return appui.NewLayout(WidgetLayoutName, appui.LayoutSpec{}, func(ctx context.Context, l *appui.LayoutTree) render.HTML {
		return html.Div(html.DivConfig{Class: "desktopui-frame desktopui-frame--widget"}, l.Primary())
	})
}

var layoutStyle = registry.RegisterStyle("desktopui-layout", layoutCSS,
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
  --desktop-sidebar-surface: color-mix(in srgb, var(--color-text, #F5F5F7) 2%, transparent);
}
@media (prefers-color-scheme: dark) {
  :root:not([data-color-scheme="light"]) .desktopui-frame {
    --desktop-sidebar-surface: color-mix(in srgb, var(--color-text, #F5F5F7) 2%, transparent);
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
  border-start-start-radius: var(--radii-lg, 10px);
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
