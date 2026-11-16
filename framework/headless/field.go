package headless

import (
	"encoding/json"
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
	Label string
	// For is the control's id.
	For string
	// Hint stays rendered and described beside an Error, after it.
	Hint string
	// Error is announced and drawn before the hint.
	Error string
	// ReserveError renders an empty described error node, id For+"-error", for
	// a script to fill; that script also sets aria-invalid.
	ReserveError bool
	// Required marks the label; the caller mirrors it onto the control.
	Required bool

	Parts Parts

	ID         string
	ExtraAttrs html.Attrs
}

// FieldControl is the wiring a field hands the control it builds.
type FieldControl struct {
	ID string
	// DescribedBy is the error's and the hint's ids, error first.
	DescribedBy string
	Invalid     bool
	Required    bool
}

// Field renders the group around the control build returns; it panics
// without Label or For.
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
		b.El("label", PartLabel, Internal(labelAttrs), render.Text(p.Label)),
		control,
	}
	if p.Error != "" {
		kids = append(kids, b.El("p", PartError,
			Mark(Internal(Attrs(map[string]string{"id": errID, "role": "alert"})), "data-hui-field-error"),
			render.Text(p.Error)))
	} else if reserved {
		kids = append(kids, b.El("p", PartError,
			Mark(Internal(Attrs(map[string]string{"id": errID, "role": "alert"})), "data-hui-field-error")))
	}
	if p.Hint != "" {
		kids = append(kids, b.El("p", PartHint,
			Internal(Attrs(map[string]string{"id": hintID})), render.Text(p.Hint)))
	}

	own := Mark(Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID})), "data-hui-field")
	return b.El("div", PartRoot, own, kids...)
}

func nonEmpty(vals ...string) string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, " ")
}

// FieldRow lays fields side by side.
func FieldRow(s Classes, fields ...render.HTML) render.HTML {
	return El("div", s, PartFieldRow, nil, fields...)
}

// Fieldset parts.
const (
	PartLegend     Part = "legend"
	PartGroupDesc  Part = "group-desc"
	PartFields     Part = "fields"
	PartGroupError Part = "group-error"
)

// FieldsetProps is a titled group of fields.
type FieldsetProps struct {
	// Legend makes the group a fieldset; empty renders a plain div.
	Legend string
	// Description and Error describe the group, in that order.
	Description string
	Error       string

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// Fieldset renders the group; Description or Error with no ID or Legend panics.
func Fieldset(p FieldsetProps, s Classes, fields ...render.HTML) render.HTML {
	b := p.Parts.Box(s)
	head := make([]render.HTML, 0, 3)
	if p.Legend != "" {
		head = append(head, b.El("legend", PartLegend, Internal(nil), render.Text(p.Legend)))
	}
	if (p.Description != "" || p.Error != "") && p.ID == "" && p.Legend == "" {
		panic("headless: Fieldset Description and Error need ID or Legend — their ids are derived from one of them, and a group instruction nothing can point at is advice nobody hears")
	}
	base := p.ID
	if base == "" {
		base = slugID("fieldset", p.Legend)
	}
	own := Merge(Safe(p.ExtraAttrs, "aria-describedby"), Attrs(map[string]string{"id": p.ID}))
	var described []string
	if p.Description != "" {
		descID := base + "-desc"
		described = append(described, descID)
		head = append(head, b.El("p", PartGroupDesc,
			Internal(Attrs(map[string]string{"id": descID})), render.Text(p.Description)))
	}
	if p.Error != "" {
		errID := base + "-error"
		described = append(described, errID)
		head = append(head, b.El("p", PartGroupError,
			Internal(Attrs(map[string]string{"id": errID})), render.Text(p.Error)))
	}
	if len(described) > 0 {
		own["aria-describedby"] = strings.Join(described, " ")
	}
	fieldsOwn := html.Attrs(nil)
	if len(fields) == 0 {
		fieldsOwn = Internal(nil)
	}
	body := b.El("div", PartFields, fieldsOwn, fields...)

	kids := append(head, body)
	if p.Legend == "" {
		return b.El("div", PartRoot, own, kids...)
	}
	return b.El("fieldset", PartRoot, own, kids...)
}

func init() {
	Register(Spec{
		Name: "Fieldset",
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Fieldset(FieldsetProps{Legend: "Access", Parts: parts}, s)
		},
		Anatomy: []Part{PartRoot, PartLegend, PartGroupDesc, PartFields, PartGroupError},
		Cases: func(k Kit) []Case {
			s := k.Classes
			field := func(label, name string) render.HTML {
				return Field(FieldProps{Label: label, For: name},
					k.For("Field"), func(c FieldControl) render.HTML {
						return Input(InputProps{Name: name, ID: c.ID}, k.For("Input"))
					})
			}
			return []Case{{
				Name: "a titled group",
				Why:  "a real legend in a real fieldset names every control inside without a single aria attribute — the native group semantic a wrapper div has to fake with labels nobody checks",
				HTML: Fieldset(FieldsetProps{Legend: "Notifications",
					Description: "How this app tells you about deploys."},
					s, field("Deploy email", "deploy-email"), field("Failure email", "failure-email")),
			}, {
				Name: "a group error",
				Why:  "the error that belongs to the group as a whole is wired by aria-describedby on the group, so it is read with the question and never replaces a field's own error",
				HTML: Fieldset(FieldsetProps{Legend: "Access", Error: "Pick at least one role."},
					s, field("Admin", "role-admin")),
			}, {
				Name: "no title",
				Why:  "a group with no name renders as a plain div: an unlabelled fieldset is a landmark that names nothing, and a screen reader announcing an empty group label is the alternative",
				HTML: Fieldset(FieldsetProps{ID: "misc", Description: "The remaining settings."},
					s, field("Retries", "retries")),
			}}
		},
	})
}

