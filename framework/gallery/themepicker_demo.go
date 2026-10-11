package gallery

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// squareTheme is the ThemePicker tile's page theme: the default look
// with square corners and a warm accent, in light and dark, so picking
// it visibly restyles the whole showcase page. Registered at init, so
// its block is in app.css before the first request. It starts from
// theme.Default because the gallery cannot see its host's theme, so
// on a host with its own spacing or fonts the switch changes those
// too (theming → "Page themes", an override is a whole theme).
var squareTheme = style.RegisterThemeOverride(squareThemeValue())

func squareThemeValue() style.Theme {
	t := theme.Default(theme.Overrides{
		Primary: "#C2410C", PrimaryFg: "#FFFFFF",
		Dark: &theme.Overrides{Primary: "#FB923C", PrimaryFg: "#1C0A00"},
	})
	for _, r := range []*style.Radius{&t.Radii.SM, &t.Radii.MD, &t.Radii.LG, &t.Radii.XL, &t.Radii.Full} {
		r.Value = 0
	}
	return t
}
