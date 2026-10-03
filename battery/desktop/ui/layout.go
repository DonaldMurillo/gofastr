package desktopui

import (
	"strconv"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// LayoutName is the desktop layout's name. It lands in the wrapper's
// class (layout-desktop) and its data-fui-layout attribute, so it is
// part of the CSS contract, not a private string.
const LayoutName = "desktop"

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

// Layout returns the desktop layout: the core-ui layout shell with the
// sidebar zone at the declared width, the measured traffic-light zone
// reserved at the top of the sidebar, transparent html/body so the
// native material shows through, and an opaque content column.
//
// Chain it the way any layout is chained, the sidebar slot usually
// holding a SourceList:
//
//	layout := desktopui.Layout().WithSidebar(
//		app.NewStaticComponent(desktopui.SourceList(cfg)))
//	site.Register("/", screen, layout)
//
// The transparent page is safe in every mode: when the shell leaves
// the webview background on, the webview's own background paints
// behind the transparent page (the material-none case); when the shell
// turns it off, the native material shows. In a plain browser
// (--serve) the page falls back to the UA background, the same
// degraded mode core-ui's widget layout accepts.
//
// The traffic-light reservation assumes the sidebar zone holds the
// lights (ChromeHiddenTitle / hiddenInset): without a sidebar there is
// no reservation, and a host that runs headerless without a sidebar
// pads its own first row.
func Layout() *appui.Layout {
	return appui.NewLayout(LayoutName)
}

// WindowLayoutName is the sidebar-less desktop layout's name
// (class layout-desktop-window).
const WindowLayoutName = "desktop-window"

// WindowLayout returns the desktop layout for a window with no sidebar:
// a settings window, an about panel, any small secondary window. html,
// body, and the content column all paint nothing, so a whole-window
// material (MaterialWindow, MaterialGlass) is the surface, and the
// content column reserves the measured traffic-light zone at its top
// for a unified or hidden-title window.
//
// A screen shown both in the main window and in a small window needs
// two registrations (or one window only): Layout() stacks nothing, but
// a 220-point sidebar in a 480-point window leaves the form a strip.
func WindowLayout() *appui.Layout {
	return appui.NewLayout(WindowLayoutName)
}

var layoutStyle = registry.RegisterStyle("desktopui-layout", layoutCSS,
	registry.WithLoad(registry.LoadAlways))

func layoutCSS(_ style.Theme) string {
	return `/* Desktop layout: the window is the frame. html and body paint
   nothing so the native material (vibrancy or glass) placed behind
   the webview shows through; the content column paints its own opaque
   background so the document stays legible. Same rule shape as
   core-ui's widget layout. */
html:has(.layout-desktop), body:has(.layout-desktop),
html:has(.layout-desktop-window), body:has(.layout-desktop-window) { background-color: transparent; }

/* Control density. A desktop window is driven by a pointer, not a
   thumb: the theme drops --spacing-touch-target to the 24-point
   WCAG 2.5.8 floor, and this knob drops the block padding every
   framework control shares (buttons, text fields, selects), so a push
   button or a text field lands near the native 24 to 30 points instead
   of the web's 44. Set on html so portaled overlays (a modal's form)
   inherit it too. Measured, unverified. */
html:has(.layout-desktop), html:has(.layout-desktop-window) {
  --ui-control-padding-y: 4px;
}

/* The zone knobs. --desktop-sidebar-width mirrors the shell Config's
   declared sidebar width (default measured against native sidebars,
   unverified); --desktop-sidebar-top-inset is the measured
   traffic-light zone (see SidebarTopInset); --desktop-sidebar-surface
   is transparent in light mode (the native sidebar material under the
   zone is the surface) and a faint label tint in dark mode. */
.layout-desktop {
  --desktop-sidebar-width: ` + strconv.Itoa(DefaultSidebarWidth) + `px;
  --desktop-sidebar-top-inset: ` + strconv.Itoa(SidebarTopInset) + `px;
  --desktop-sidebar-surface: transparent;
}
/* The sidebar zone: at the declared width, with the measured
   traffic-light zone reserved at the top. */
.layout-desktop .layout-body > nav {
  flex-basis: var(--desktop-sidebar-width, 220px);
  flex-grow: 0;
  flex-shrink: 0;
  block-size: auto;
  background: var(--desktop-sidebar-surface, transparent);
  border-right: none;
  padding-top: var(--desktop-sidebar-top-inset, 52px);
}
/* Dark mode: the dark vibrancy sidebar material lands on the same
   near-black as the content background (#1E1E1E), so the split that
   reads in light mode disappears (measured against the native Notes
   capture: its dark sidebar sits about 3/255 above its content). A 2%
   wash of the label color over the zone lands within a couple of
   points of that lift while the material still shows through. */
:root[data-color-scheme="dark"] .layout-desktop {
  --desktop-sidebar-surface: color-mix(in srgb, var(--color-text, #F5F5F7) 2%, transparent);
}
@media (prefers-color-scheme: dark) {
  :root:not([data-color-scheme="light"]) .layout-desktop {
    --desktop-sidebar-surface: color-mix(in srgb, var(--color-text, #F5F5F7) 2%, transparent);
  }
}
/* The content column: the one opaque region, so text sits on a solid
   page even where the window material is translucent. The start
   corners round into the window frame; measured, unverified. */
.layout-desktop .layout-body > main,
.layout-desktop .layout-body > .layout-content {
  background-color: var(--color-background, #FFFFFF);
  border-start-start-radius: var(--radii-lg, 12px);
}
/* A desktop window never collapses its sidebar into a stacked strip:
   core-ui's layout stacks the nav above the content under 48rem (the
   phone shape), which in a narrow window put the whole source list on
   top of the page. The row holds at every width; a host that wants a
   narrow window uses WindowLayout. */
@media (max-width: 47.99rem) {
  .layout-desktop .layout-body { display: flex; }
  .layout-desktop .layout-body > nav { border-bottom: none; }
}

/* The sidebar-less window: no zone, no opaque column. The window
   material is the surface; the content column clears the traffic
   lights with the same measured zone the sidebar reserves. */
.layout-desktop-window .layout-body > main,
.layout-desktop-window .layout-body > .layout-content {
  background-color: transparent;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-lg, 16px);
  padding: ` + strconv.Itoa(SidebarTopInset) + `px var(--spacing-xl, 24px) var(--spacing-xl, 24px);
}
`
}
