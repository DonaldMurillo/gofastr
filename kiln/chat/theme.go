package chat

import (
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core-ui/widget/theme"
	"github.com/gofastr/gofastr/kiln/world"
)

// pageCSS resolves the kiln page stylesheet through core-ui/widget/theme.
// World.App.Theme overrides (e.g. set by a future set_theme tool) merge
// on top of the framework default.
func pageCSS() string {
	return pageCSSFor(nil)
}

// pageCSSFor resolves the page CSS with optional world-level overrides.
// Used by /kiln/theme.css when the kiln server has access to the live
// session and can pull the current world.App.Theme.
func pageCSSFor(app *world.AppConfig) string {
	t := theme.PageTheme()
	applyAppOverrides(&t, app)
	return theme.PageCSS(t)
}

// applyAppOverrides walks world.App.Theme (a flat name→value map)
// and overrides matching canonical color tokens in t. Keys are
// matched against the Color.Name field of each color in t.Colors.
//
// Today only color overrides are exposed via set_theme; spacing /
// radii / font overrides can land later by extending this function.
func applyAppOverrides(t *style.Theme, app *world.AppConfig) {
	if app == nil || len(app.Theme) == 0 {
		return
	}
	for k, v := range app.Theme {
		switch k {
		case "primary":
			t.Colors.Primary = style.Color{Name: t.Colors.Primary.Name, Value: v}
		case "primary-fg":
			t.Colors.PrimaryFg = style.Color{Name: t.Colors.PrimaryFg.Name, Value: v}
		case "secondary":
			t.Colors.Secondary = style.Color{Name: t.Colors.Secondary.Name, Value: v}
		case "background":
			t.Colors.Background = style.Color{Name: t.Colors.Background.Name, Value: v}
		case "surface":
			t.Colors.Surface = style.Color{Name: t.Colors.Surface.Name, Value: v}
		case "surface-soft":
			t.Colors.SurfaceSoft = style.Color{Name: t.Colors.SurfaceSoft.Name, Value: v}
		case "text":
			t.Colors.Text = style.Color{Name: t.Colors.Text.Name, Value: v}
		case "text-muted":
			t.Colors.TextMuted = style.Color{Name: t.Colors.TextMuted.Name, Value: v}
		case "text-subtle":
			t.Colors.TextSubtle = style.Color{Name: t.Colors.TextSubtle.Name, Value: v}
		case "border":
			t.Colors.Border = style.Color{Name: t.Colors.Border.Name, Value: v}
		case "border-strong":
			t.Colors.BorderStrong = style.Color{Name: t.Colors.BorderStrong.Name, Value: v}
		case "accent":
			t.Colors.Accent = style.Color{Name: t.Colors.Accent.Name, Value: v}
		case "success":
			t.Colors.Success = style.Color{Name: t.Colors.Success.Name, Value: v}
		case "warning":
			t.Colors.Warning = style.Color{Name: t.Colors.Warning.Name, Value: v}
		case "danger":
			t.Colors.Danger = style.Color{Name: t.Colors.Danger.Name, Value: v}
		case "info":
			t.Colors.Info = style.Color{Name: t.Colors.Info.Name, Value: v}
		}
	}
}
