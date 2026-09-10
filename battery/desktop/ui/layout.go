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

var layoutStyle = registry.RegisterStyle("desktopui-layout", layoutCSS,
	registry.WithLoad(registry.LoadAlways))

func layoutCSS(_ style.Theme) string {
	return `/* Desktop layout: the window is the frame. html and body paint
   nothing so the native material (vibrancy or glass) placed behind
   the webview shows through; the content column paints its own opaque
   background so the document stays legible. Same rule shape as
   core-ui's widget layout. */
html:has(.layout-desktop), body:has(.layout-desktop) { background-color: transparent; }

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
`
}
