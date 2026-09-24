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

// FieldLayout is how a field's label sits against its control:
// stacked above it, or inline beside it. Inline is a preference the
// stylesheet may override in a narrow context, not a promise.
type FieldLayout int

const (
	LayoutUnset FieldLayout = iota
	Stacked
	Inline
)

// String returns the flattened form ("stacked", "inline").
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

// FieldRadius is a field control's corner shape: the radius the
// field's inputs, selects and summaries draw.
type FieldRadius int

const (
	FieldRadiusUnset FieldRadius = iota
	FieldRound
	FieldSquare
)

// String returns the flattened form ("round", "square").
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
type ComponentOptions struct {
	Density Density
	Button  ButtonOptions
	Field   FieldOptions
}

// DefaultOptions is the complete option set: what a theme carries when
// a host says nothing.
var DefaultOptions = ComponentOptions{
	Density: Comfortable,
	Button:  ButtonOptions{Treatment: Filled, Radius: Round},
	Field:   FieldOptions{Layout: Stacked, Radius: FieldRound},
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
	if o.Field.Layout == LayoutUnset {
		o.Field.Layout = DefaultOptions.Field.Layout
	}
	if o.Field.Radius == FieldRadiusUnset {
		o.Field.Radius = DefaultOptions.Field.Radius
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
	if o.Field.Layout != LayoutUnset {
		m["field.layout"] = o.Field.Layout.String()
	}
	if o.Field.Radius != FieldRadiusUnset {
		m["field.radius"] = o.Field.Radius.String()
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

// Option is one entry of the option catalogue: a flattened Components
// key, the member values its enum accepts in declaration order, and the
// default DefaultOptions carries. Tools that author themes list these
// members, so a control can never offer a value OptionsFromFlattened
// refuses.
type Option struct {
	// Key is the flattened Components key ("button.treatment"), the
	// exact string Flattened writes and OptionsFromFlattened switches
	// on; ThemeToTokens emits it under "component.".
	Key string
	// Members are the values the option's enum accepts, in declaration
	// order, derived from the enums' String methods.
	Members []string
	// Default is the value DefaultOptions carries for the key.
	Default string
}

// Options returns the option catalogue: every flattened option key in a
// fixed order — the declaration order the option set documents, density
// first, then the button family, then the field family — each with its
// members in declaration order and its default. The theme editor renders
// each entry as a select whose options are the members, so an authored
// value is a member by construction; the catalogue test pins the
// catalogue to the flattened vocabulary so a new option cannot be added
// without the catalogue following.
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

// optionMembers derives one enum's member strings by walking its values
// from one upward through String, the same method Flattened writes
// through, until String returns "". Every option enum is iota-based with
// its Unset sentinel at zero, its members contiguous from one, and ""
// past the last, so a member added to an enum joins the catalogue with
// no second list and no bound to move.
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

// maxOptionMembers bounds the member walk: a String whose default arm
// returns a word instead of "" would otherwise walk forever.
const maxOptionMembers = 32
