package headless

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Field parts.
const (
	PartFieldRow Part = "field-row"
)

// FieldProps is the label / control / hint / error group.
type FieldProps struct {
	// Label is required. An input without a label is not a variant, it
	// is a defect: nothing announces it and nothing clicks it into
	// focus.
	Label string
	// For is the control's id. Without it the label is decorative — it
	// neither names the control for assistive tech nor enlarges its hit
	// area — so a Field with a Label and no For is refused.
	For string
	// Hint is help text. Suppressed while Error is set, so the two
	// never stack and shift the row.
	Hint string
	// Error replaces the hint and is announced.
	Error string
	// Required marks the label and is mirrored onto the control by the
	// caller (the control owns its own required attribute).
	Required bool

	ID         string
	ExtraAttrs html.Attrs
}

// FieldControl is what a field tells its control about itself. The
// control is built from it rather than beside it, which is the whole
// point: the id scheme, the description wiring and the invalid state
// all originate in one place and reach the input by construction.
//
// Passing a pre-built control instead is how a hint ends up rendered,
// given an id, and never referenced — visible on screen and absent to
// a screen reader. That was true of every field in this system until
// Field started handing these down.
type FieldControl struct {
	// ID is the field's For: the id the label points at.
	ID string
	// DescribedBy is the hint's or the error's id, for the control's
	// aria-describedby. Empty when the field has neither.
	DescribedBy string
	// Invalid is true when the field has an Error, so a control never
	// has to be told twice.
	Invalid bool
	// Required mirrors the field's own flag.
	Required bool
}

// Field renders the group. build receives the wiring and returns the
// control.
func Field(p FieldProps, s Classes, build func(FieldControl) render.HTML) render.HTML {
	if p.Label == "" {
		panic("headless: Field requires Label")
	}
	if p.For == "" {
		panic("headless: Field requires For — a label that names no control is decoration")
	}
	describedBy, hintID, errID := Describe(p.For, p.Hint, p.Error)
	control := build(FieldControl{
		ID: p.For, DescribedBy: describedBy,
		Invalid: p.Error != "", Required: p.Required,
	})

	labelAttrs := Attrs(map[string]string{"for": p.For})
	if p.Required {
		labelAttrs["data-required"] = ""
	}
	kids := []render.HTML{
		El("label", s, PartLabel, labelAttrs, render.Text(p.Label)),
		control,
	}
	switch {
	case p.Error != "":
		// role=alert, not aria-live: an error that appears after a
		// failed submit has to interrupt, and the element is inserted
		// rather than updated in place.
		kids = append(kids, El("p", s, PartError,
			Attrs(map[string]string{"id": errID, "role": "alert"}),
			render.Text(p.Error)))
	case p.Hint != "":
		kids = append(kids, El("p", s, PartHint,
			Attrs(map[string]string{"id": hintID}), render.Text(p.Hint)))
	}

	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	return El("div", s, PartRoot, own, kids...)
}

// FieldDescribedBy is what a control inside a Field must carry so the
// hint or error actually reaches assistive tech. Controls call it; it
// exists so the id scheme lives in exactly one place.
func FieldDescribedBy(controlID, hint, errText string) string {
	d, _, _ := Describe(controlID, hint, errText)
	return d
}

// FieldRow lays fields side by side.
func FieldRow(s Classes, fields ...render.HTML) render.HTML {
	return El("div", s, PartFieldRow, nil, fields...)
}

// ─── ConditionalField ───────────────────────────────────────────────

// ConditionalFieldProps is a region shown when another field has a
// given value.
type ConditionalFieldProps struct {
	// When is the NAME of the watched field. Required: a region that
	// watches nothing is always shown, which is a div.
	//
	// The scope the runtime reads the name in: the region's own form
	// first — two forms can each carry a "plan" control and the region
	// follows the one it belongs to — and, when the region has no
	// form or its form holds no control of that name, the document,
	// preferring controls no form owns (a page-level switch) and
	// otherwise the first in document order.
	When string
	// Value is the watched field's value that shows the region.
	// Required: shown on every value is the same as always shown.
	// The empty string is refused, so "show when unchecked" — a
	// checkbox whose unchecked value is "" — is not expressible here;
	// watch a select or a radio pair whose values are both stated
	// instead.
	Value string

	ID         string
	ExtraAttrs html.Attrs
}

