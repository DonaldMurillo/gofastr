package headless

import (
	"strings"

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
	// Hint is help text: the rule the value must obey. It stays
	// rendered — and stays in the description — when Error is set: the
	// hint is the rule, the error is the violation, and dropping the
	// rule exactly when it was broken is dropping it when the reader
	// needs it most. The error is drawn first and read first, so the
	// correction arrives before the reminder.
	//
	// The cost is paid in pixels and syllables: an errored row is
	// taller, and its description is longer, than an error-only one.
	// That is the deliberate trade of this primitive; a caller who
	// wants the hint gone on error says so by not setting it.
	Hint string
	// Error is announced and drawn before the hint.
	Error string
	// ReserveError keeps an error paragraph rendered — empty and wired
	// into the control's aria-describedby — when Error itself is
	// empty. It exists for a script that fills the node without
	// re-rendering the field (a live editor applying edits as the
	// operator types). An empty described-by target announces nothing
	// until it is filled, which is the point: the wiring ships, the
	// words arrive when they are true.
	//
	// The node's id is the contract a filling script looks it up by:
	// the control's id with "-error" appended, the same id that rides
	// aria-describedby. It carries no hook of its own — a data-hui-*
	// attribute is something this package's runtime module binds, and
	// no module has behaviour for an empty paragraph.
	//
	// A caller that fills the node must also set aria-invalid on the
	// control it describes; this component cannot know the script's
	// verdict. The server-rendered path should pass Error instead —
	// a reserved node is a scaffold, not an answer.
	ReserveError bool
	// Required marks the label and is mirrored onto the control by the
	// caller (the control owns its own required attribute).
	Required bool

	// Parts is the caller's reach into the field's named parts:
	// attributes on the root (a class appends, never replaces). No
	// part is fillable — every element a field draws is half of a
	// relationship built from the control's id, and replacement
	// content would break the half it replaced.
	Parts Parts

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
	// DescribedBy is the error's and the hint's ids, error first, for
	// the control's aria-describedby. Empty when the field has
	// neither — unless ReserveError held an empty error node in place,
	// whose id rides here too.
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
	reserved := p.ReserveError && p.Error == ""
	describedBy, hintID, errID := Describe(p.For, p.Hint, p.Error)
	if reserved {
		if errID == "" {
			errID = p.For + "-error"
		}
		// The node's id is part of the control's description even
		// while the node is empty: the wiring is what the reserving
		// caller ships, the words are what their script writes.
		describedBy = nonEmpty(errID, describedBy)
	}
	control := build(FieldControl{
		ID: p.For, DescribedBy: describedBy,
		Invalid: p.Error != "", Required: p.Required,
	})

	b := p.Parts.Box(s)
	labelAttrs := Attrs(map[string]string{"for": p.For})
	if p.Required {
		labelAttrs["data-required"] = ""
	}
	kids := []render.HTML{
		b.El("label", PartLabel, labelAttrs, render.Text(p.Label)),
		control,
	}
	if p.Error != "" {
		// role=alert, not aria-live: an error that appears after a
		// failed submit has to interrupt, and the element is inserted
		// rather than updated in place.
		kids = append(kids, b.El("p", PartError,
			Attrs(map[string]string{"id": errID, "role": "alert"}),
			render.Text(p.Error)))
	} else if reserved {
		// The same paragraph, empty, found by its id. An empty alert
		// node is silent, which is why shipping it reserved is safe,
		// and the description picks the words up on focus once they
		// are there. Whether filling it also INTERRUPTS depends on the
		// engine: the insertion is what flips :empty off, so an engine
		// that recomputes style before processing the mutation
		// announces and one that had dropped the hidden node may not.
		// Do not rely on the interrupt; the visible text and the
		// description are the contract. The stylesheet takes it out of the grid
		// while it is empty, so a reserved field is not a field with a
		// blank row under it.
		kids = append(kids, b.El("p", PartError,
			Attrs(map[string]string{"id": errID, "role": "alert"})))
	}
	if p.Hint != "" {
		kids = append(kids, b.El("p", PartHint,
			Attrs(map[string]string{"id": hintID}), render.Text(p.Hint)))
	}

	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	return b.El("div", PartRoot, own, kids...)
}

// nonEmpty joins the values that say something, in order, with a
// space — the described-by shape when the ids arrive from more than
// one branch.
func nonEmpty(vals ...string) string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, " ")
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
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Field(FieldProps{Label: "Port", For: "field-parts", Hint: "1–65535", Parts: parts}, s,
				func(c FieldControl) render.HTML {
					return Input(InputProps{Name: c.ID, ID: c.ID, DescribedBy: c.DescribedBy}, s)
				})
		},
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
				Why:  "the error JOINS the hint in the description, ahead of it: the hint is the rule the value must obey and the error is the violation, so dropping the rule exactly when it was broken is dropping it when it is needed most — and the correction is read first because it comes first",
				HTML: Field(FieldProps{Label: "Port", For: "field-port-bad", Hint: "1–65535", Error: "Already in use."}, s, input),
			}, {
				Name: "reserved error node",
				Why:  "a script that fills the error without re-rendering needs the node and its id already in the description — an empty described-by target announces nothing until it is filled, which is the point, and the stylesheet keeps the empty node out of the layout",
				HTML: Field(FieldProps{Label: "Token", For: "field-token-live", ReserveError: true}, s, input),
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
