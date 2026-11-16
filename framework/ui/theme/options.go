package theme

import (
	"fmt"
	"sort"
)

// Density is how tall controls are and how far apart they sit.
type Density int

const (
	DensityUnset Density = iota
	// Comfortable is the default: 44px controls (the WCAG 2.5.5 tap target), md gaps.
	Comfortable
	// Compact is 36px controls and sm gaps.
	Compact
)

// String returns the flattened form, "" for unset.
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

// ParseDensity is String's inverse.
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

// ButtonTreatment is how a theme draws a button variant's colour.
type ButtonTreatment int

const (
	TreatmentUnset ButtonTreatment = iota
	// Filled is a solid primary background with primary-fg text and no border.
	Filled
	// Outline is no fill, with primary text and border.
	Outline
	// Soft is the soft surface tint with primary text and no border.
	Soft
)

// String returns the flattened form, "" for unset.
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
	// Round is the theme's md radius token.
	Round
	// Square is no rounding.
	Square
	// Pill is fully rounded.
	Pill
)

// String returns the flattened form, "" for unset.
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

// FieldLayout places a field's label above its control or beside it.
type FieldLayout int

const (
	LayoutUnset FieldLayout = iota
	// Stacked puts the label above its control.
	Stacked
	// Inline puts the label beside its control; a narrow context may still stack it.
	Inline
)

// String returns the flattened form, "" for unset.
func (l FieldLayout) String() string {
	switch l {
	case Stacked:
		return "stacked"
	case Inline:
		return "inline"
	default:
		return ""
	}
}

// ParseFieldLayout is String's inverse.
func ParseFieldLayout(s string) (FieldLayout, error) {
	switch s {
	case "stacked":
		return Stacked, nil
	case "inline":
		return Inline, nil
	default:
		return LayoutUnset, fmt.Errorf("unknown field layout %q (want stacked or inline)", s)
	}
}

// FieldRadius is the corner shape of a field's controls.
type FieldRadius int

const (
	FieldRadiusUnset FieldRadius = iota
	// FieldRound is the md radius token on a field's controls.
	FieldRound
	// FieldSquare is no rounding on a field's controls.
	FieldSquare
)

// String returns the flattened form, "" for unset.
func (r FieldRadius) String() string {
	switch r {
	case FieldRound:
		return "round"
	case FieldSquare:
		return "square"
	default:
		return ""
	}
}

// ParseFieldRadius is String's inverse.
func ParseFieldRadius(s string) (FieldRadius, error) {
	switch s {
	case "round":
		return FieldRound, nil
	case "square":
		return FieldSquare, nil
	default:
		return FieldRadiusUnset, fmt.Errorf("unknown field radius %q (want round or square)", s)
	}
}

// ButtonOptions is the button family's slice of the option set.
type ButtonOptions struct {
	Treatment ButtonTreatment
	Radius    ButtonRadius
}

// FieldOptions is the form field family's slice of the option set.
type FieldOptions struct {
	Layout FieldLayout
	Radius FieldRadius
}

// ComponentOptions is the typed form of style.Theme.Components; a zero field is unset.
type ComponentOptions struct {
	Density Density
	Button  ButtonOptions
	Field   FieldOptions
}

// DefaultOptions is the complete option set a theme carries when a host sets none.
var DefaultOptions = ComponentOptions{
	Density: Comfortable,
	Button:  ButtonOptions{Treatment: Filled, Radius: Round},
	Field:   FieldOptions{Layout: Stacked, Radius: FieldRound},
}

// Complete returns o with every unset option set to its default.
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
	if o.Field.Layout == LayoutUnset {
		o.Field.Layout = DefaultOptions.Field.Layout
	}
	if o.Field.Radius == FieldRadiusUnset {
		o.Field.Radius = DefaultOptions.Field.Radius
	}
	return o
}

// Flattened writes o as style.Theme.Components keys, omitting unset options.
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
	if o.Field.Layout != LayoutUnset {
		m["field.layout"] = o.Field.Layout.String()
	}
	if o.Field.Radius != FieldRadiusUnset {
		m["field.radius"] = o.Field.Radius.String()
	}
	return m
}

// OptionsFromFlattened is Flattened's inverse; it errors on an unknown key or value.
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
		case "field.layout":
			o.Field.Layout, err = ParseFieldLayout(v)
		case "field.radius":
			o.Field.Radius, err = ParseFieldRadius(v)
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

// Option is one catalogue entry: a flattened key, its members in declaration
// order, and its default.
type Option struct {
	Key     string   // the flattened style.Theme.Components key
	Members []string // the values OptionsFromFlattened accepts
	Default string   // the value DefaultOptions carries
}

// Options returns the catalogue of every option key, in a fixed order.
func Options() []Option {
	def := DefaultOptions.Flattened()
	return []Option{
		{Key: "density", Members: optionMembers(Density.String), Default: def["density"]},
		{Key: "button.treatment", Members: optionMembers(ButtonTreatment.String), Default: def["button.treatment"]},
		{Key: "button.radius", Members: optionMembers(ButtonRadius.String), Default: def["button.radius"]},
		{Key: "field.layout", Members: optionMembers(FieldLayout.String), Default: def["field.layout"]},
		{Key: "field.radius", Members: optionMembers(FieldRadius.String), Default: def["field.radius"]},
	}
}

func optionMembers[T ~int](stringOf func(T) string) []string {
	var out []string
	for v := T(1); v <= maxOptionMembers; v++ {
		name := stringOf(v)
		if name == "" {
			return out
		}
		out = append(out, name)
	}
	panic(fmt.Sprintf("theme: an option enum's String never returns \"\" past its last member (walked %d values)", maxOptionMembers))
}

const maxOptionMembers = 32