// ConditionalFieldProps is a region shown when another field has a value.
type ConditionalFieldProps struct {
	// When is the name of the watched field.
	When string
	// Value or Values, exactly one and never empty, shows the region.
	Value  string
	Values []string

	ID         string
	ExtraAttrs html.Attrs
}

// ConditionalField renders the region visible; headless-when hides it.
func ConditionalField(p ConditionalFieldProps, s Classes, children ...render.HTML) render.HTML {
	if p.When == "" {
		panic("headless: ConditionalField requires When — a region that watches nothing is always shown")
	}
	own := map[string]string{"id": p.ID, "data-hui-when": p.When}
	switch {
	case p.Value != "" && len(p.Values) > 0:
		panic("headless: ConditionalField takes Value or Values, not both — two conditions cannot share one region")
	case p.Value != "":
		own["data-hui-when-value"] = p.Value
	case len(p.Values) > 0:
		for _, v := range p.Values {
			if v == "" {
				panic("headless: ConditionalField Values holds an empty value — it matches only the empty string, which the single-value form already refuses; watch a control whose values are all stated")
			}
		}
		raw, err := json.Marshal(p.Values)
		if err != nil {
			panic("headless: ConditionalField Values did not encode: " + err.Error())
		}
		own["data-hui-when-in"] = string(raw)
	default:
		panic("headless: ConditionalField requires Value or Values — shown on every value is the same as always shown")
	}
	return El("div", s, PartRoot,
		Merge(Safe(p.ExtraAttrs, "data-hui-when", "data-hui-when-value", "data-hui-when-in"),
			Attrs(own)), children...)
}

func init() {
	Register(Spec{
		Name:    "Field",
		Anatomy: []Part{PartRoot, PartLabel, PartHint, PartError},
		Hooks:   []string{"data-hui-field", "data-hui-field-error"},
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
		Hooks:   []string{"data-hui-when", "data-hui-when-value", "data-hui-when-in"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "shown when the watched field matches",
				Why:  "rendered visible with no hidden attribute, because a field only script can reveal is a field a scriptless reader never reaches — the runtime hides it when the watched field does not match, and not before",
				HTML: render.Join(
					Choice(ChoiceProps{Type: "radio", Name: "notify", Value: "webhook",
						Label: "Webhook"}, k.For("Choice")),
					ConditionalField(ConditionalFieldProps{When: "notify", Value: "webhook"}, s,
						Field(FieldProps{Label: "Webhook URL", For: "cond-webhook-url"}, k.For("Field"),
							func(c FieldControl) render.HTML {
								return Input(InputProps{Name: "webhook-url", ID: c.ID}, k.For("Input"))
							})),
				),
			}, {
				Name: "shown when the watched field matches any listed value",
				Why:  "the list is one JSON attribute, so a member value holding a comma, a quote or a bracket stays one member — the module's JSON.parse and the browser's attribute parser each see exactly what Go encoded",
				HTML: render.Join(
					Select(SelectProps{Name: "notify", ID: "cond-notify-in", Options: []Option{
						{Value: "email", Label: "Email"},
						{Value: `webhook, "both"]`, Label: "Both"},
						{Value: "none", Label: "None"},
					}}, k.For("Select")),
					ConditionalField(ConditionalFieldProps{When: "notify",
						Values: []string{"email", `webhook, "both"]`}}, s,
						Field(FieldProps{Label: "Address", For: "cond-notify-addr"}, k.For("Field"),
							func(c FieldControl) render.HTML {
								return Input(InputProps{Name: "notify-addr", ID: c.ID}, k.For("Input"))
							})),
				),
			}}
		},
	})
}
