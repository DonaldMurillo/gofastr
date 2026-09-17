package theme

import (
	"fmt"
	"sort"
)

// ComponentOptions: the typed form of a theme's component options, the
// layer above the flattened style.Theme.Components map. Zero means
// "unspecified" while overrides merge; Default() flattens the merged
// result, which is complete — every option carries a value — so every
// theme the framework ships declares a full option set at every theme
// boundary. That completeness is what makes option variables nest: an
// inner scope redeclares the whole set, so it wins by proximity and
// nothing leaks in from an outer scope.
//
// Two axes, explained once: Variant (ButtonPrimary, ButtonDanger,
// ghost) is what a button MEANS; Treatment (filled, outline, soft) is
// how a theme DRAWS that meaning. A danger outline and a primary
// outline share their drawing; the variant supplies the semantic
// colour. Density is the same kind of policy: it retunes control
// heights and gaps theme-wide, and an explicit Size on one component
// always wins over it.

// Density is the vertical rhythm of controls: how tall a control aims
// to be and which spacing step separates them.
type Density int

const (
	DensityUnset Density = iota
	// Comfortable is the default: 44px controls (the WCAG 2.5.5 tap
	// target) and the md spacing step between them.
	Comfortable
	// Compact is the dense setting: 36px controls, sm gaps.
	Compact
)

// String returns the flattened form ("comfortable", "compact"), the
// exact spelling style.Theme.Components carries.
func (d Density) String() string {
	switch d {
	case Comfortable:
		return "comfortable"
	case Compact:
		return "compact"
	default:
		return ""
	}
}

// ParseDensity is String's inverse, for reading a flattened
// Components map back into the typed form.
func ParseDensity(s string) (Density, error) {
	switch s {
	case "comfortable":
		return Comfortable, nil
	case "compact":
		return Compact, nil
	default:
		return DensityUnset, fmt.Errorf("unknown density %q (want comfortable or compact)", s)
	}
}

// ButtonTreatment is how a theme draws a button: where the ink goes.
type ButtonTreatment int

const (
	TreatmentUnset ButtonTreatment = iota
	// Filled: solid primary background, primary-fg text, no border.
	Filled
	// Outline: no fill, primary text and border.
	Outline
	// Soft: the soft surface tint, primary text, no border.
	Soft
)

// String returns the flattened form ("filled", "outline", "soft").
func (t ButtonTreatment) String() string {
	switch t {
	case Filled:
		return "filled"
	case Outline:
		return "outline"
	case Soft:
		return "soft"
	default:
		return ""
	}
}

// ParseButtonTreatment is String's inverse.
func ParseButtonTreatment(s string) (ButtonTreatment, error) {
	switch s {
	case "filled":
		return Filled, nil
	case "outline":
		return Outline, nil
	case "soft":
		return Soft, nil
	default:
		return TreatmentUnset, fmt.Errorf("unknown button treatment %q (want filled, outline or soft)", s)
	}
}

// ButtonRadius is a button's corner shape.
type ButtonRadius int

const (
	RadiusUnset ButtonRadius = iota
	// Round: the theme's md radius token.
	Round
	// Square: no rounding at all.
	Square
	// Pill: fully rounded.
	Pill
)

// String returns the flattened form ("round", "square", "pill").
func (r ButtonRadius) String() string {
	switch r {
	case Round:
		return "round"
	case Square:
		return "square"
	case Pill:
		return "pill"
	default:
		return ""
	}
}

// ParseButtonRadius is String's inverse.
func ParseButtonRadius(s string) (ButtonRadius, error) {
	switch s {
	case "round":
		return Round, nil
	case "square":
		return Square, nil
	case "pill":
		return Pill, nil
	default:
		return RadiusUnset, fmt.Errorf("unknown button radius %q (want round, square or pill)", s)
	}
}

// ButtonOptions is the button family's slice of the option set.
type ButtonOptions struct {
	Treatment ButtonTreatment
	Radius    ButtonRadius
}

// ComponentOptions is the typed component-option set a host passes in
// Overrides. Every field's zero value means "unspecified" during
// merging; an explicit Comfortable, Filled or Round RESETS an earlier
// override rather than being ignored as a no-op.
type ComponentOptions struct {
	Density Density
	Button  ButtonOptions
	// Field, Card and the rest arrive with their family's change, not
	// before.
}

// DefaultOptions is the complete option set: what a theme carries when
// a host says nothing.
var DefaultOptions = ComponentOptions{
	Density: Comfortable,
	Button:  ButtonOptions{Treatment: Filled, Radius: Round},
}

// Complete returns o with every unset option replaced by its default.
// Default() results are complete by construction; Complete is the
// explicit form for themes assembled by hand.
func (o ComponentOptions) Complete() ComponentOptions {
	if o.Density == DensityUnset {
		o.Density = DefaultOptions.Density
	}
	if o.Button.Treatment == TreatmentUnset {
		o.Button.Treatment = DefaultOptions.Button.Treatment
	}
	if o.Button.Radius == RadiusUnset {
		o.Button.Radius = DefaultOptions.Button.Radius
	}
	return o
}

// Flattened writes o into the style.Theme.Components keys. Unset
// options are omitted: flattening happens after merging, and an
// omitted key inherits, which is the nesting contract.
func (o ComponentOptions) Flattened() map[string]string {
	m := map[string]string{}
	if o.Density != DensityUnset {
		m["density"] = o.Density.String()
	}
	if o.Button.Treatment != TreatmentUnset {
		m["button.treatment"] = o.Button.Treatment.String()
	}
	if o.Button.Radius != RadiusUnset {
		m["button.radius"] = o.Button.Radius.String()
	}
	return m
}

// OptionsFromFlattened parses a style.Theme.Components map back into
// the typed form, the inverse of Flattened. Every key must be known
// and every value a member of its enum: the flattened form is data
// that crossed a boundary (a theme file, an edited theme, an embed),
// and a typo dropped silently is a typo nobody ever hears about again.
func OptionsFromFlattened(m map[string]string) (ComponentOptions, error) {
	var o ComponentOptions
	for _, k := range sortedOptionKeys(m) {
		v := m[k]
		var err error
		switch k {
		case "density":
			o.Density, err = ParseDensity(v)
		case "button.treatment":
			o.Button.Treatment, err = ParseButtonTreatment(v)
		case "button.radius":
			o.Button.Radius, err = ParseButtonRadius(v)
		default:
			err = fmt.Errorf("unknown component option %q", k)
		}
		if err != nil {
			return ComponentOptions{}, fmt.Errorf("theme: Components[%q]: %w", k, err)
		}
	}
	return o, nil
}

func sortedOptionKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
