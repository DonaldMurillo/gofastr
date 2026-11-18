package gallery

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

var squareTheme = style.RegisterThemeOverride(squareThemeValue())

var squareName = i18n.Define("gallery.themepicker.square", "Square")

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