// ConditionalField renders the region — VISIBLE.
//
// It carries no hidden attribute, because hiding it here would make
// the dependent field reachable only after script had run: a page
// with script disabled, a reader mode, a crawler and a first paint
// before script arms would all see a field that never arrived. The
// module that binds data-hui-when hides and shows it as the watched
// field changes; the hiding is the platform's own hidden attribute,
// restated by a stylesheet at a specificity nothing here can beat.
//
// It is a div and adds no semantics: the fields inside arrive with
// their own labels, and a region name would be read before each one.
func ConditionalField(p ConditionalFieldProps, s Classes, children ...render.HTML) render.HTML {
	if p.When == "" {
		panic("headless: ConditionalField requires When — a region that watches nothing is always shown")
	}
	if p.Value == "" {
		panic("headless: ConditionalField requires Value — shown on every value is the same as always shown")
	}
	own := Merge(Safe(p.ExtraAttrs, "data-hui-when", "data-hui-when-value"),
		Attrs(map[string]string{
			"id": p.ID, "data-hui-when": p.When, "data-hui-when-value": p.Value,
		}))
	return El("div", s, PartRoot, own, children...)
}

func init() {
	Register(Spec{
		Name:    "Field",
		Anatomy: []Part{PartRoot, PartLabel, PartHint, PartError},
		// Nothing is fillable. Every part a field draws is half of a
		// relationship built from the same id as the control: a slot
		// here would let a page replace the hint with markup that has
		// no id, leaving aria-describedby pointing at nothing.
		Cases: func(k Kit) []Case {
			s := k.Classes
			input := func(c FieldControl) render.HTML {
				return Input(InputProps{Name: c.ID, ID: c.ID, DescribedBy: c.DescribedBy,
					Invalid: c.Invalid, Required: c.Required}, s)
			}
			return []Case{{
				Name: "hinted",
				Why:  "the hint is tied to the control by id, or it is on screen for sighted users only and the format rule never arrives",
				HTML: Field(FieldProps{Label: "Port", For: "field-port", Hint: "1–65535"}, s, input),
			}, {
				Name: "errored",
				Why:  "the error REPLACES the hint in the description rather than joining it: when something is wrong, reading both buries the correction",
				HTML: Field(FieldProps{Label: "Port", For: "field-port-bad", Hint: "1–65535", Error: "Already in use."}, s, input),
			}, {
				Name: "required",
				Why:  "required is a state the parser is told about, not a red asterisk — which is a picture of a rule",
				HTML: Field(FieldProps{Label: "App name", For: "app", Required: true}, s, input),
			}}
		},
	})

	Register(Spec{
		Name:    "FieldRow",
		Anatomy: []Part{PartFieldRow},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "pair",
				Why:  "two fields side by side are still two fields: the row adds layout and no semantics, so nothing here is announced",
				HTML: FieldRow(s,
					Field(FieldProps{Label: "From", For: "from"}, k.For("Field"), func(c FieldControl) render.HTML {
						return Input(InputProps{Name: "from", ID: c.ID}, k.For("Input"))
					}),
					Field(FieldProps{Label: "To", For: "to"}, k.For("Field"), func(c FieldControl) render.HTML {
						return Input(InputProps{Name: "to", ID: c.ID}, k.For("Input"))
					})),
			}}
		},
	})

	Register(Spec{
		Name:    "ConditionalField",
		Anatomy: []Part{PartRoot},
		Hooks:   []string{"data-hui-when", "data-hui-when-value"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "shown when the watched field matches",
				Why:  "rendered visible with no hidden attribute, because a field only script can reveal is a field a scriptless reader never reaches — the runtime hides it when the watched field does not match, and not before",
				// The watched field rides along so the reference the
				// hooks carry resolves inside the fixture, exactly as
				// it must on a page.
				HTML: render.Join(
					Choice(ChoiceProps{Type: "radio", Name: "notify", Value: "webhook",
						Label: "Webhook"}, k.For("Choice")),
					ConditionalField(ConditionalFieldProps{When: "notify", Value: "webhook"}, s,
						Field(FieldProps{Label: "Webhook URL", For: "cond-webhook-url"}, k.For("Field"),
							func(c FieldControl) render.HTML {
								return Input(InputProps{Name: "webhook-url", ID: c.ID}, k.For("Input"))
							})),
				),
			}}
		},
	})
}
