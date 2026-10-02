package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// The component-options compiler: the one function that turns a
// theme's flattened Components map into the custom properties
// component stylesheets consume. core-ui/style stores, hashes and
// copies the options but cannot draw them; this package owns the
// vocabulary, so it registers the compiler here, from init, beside the
// style registrations in styles_components.go — before any host
// composes app.css, the catalog or the manifest (all three freeze at
// first use; style.RegisterComponentOptionsCompiler panics on a late
// registration for exactly that reason).
//
// # The cascade rule
//
// Theme boundaries DECLARE the --fui-* option variables; component
// rules CONSUME them (border-radius: var(--fui-button-radius)) and
// never redeclare them. No descendant option rules: .fui-theme-a
// .fui-button would outrank the component's own variant and state
// selectors, so options travel by inheritance instead. Because every
// theme built by theme.Default declares the complete set, an inner
// scope redeclares all of it and wins by proximity; nesting needs no
// exception and no specificity ladder.
//
// The variables are re-emitted at every boundary (root and scope,
// light and dark — see style.ThemeOverrideCSS) because a custom
// property's var() references compute where the declaration sits:
// --fui-button-primary-bg: var(--color-primary) declared only at :root
// would carry the root's resolved primary into a scope with its own
// palette.
//
// # The prefix
//
// `fui-` is reserved for this package's class names and option
// variables. A caller who writes fui-button on their own markup gets
// the framework's styling whenever this sheet is on the page; the docs
// say so once. Classes belong to framework/ui; the data-hui-* hooks
// belong to framework/headless. The prefixes match, the ownership
// does not.
// The button stylesheet consumes these variables (styles_components.go:
// buttonCSS); the remaining component families' sheets arrive with
// their own changes.
func init() {
	// The complete default set rides the registration: it is the :root
	// floor every optionless theme (DefaultTheme, the theme-init
	// scaffold, a host with no App.Theme) emits, so the component rules
	// consuming the --fui-* variables resolve on every host that links
	// this package.
	style.RegisterComponentOptionsCompiler(componentOptionsCSS, theme.DefaultOptions.Flattened())
}

// componentOptionsCSS compiles one theme's flattened options into the
// option-variable declarations. Only options the map actually carries
// are emitted: a key left out inherits from the enclosing boundary,
// which is the nesting contract, and theme.Default guarantees the
// complete set at every boundary it builds. An unknown key or an
// unknown value panics — the map is theme data that crossed a
// boundary, and dropping it silently would hide the typo forever.
func componentOptionsCSS(components map[string]string) []style.Declaration {
	opts, err := theme.OptionsFromFlattened(components)
	if err != nil {
		panic(fmt.Sprintf("framework/ui: %v (the map a theme registered as Components does not match the option vocabulary; build it through theme.Default or theme.ComponentOptions.Flattened)", err))
	}
	var decls []style.Declaration
	switch opts.Density {
	case theme.Comfortable:
		// The comfortable height rides the --spacing-touch-target
		// token (Layout.TouchTarget, 44px by default — the WCAG 2.5.5
		// floor), so a host that raises the token for an accessibility
		// skin keeps its taller controls. Compact is a deliberate
		// squeeze BELOW the floor and stays a literal.
		decls = append(decls,
			style.Declaration{Name: "--fui-density-control-h", Value: "var(--spacing-touch-target)"},
			style.Declaration{Name: "--fui-density-gap", Value: "var(--spacing-md)"},
		)
	case theme.Compact:
		decls = append(decls,
			style.Declaration{Name: "--fui-density-control-h", Value: "36px"},
			style.Declaration{Name: "--fui-density-gap", Value: "var(--spacing-sm)"},
		)
	case theme.DensityUnset:
		// Inherit: nothing to declare.
	}
	switch opts.Button.Radius {
	case theme.Round:
		decls = append(decls, style.Declaration{Name: "--fui-button-radius", Value: "var(--radii-md)"})
	case theme.Square:
		decls = append(decls, style.Declaration{Name: "--fui-button-radius", Value: "0"})
	case theme.Pill:
		decls = append(decls, style.Declaration{Name: "--fui-button-radius", Value: "9999px"})
	case theme.RadiusUnset:
	}
	// Field: the columns variable is the layout (stacked is one full
	// track; inline is a label track beside a control track whose
	// minimum is zero so a long value can never force overflow), the
	// message column keeps the hint and the error under the control
	// in the inline layout, and the radius is what the field's
	// inputs, selects and summaries draw.
	switch opts.Field.Layout {
	case theme.Stacked:
		decls = append(decls,
			style.Declaration{Name: "--fui-field-columns", Value: "minmax(0, 1fr)"},
			style.Declaration{Name: "--fui-field-message-column", Value: "1 / -1"},
		)
	case theme.Inline:
		decls = append(decls,
			style.Declaration{Name: "--fui-field-columns", Value: "minmax(8rem, 1fr) minmax(0, 3fr)"},
			style.Declaration{Name: "--fui-field-message-column", Value: "2"},
		)
	case theme.LayoutUnset:
	}
	switch opts.Field.Radius {
	case theme.FieldRound:
		decls = append(decls, style.Declaration{Name: "--fui-field-radius", Value: "var(--radii-md)"})
	case theme.FieldSquare:
		decls = append(decls, style.Declaration{Name: "--fui-field-radius", Value: "0"})
	case theme.FieldRadiusUnset:
	}
	// Treatment draws background, foreground and border TOGETHER, per
	// variant: one option, three variables per variant, no descendant
	// rule that could outrank a variant's own selector. Each treated
	// variant reads its OWN colour pair: primary primary/primary-fg,
	// danger danger/danger-fg. The token system's ≥4.5:1 contract (and
	// Theme.Validate's guard) covers a colour and its -fg companion
	// only — danger once borrowed --color-primary-fg, and any host
	// whose primary is light with dark ink (amber, yellow, pastel)
	// inherited an unreadable filled danger button from the pairing.
	// Secondary and ghost read no treatment: they are drawn, not
	// treated.
	variantTrio := func(prefix, colour, fg string) {
		switch opts.Button.Treatment {
		case theme.Filled:
			decls = append(decls,
				style.Declaration{Name: prefix + "-bg", Value: colour},
				style.Declaration{Name: prefix + "-fg", Value: fg},
				style.Declaration{Name: prefix + "-border", Value: "transparent"},
			)
		case theme.Outline:
			decls = append(decls,
				style.Declaration{Name: prefix + "-bg", Value: "transparent"},
				style.Declaration{Name: prefix + "-fg", Value: colour},
				style.Declaration{Name: prefix + "-border", Value: colour},
			)
		case theme.Soft:
			decls = append(decls,
				style.Declaration{Name: prefix + "-bg", Value: "color-mix(in srgb, " + colour + " 15%, transparent)"},
				style.Declaration{Name: prefix + "-fg", Value: colour},
				style.Declaration{Name: prefix + "-border", Value: "transparent"},
			)
		case theme.TreatmentUnset:
		}
	}
	variantTrio("--fui-button-primary", "var(--color-primary)", "var(--color-primary-fg)")
	variantTrio("--fui-button-danger", "var(--color-danger)", "var(--color-danger-fg)")
	return decls
}
