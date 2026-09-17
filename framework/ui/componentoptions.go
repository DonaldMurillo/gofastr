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
// --fui-button-bg: var(--color-primary) declared only at :root would
// carry the root's resolved primary into a scope with its own palette.
//
// # The prefix
//
// `fui-` is reserved for this package's class names and option
// variables. A caller who writes fui-button on their own markup gets
// the framework's styling whenever this sheet is on the page; the docs
// say so once. Classes belong to framework/ui; the data-hui-* hooks
// belong to framework/headless. The prefixes match, the ownership
// does not.
//
// Nothing consumes these variables yet; the component stylesheets
// that read them arrive with their components' own changes.
func init() {
	style.RegisterComponentOptionsCompiler(componentOptionsCSS)
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
		// 44px is the WCAG 2.5.5 minimum tap target; the md step is
		// the comfortable gap.
		decls = append(decls,
			style.Declaration{Name: "--fui-density-control-h", Value: "44px"},
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
	// Treatment draws background, foreground and border TOGETHER: one
	// option, three variables, no descendant rule that could outrank a
	// variant.
	switch opts.Button.Treatment {
	case theme.Filled:
		decls = append(decls,
			style.Declaration{Name: "--fui-button-bg", Value: "var(--color-primary)"},
			style.Declaration{Name: "--fui-button-fg", Value: "var(--color-primary-fg)"},
			style.Declaration{Name: "--fui-button-border", Value: "transparent"},
		)
	case theme.Outline:
		decls = append(decls,
			style.Declaration{Name: "--fui-button-bg", Value: "transparent"},
			style.Declaration{Name: "--fui-button-fg", Value: "var(--color-primary)"},
			style.Declaration{Name: "--fui-button-border", Value: "var(--color-primary)"},
		)
	case theme.Soft:
		decls = append(decls,
			style.Declaration{Name: "--fui-button-bg", Value: "var(--color-surface-soft)"},
			style.Declaration{Name: "--fui-button-fg", Value: "var(--color-primary)"},
			style.Declaration{Name: "--fui-button-border", Value: "transparent"},
		)
	case theme.TreatmentUnset:
	}
	return decls
}
