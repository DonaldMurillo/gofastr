package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// FloatingToolbar is the macOS floating toolbar group: ui.Toolbar
// wrapped in the thin glass surface with a capsule radius.
//
// It composes the framework component instead of forking it, the
// wrapper-variant shape the desktop plan settled on: ui.Toolbar keeps
// its semantics (role=toolbar, labelled groups, separators) and its
// runtime behavior; this stylesheet restyles only the chrome (surface,
// border, radius) inside the wrapper's scope, at higher specificity
// than ui-toolbar's own rules.
//
// The capsule radius is the concentric rule's capsule case (half the
// container height) expressed through --radii-full. The wrapper floats
// (sticky, top, centered, fit-content) at the theme's sticky z tier.
func FloatingToolbar(cfg ui.ToolbarConfig) render.HTML {
	inner := Glass(GlassConfig{}, ui.Toolbar(cfg))
	return floatingToolbarStyle.WrapHTML(render.Tag("div", html.Attrs{
		"class": "desktopui-floating-toolbar",
	}, inner))
}

var floatingToolbarStyle = registry.RegisterStyle("desktopui-floating-toolbar", floatingToolbarCSS)

func floatingToolbarCSS(_ style.Theme) string {
	return `[data-fui-comp="desktopui-floating-toolbar"] {
  /* Floats over the content column: detached from the window edges the
     way a macOS 26 floating toolbar group sits. */
  position: sticky;
  top: var(--spacing-md, 8px);
  z-index: var(--z-sticky, 200);
  margin-inline: auto;
  inline-size: fit-content;
}
[data-fui-comp="desktopui-floating-toolbar"] [data-fui-comp="desktopui-glass"] {
  /* Capsule: the concentric rule's capsule case. */
  border-radius: var(--radii-full, 9999px);
}
[data-fui-comp="desktopui-floating-toolbar"] [data-fui-comp="ui-toolbar"] {
  /* Strip the framework toolbar's own chrome: the glass surface is the
     chrome now. This is the wrapper-variant contract, not an outside
     override: the rule ships in a registered component stylesheet. */
  background: transparent;
  border: none;
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  border-radius: var(--radii-full, 9999px);
}
`
}
