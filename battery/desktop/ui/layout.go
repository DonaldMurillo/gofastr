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

// TrafficLightInset is the traffic-light inset (x and y, points) this
// layout reserves above the sidebar. It is the value the shell-side
// contract's TrafficLightInset defaults to: Electron's hiddenInset
// margin is (12, 11) and its own source comment says it does not match
// native apps; the desktop contract rounds to 12x12 so the page
// reservation and the native button placement agree. A shell that
// moves the lights overrides --desktop-traffic-inset to match.
const TrafficLightInset = 12

// DefaultSidebarWidth is the sidebar zone's default width in points
// (measured against Finder/Notes sidebars, unverified). A host whose
// Config declares a different width overrides --desktop-sidebar-width
// with it; the knob lives here because the canonical style.Theme
// cannot carry non-canonical tokens (the same reasoning as the admin
// battery's --admin-rail).
const DefaultSidebarWidth = 220

// Layout returns the desktop layout: the core-ui layout shell with the
// sidebar zone at the declared width, the traffic-light inset reserved
// at the top of the sidebar, transparent html/body so the native
// material shows through, and an opaque content column.
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
   unverified); --desktop-traffic-inset mirrors the shell's
   TrafficLightInset default (12x12, see the constant). */
.layout-desktop {
  --desktop-sidebar-width: ` + strconv.Itoa(DefaultSidebarWidth) + `px;
  --desktop-traffic-inset: ` + strconv.Itoa(TrafficLightInset) + `px;
}
/* The sidebar zone: transparent (the native sidebar material placed
   under it is the surface), at the declared width, with the
   traffic-light inset reserved at the top. */
.layout-desktop .layout-body > nav {
  flex-basis: var(--desktop-sidebar-width, 220px);
  flex-grow: 0;
  flex-shrink: 0;
  block-size: auto;
  background: transparent;
  border-right: none;
  padding-top: var(--desktop-traffic-inset, 12px);
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
