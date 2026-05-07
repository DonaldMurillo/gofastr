// Package style provides the theming, token resolution, utility class generation,
// and CSS extraction system for the GoFastr core-ui framework.
//
// The style package defines a Theme type that encapsulates a complete visual design
// system including colors, spacing, radii, fonts, breakpoints, and component styles.
// Token references like {colors.primary} can be resolved to concrete CSS values.
//
// Utility classes follow a Tailwind-like naming convention and are generated from
// theme tokens, producing minimal CSS for only the classes actually used in rendered HTML.
//
// # Quick Start
//
//	theme := style.DefaultTheme()
//	css := theme.CSSCustomProperties()
//	attrs := style.Use("card")
package style
