package theme

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// Brutal is the framework theme re-skinned as neo-brutalism: the same
// token names with square corners, 2px strokes, hard offset shadows,
// Archivo type and a yellow primary on cream, in light and dark. The
// shadows are drawn in the border colour, so they flip with the scheme
// (black on cream, cream on near-black). Register it as a page theme
// (style.RegisterThemeOverride) and offer it in a ui.ThemePicker, or
// build an app on it.
//
// Archivo is named first in the font stacks; an app that wants the face
// itself self-hosts it (the strict CSP blocks font CDNs), and without it
// the stack falls back to the system sans.
func Brutal() style.Theme {
	const ink, cream = "#111111", "#F5EFD6"
	archivo := `"Archivo", ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif`
	t := Default(Overrides{
		Background: "#FFF6D8", Surface: "#FFFFFF", SurfaceSoft: "#FFE9A3",
		Border: ink, BorderStrong: ink,
		Text: ink, TextMuted: "#2B2B2B", TextSubtle: "#4A4A4A",
		Primary: "#FFD60A", PrimaryFg: ink, Accent: ink,
		FontBody: archivo, FontHeading: archivo,
		Dark: &Overrides{
			Background: "#141209", Surface: "#1D1A0E", SurfaceSoft: "#2A2512",
			Border: cream, BorderStrong: cream,
			Text: cream, TextMuted: "#E6DFC2", TextSubtle: "#C9C1A0",
			Primary: "#FFD60A", PrimaryFg: ink, Accent: "#FFD60A",
		},
	})
	t.Name = "brutal"
	for _, r := range []*style.Radius{&t.Radii.SM, &t.Radii.MD, &t.Radii.LG, &t.Radii.XL, &t.Radii.Full} {
		r.Value = 0
	}
	t.Strokes.Thin.Value = "2px"
	t.Strokes.Thick.Value = "3px"
	hard := func(px string) string { return px + " " + px + " 0 var(--color-border, " + ink + ")" }
	t.Shadows.XS.Value = hard("2px")
	t.Shadows.SM.Value = hard("3px")
	t.Shadows.MD.Value = hard("4px")
	t.Shadows.LG.Value = hard("5px")
	t.Shadows.XL.Value = hard("6px")
	return t
}
