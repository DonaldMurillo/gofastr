package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ResponsiveConfig configures the swap.
type ResponsiveConfig struct {
	// Below is the breakpoint the mobile variant shows below: StackBelowMD (48rem, the
	// default) or StackBelowLG (64rem). Other values panic.
	Below StackBreakpoint
	// Class is appended to the wrapping <div>'s class list.
	Class string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// Responsive renders a desktop and a mobile variant and shows one per viewport.
func Responsive(cfg ResponsiveConfig, desktop, mobile render.HTML) render.HTML {
	checkStackBreakpoint(cfg.Below)
	cls := "fui-responsive"
	if cfg.Below == StackBelowLG {
		cls += " fui-responsive--stack-below-lg"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	desktopAttrs := html.Attrs{}
	if desktop == "" {
		desktopAttrs["data-cui-internal"] = ""
	}
	mobileAttrs := html.Attrs{}
	if mobile == "" {
		mobileAttrs["data-cui-internal"] = ""
	}
	return responsiveStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	},
		html.Div(html.DivConfig{
			Class:      "fui-responsive__desktop",
			ExtraAttrs: desktopAttrs,
		}, desktop),
		html.Div(html.DivConfig{
			Class:      "fui-responsive__mobile",
			ExtraAttrs: mobileAttrs,
		}, mobile),
	))
}

var responsiveStyle = registry.RegisterStyle("ui-responsive", responsiveCSS)

func responsiveCSS(_ style.Theme) string {
	return `
@media (min-width: 48rem) { .fui-responsive:not(.fui-responsive--stack-below-lg) > .fui-responsive__mobile { display: none !important; } }
@media (max-width: 47.99rem) { .fui-responsive:not(.fui-responsive--stack-below-lg) > .fui-responsive__desktop { display: none !important; } }
@media (min-width: 64rem) { .fui-responsive--stack-below-lg > .fui-responsive__mobile { display: none !important; } }
@media (max-width: 63.99rem) { .fui-responsive--stack-below-lg > .fui-responsive__desktop { display: none !important; } }
`
}
