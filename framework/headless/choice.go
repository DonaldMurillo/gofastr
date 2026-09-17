package headless

// The choice family: checkbox, radio and switch (one control wrapped
// by its label) and the fieldset groups that hold a set of them under
// one legend. Roles, the label-wraps-control relationship and the
// group semantics live here; the row's visual shape lives in the class map.

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Choice parts: the control and its visible text are shared
// vocabulary (PartControl, PartText, PartHint); only the affix shell
// in control.go needs names of its own, declared there.

// ChoiceProps configures one checkbox or radio.
type ChoiceProps struct {
	// Type is "checkbox" or "radio".
	Type string
	// Name groups the choice. For a radio it IS the group; items that
	// must act as one group share it, which is why the group helpers
	// stamp it on every item.
	Name string
	// Value is what the choice submits when checked. Required for
	// Radio: without distinct values a group cannot tell its options
	// apart. A checkbox may leave it empty and submit "on", the HTML
	// default.
	Value string
	// Label is the visible text beside the control. Required: an
	// unlabelled choice is a bug, not a variant.
	Label string
	// Hint is secondary text under the label, for the consequence of
	// the choice rather than its meaning.
	Hint     string
	Checked  bool
	Disabled bool

	ID string
	// Extra attrs land on the input, the control that submits.
	Extra html.Attrs
}

// Choice renders label > input + span(text). The label wraps the
// control, so clicking the text toggles it with no for/id pair to
// keep in sync — and the wrap itself is the accessible target, which
// is why the control's own small box already passes WCAG 2.5.8.
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

	return El("label", s, PartRoot, nil,
		El("input", s, PartControl, input),
		El("span", s, PartText, nil, text...),
	)
}

// SwitchProps configures a toggle.
type SwitchProps struct {
	// Name is the key the switch submits under when on.
	Name string
	// Value overrides what an on switch submits. Empty submits "on",
	// the HTML default, which is right for a lone "email me" toggle.
	Value string
	// Label is the visible text beside the track. Required, same rule
	// as every choice.
	Label    string
	Checked  bool
	Disabled bool

	ID string
	// Extra attrs land on the input, the control that submits.
	Extra html.Attrs
}

// Switch renders a checkbox that looks like a track-and-thumb. The
// input keeps type=checkbox — it still submits like one — and role=
// switch states the shape to assistive tech. The track, the thumb and
// the motion are the stylesheet's, keyed off :checked, so nothing in
// this markup can fall out of step with the state.
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
	return El("label", s, PartRoot, nil,
		El("input", s, PartControl, input),
		// No part: the switch's text span carries no class today and
		// PartText is left to the class map to decide.
		El("span", s, PartText, nil, render.Text(p.Label)),
	)
}

// GroupProps configures a fieldset of choices under one legend. The
// items arrive already rendered (each wrapped by its own styled
// component) because the wrapper owns the per-item style handle.
type GroupProps struct {
	// Legend is the group's shared label. Required: a set of choices
	// with no question above them is as broken as an unlabelled input.
	Legend string

	ID    string
	Extra html.Attrs
}

// Group renders the fieldset. The legend is a real <legend> inside a
// real <fieldset>: that pair is the native group semantic, naming
// every control inside without a single aria attribute.
func Group(p GroupProps, s Classes, items ...render.HTML) render.HTML {
	if p.Legend == "" {
		panic("headless: Group requires Legend — a set of choices with no question above them is as broken as an unlabelled input")
	}
	attrs := Safe(p.Extra)
	attrsSet(attrs, "id", p.ID)
	kids := append([]render.HTML{
		El("legend", s, PartLabel, nil, render.Text(p.Legend)),
	}, items...)
	return El("fieldset", s, PartRoot, attrs, kids...)
}

func init() {
	Register(Spec{
		Name:    "Choice",
		Anatomy: []Part{PartRoot, PartControl, PartText, PartHint},
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
			}}
		},
	})
}
