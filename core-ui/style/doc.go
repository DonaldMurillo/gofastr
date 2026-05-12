// Package style provides the typed design system and CSS builders
// for the GoFastr core-ui framework.
//
// # Theme — the typed design system
//
// A Theme is a Go struct of typed token value types (Color, Spacing,
// Radius, Font, etc.). Every primitive token is required; passing a
// half-populated Theme to app.WithTheme panics at startup with a path
// to the missing field. Names auto-derive from struct-field paths
// (Colors.PrimaryFg → "primary-fg") via AutoFillNames so authors only
// write Values:
//
//	theme := style.Theme{
//	    Colors: style.ColorSet{Primary: style.Color{Value: "#4F46E5"}, ...},
//	    Spacing: style.SpacingScale{XS: style.Spacing{Value: 2}, ...},
//	    ...
//	    Layout: style.LayoutSet{TouchTarget: style.Spacing{Value: 44}},
//	}
//	style.AutoFillNames(&theme)
//	theme.MustValidate()
//
// At render time tokens emit CSS-variable references — never literal
// hex — so section-level theme overrides via the CSS cascade work:
//
//	theme.App.Colors.Primary.CSS()    // → "var(--color-primary)"
//	theme.App.Colors.Primary.Value    // → "#4F46E5"
//
// # ComponentSheet — auto-scoped per-component CSS
//
// NewComponentSheet(name, theme) returns a builder that prefixes every
// rule with [data-fui-comp="<name>"] at Build() time. The framework
// emits the marker attribute on the component's outermost tag (via
// the registry package), so the scope is byte-for-byte deterministic
// and CSP-clean. Selectors that can't be scoped (body, html, :root,
// ::backdrop, …) cause Build() to return an error wrapping
// ErrUnscopable. Detect it in a build script or lint:
//
//	_, err := style.NewComponentSheet("modal", theme).
//	    Rule("body").Set("margin", "0").End().
//	    Build()
//	if errors.Is(err, style.ErrUnscopable) {
//	    // move the rule to theme.go / uihost.WithCustomCSS
//	}
//
//	func modalCSS(t style.Theme) string {
//	    return style.NewComponentSheet("modal", t).
//	        Rule(".header").Set("font-weight", "{fonts.weight.bold}").End().
//	        Rule(".body").  Set("padding",     "{spacing.lg}").End().
//	        MustBuild()
//	}
//
// # StyleSheet — the lower-level builder
//
// NewStyleSheet(theme) returns the global-style builder used by
// theme.go and app-level CSS. Rule/Set/Pseudo/Child/Media/Container/
// Keyframes; token references like {colors.primary} resolve to
// var(--color-primary). Odd-count Set args, Set-before-Rule, and
// most other foot-guns panic with a useful message.
//
// # Section-level overrides — fui-theme-<hash>
//
// RegisterThemeOverride(theme, partial) returns a class name like
// "fui-theme-a1b2c3d4" plus a CSS block that redeclares only the
// overridden tokens. Wrap a region with that class via ui.Themed
// and every descendant's var() refs resolve through the override.
// See core-ui/ARCHITECTURE.md for the cascade rationale.
//
// # Utility classes
//
// style.Use("card") returns the attribute map for a utility class
// generated from theme tokens. Utility classes follow a Tailwind-
// like convention and are extracted to CSS at build time by
// CollectUsed/RenderCollected.
//
// # Token reference
//
// The Theme struct is a flat list of typed-token sub-structs. Field
// names auto-derive to kebab-case CSS variables via AutoFillNames.
//
//	Colors:      Primary, PrimaryFg, Secondary, SecondaryFg,
//	             Background, Surface, SurfaceSoft,
//	             Text, TextMuted, TextSubtle,
//	             Border, BorderStrong,
//	             Danger, Success, Warning, Info, Accent
//	Spacing:     XS, SM, MD, LG, XL, XXL, XXXL  (pixels)
//	Radii:       None, SM, MD, LG, XL, Full     (pixels)
//	Fonts:       Body, Heading, Mono            (font-family stacks)
//	Breakpoints: SM, MD, LG, XL, XXL            (pixels)
//	Shadows:     None, SM, MD, LG, XL           (box-shadow values)
//	ZIndex:      Dropdown, Sticky, Modal, Popover, Toast
//	Durations:   Fast, Normal, Slow             (time.Duration)
//	Typography:  XS, SM, Base, LG, XL, XXL, XXXL  (font-size strings)
//	Layout:      TouchTarget                    (Spacing — WCAG 2.5.5)
//
// E.g. Colors.PrimaryFg → CSS variable --color-primary-fg →
// theme.App.Colors.PrimaryFg.CSS() → "var(--color-primary-fg)".
// In StyleSheet builders, {colors.primary-fg} resolves identically.
package style
