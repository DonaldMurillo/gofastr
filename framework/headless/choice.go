package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ChoiceProps configures one checkbox or radio.
type ChoiceProps struct {
	Type string
	Name string
	// Value is required for a radio; an empty checkbox value submits "on".
	Value    string
	Label    string
	Hint     string
	Checked  bool
	Disabled bool

	ID string
	// Extra attrs land on the input.
	Extra html.Attrs
}

// Choice renders label > input + span(text).
func Choice(p ChoiceProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Choice requires Label — an unlabelled choice is a bug, not a variant")
	}
	if p.Type != "checkbox" && p.Type != "radio" {
		panic("headless: Choice Type must be checkbox or radio, not " + strconv.Quote(p.Type) + " — any other type is a text input that breaks the group")
	}
	if p.Name == "" {
		panic("headless: Choice requires Name — a control with no name submits nothing")
	}
	if p.Type == "radio" && p.Value == "" {
		panic("headless: a radio Choice requires Value — without distinct values a group cannot tell its options apart")
	}
	input := Merge(Safe(p.Extra), html.Attrs{
		"type": p.Type,
		"name": p.Name,
	})
	attrsSet(input, "value", p.Value)
	attrsSet(input, "id", p.ID)
	Flag(input, "checked", p.Checked)
	Flag(input, "disabled", p.Disabled)

	text := []render.HTML{render.Text(p.Label)}
	if p.Hint != "" {
		text = append(text, El("span", s, PartHint, nil, render.Text(p.Hint)))
	}

	return El("label", s, PartRoot, Mark(html.Attrs{}, "data-hui-choice"),
		El("input", s, PartControl, Internal(input)),
		El("span", s, PartText, Internal(nil), text...),
	)
}

// SwitchProps configures a toggle.
type SwitchProps struct {
	Name string
	// Value is what an on switch submits; empty submits "on".
	Value    string
	Label    string
	Checked  bool
	Disabled bool

	ID string
	// Extra attrs land on the input.
	Extra html.Attrs
}

// Switch renders a checkbox with role=switch, wrapped by its label.
func Switch(p SwitchProps, s Classes) render.HTML {
	if p.Label == "" {
		panic("headless: Switch requires Label — an on/off switch about nothing is a bug, not a variant")
	}
	if p.Name == "" {
		panic("headless: Switch requires Name — a control with no name submits nothing")
	}
	input := Merge(Safe(p.Extra), html.Attrs{
		"type": "checkbox",
		"role": "switch",
		"name": p.Name,
	})
	attrsSet(input, "value", p.Value)
	attrsSet(input, "id", p.ID)
	Flag(input, "checked", p.Checked)
	Flag(input, "disabled", p.Disabled)
	return El("label", s, PartRoot, Mark(html.Attrs{}, "data-hui-choice"),
		El("input", s, PartControl, Internal(input)),
		El("span", s, PartText, Internal(nil), render.Text(p.Label)),
	)
}

// GroupProps configures a fieldset of rendered choices under one legend.
type GroupProps struct {
	Legend string
	// Required marks the legend; the caller marks the leaves required.
	Required bool

	ID    string
	Extra html.Attrs
}

// Group renders the items in a <fieldset> under a <legend>.
func Group(p GroupProps, s Classes, items ...render.HTML) render.HTML {
	if p.Legend == "" {
		panic("headless: Group requires Legend — a set of choices with no question above them is as broken as an unlabelled input")
	}
	attrs := Safe(p.Extra)
	attrsSet(attrs, "id", p.ID)
	legendAttrs := Attrs(nil)
	if p.Required {
		legendAttrs["data-required"] = ""
	}
	kids := append([]render.HTML{
		El("legend", s, PartLabel, Internal(legendAttrs), render.Text(p.Legend)),
	}, items...)
	return El("fieldset", s, PartRoot, attrs, kids...)
}

func init() {
	Register(Spec{
		Name:    "Choice",
		Anatomy: []Part{PartRoot, PartControl, PartText, PartHint},
		Hooks:   []string{"data-hui-choice"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "checkbox with a hint",
				Why:  "the label WRAPS the control, so the hit area is the whole row and there is no for/id pair left to go stale",
				HTML: Choice(ChoiceProps{Type: "checkbox", Name: "prune", Value: "1",
					Label: "Prune old images", Hint: "Keeps the last three builds."}, s),
			}, {
				Name: "radio",
				Why:  "a real radio, so one arrow key moves through the set and the browser enforces that exactly one is chosen",
				HTML: Choice(ChoiceProps{Type: "radio", Name: "policy", Value: "always", Label: "Always"}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Switch",
		Anatomy: []Part{PartRoot, PartControl, PartText},
		Hooks:   []string{"data-hui-choice"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "on",
				Why:  "a checkbox that says role=switch: it submits like a checkbox and is announced as on or off rather than checked or unchecked",
				HTML: Switch(SwitchProps{Name: "auto", Label: "Restart automatically", Checked: true}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Group",
		Anatomy: []Part{PartRoot, PartLabel},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "radio set",
				Why:  "the legend is the question the choices answer — without it a screen reader reads three labels and never says what is being decided",
				HTML: Group(GroupProps{Legend: "Restart policy"}, s,
					Choice(ChoiceProps{Type: "radio", Name: "policy", Value: "always", Label: "Always"}, k.For("Choice")),
					Choice(ChoiceProps{Type: "radio", Name: "policy", Value: "failure", Label: "On failure"}, k.For("Choice"))),
			}, {
				Name: "required set",
				Why:  "the cue and the rule are two different things: data-required on the legend is what a sighted reader looks for, and the required attribute on a leaf is what the browser enforces — a group carrying only one of them is a group that either lies or looks optional",
				HTML: Group(GroupProps{Legend: "Plan", Required: true}, s,
					Choice(ChoiceProps{Type: "radio", Name: "plan", Value: "free", Label: "Free",
						Extra: html.Attrs{"required": ""}}, k.For("Choice")),
					Choice(ChoiceProps{Type: "radio", Name: "plan", Value: "pro", Label: "Pro"}, k.For("Choice"))),
			}}
		},
	})
}
