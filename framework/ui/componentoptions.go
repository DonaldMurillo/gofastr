package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

const (
	compactControlHeight = "36px"
	compactGap           = "var(--spacing-sm)"
)

const buttonEdge = "var(--ui-button-edge, transparent)"

func init() {
	style.RegisterComponentOptionsCompiler(componentOptionsCSS, theme.DefaultOptions.Flattened())
}

func componentOptionsCSS(components map[string]string) []style.Declaration {
	opts, err := theme.OptionsFromFlattened(components)
	if err != nil {
		panic(fmt.Sprintf("framework/ui: %v (the map a theme registered as Components does not match the option vocabulary; build it through theme.Default or theme.ComponentOptions.Flattened)", err))
	}
	var decls []style.Declaration
	switch opts.Density {
	case theme.Comfortable:
		decls = append(decls,
			style.Declaration{Name: "--fui-density-control-h", Value: "var(--spacing-touch-target)"},
			style.Declaration{Name: "--fui-density-gap", Value: "var(--spacing-md)"},
		)
	case theme.Compact:
		decls = append(decls,
			style.Declaration{Name: "--fui-density-control-h", Value: compactControlHeight},
			style.Declaration{Name: "--fui-density-gap", Value: compactGap},
		)
	case theme.DensityUnset:
	}
	switch opts.Button.Radius {
	case theme.Round:
		decls = append(decls, style.Declaration{Name: "--fui-button-radius", Value: "var(--radii-md)"})
	case theme.Square:
		decls = append(decls, style.Declaration{Name: "--fui-button-radius", Value: "0"})
	case theme.Pill:
		decls = append(decls, style.Declaration{Name: "--fui-button-radius", Value: "var(--radii-full)"})
	case theme.RadiusUnset:
	}
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
	variantTrio := func(prefix, colour, fg string) {
		switch opts.Button.Treatment {
		case theme.Filled:
			decls = append(decls,
				style.Declaration{Name: prefix + "-bg", Value: colour},
				style.Declaration{Name: prefix + "-fg", Value: fg},
				style.Declaration{Name: prefix + "-border", Value: buttonEdge},
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
				style.Declaration{Name: prefix + "-border", Value: buttonEdge},
			)
		case theme.TreatmentUnset:
		}
	}
	variantTrio("--fui-button-primary", "var(--color-primary)", "var(--color-primary-fg)")
	variantTrio("--fui-button-danger", "var(--color-danger)", "var(--color-danger-fg)")
	return decls
}
