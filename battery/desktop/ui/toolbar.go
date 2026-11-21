package desktopui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// its labelled group semantics, separators and runtime behavior.
// This stylesheet restyles only the chrome (surface,
func FloatingToolbar(cfg ui.ToolbarConfig) render.HTML {
	inner := Glass(GlassConfig{}, ui.Toolbar(cfg))
	return floatingToolbarStyle.WrapHTML(render.Tag("div", html.Attrs{
		"class": "desktopui-floating-toolbar",
	}, inner))
}

var floatingToolbarStyle = registry.RegisterStyle("desktopui-floating-toolbar", floatingToolbarCSS)

func floatingToolbarCSS(_ style.Theme) string {
	return `[data-cui-comp="desktopui-floating-toolbar"] {
  /* Floats over the content column: detached from the window edges the
     way a macOS 26 floating toolbar group sits. */
  position: sticky;
  top: calc(var(--ui-sticky-top, 0px) + var(--spacing-md, 8px));
  z-index: var(--z-sticky, 200);
  margin-inline: auto;
  inline-size: fit-content;
}
[data-cui-comp="desktopui-floating-toolbar"] [data-cui-comp="desktopui-glass"] {
  /* Capsule: the concentric rule's capsule case. */
  border-radius: var(--radii-full, 9999px);
}
[data-cui-comp="desktopui-floating-toolbar"] [data-cui-comp="ui-toolbar"] {
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
