package headless

// The text-entry family: the single-line input, the multiline
// textarea, the native select, and the two affix-shell controls
// (password with its reveal button, colour with its swatch).
// Structure, labelling and the data-hui-* hooks a runtime module binds
// to live here; heights, borders and class structure live in the class map.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Control parts. The single-line controls and the affix shells are
// their own root; the input inside an affix shell is PartControl, and
// the reveal button and swatch are named so a class map can style them
// without the markup having to carry a class for them to be found by.
const (
	PartOption      Part = "option"
	PartAffixButton Part = "affix-button"
	PartAffixSwatch Part = "affix-swatch"
)

// InputProps is the single-line control every Input flavour renders:
// one void input, the same state attrs assembled the same way. The
// flavours differ only in Type and the typed attributes they add
// (min/max/step), which is why none of them can drift in structure
// from a plain Input.
type InputProps struct {
	Type string
	// DescribedBy is the aria-describedby a Field hands down. It is
	// the whole reason a hint or an error reaches assistive tech: the
	// text is on screen either way, but without this attribute a
	// screen reader user hears the label and nothing else — no
	// format hint, and no reason the control is red.
	DescribedBy string
	Name        string
	Value       string
	Placeholder string
	Required    bool
	Disabled    bool
	// Invalid states aria-invalid for assistive tech; the class map
	// colours the border off the same attribute.
	Invalid bool
	// AriaLabel names the control where a visible label cannot go — a
	// search box in a toolbar, a field inside a table's own controls.
	// Everywhere else the label belongs to a Field, on screen where
	// everyone can read it. A placeholder is not a label: it goes away
	// on the first keystroke, its contrast is deliberately low, and
	// not every screen reader treats it as a name.
	AriaLabel string

	ID    string
	Extra html.Attrs
	// Owned are the type-specific attrs — min, max and step, and only
	// those — applied after the common set and after Extra so a caller
	// cannot widen a numeric input's bounds through extra attrs behind
	// the config's back. Any other key is refused: this is a seam for
	// bounds, not a second ExtraAttrs. Empty values are skipped,
	// leaving Extra's say if it has one.
	Owned html.Attrs
}

func Input(p InputProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Input requires Name — a control with no name submits nothing")
	}
	attrs := Safe(p.Extra)
	// A text input is what an unset type means, and it should say so:
	// type="" is what the browser guesses from, and what the goldens
	// carried for fifteen fixtures before anyone read them.
	attrs["type"] = orDefault(p.Type, "text")
	attrs["name"] = p.Name
	attrsSet(attrs, "value", p.Value)
	attrsSet(attrs, "aria-label", p.AriaLabel)
	attrsSet(attrs, "placeholder", p.Placeholder)
	attrsSet(attrs, "id", p.ID)
	attrsSet(attrs, "aria-describedby", p.DescribedBy)
	Flag(attrs, "required", p.Required)
	Flag(attrs, "disabled", p.Disabled)
	if p.Invalid {
		attrs["aria-invalid"] = "true"
	}
	seen := map[string]bool{}
	for k, v := range p.Owned {
		key := strings.ToLower(k)
		switch key {
		case "min", "max", "step":
		default:
			panic("headless: Input Owned carries " + k + " — Owned is for min, max and step only")
		}
		if v == "" {
			continue
		}
		// Two spellings of one key are one attribute to the browser
		// and an order-dependent value here; refused rather than left
		// to map iteration.
		if seen[key] {
			panic("headless: Input Owned repeats " + key + " under two spellings")
		}
		seen[key] = true
		attrs[key] = v
	}
	return El("input", s, PartRoot, attrs)
}

// TextareaProps configures a multiline control.
type TextareaProps struct {
	Name string
	// DescribedBy is the aria-describedby a Field hands down. It is
	// the whole reason a hint or an error reaches assistive tech: the
	// text is on screen either way, but without this attribute a
	// screen reader user hears the label and nothing else — no
	// format hint, and no reason the control is red.
	DescribedBy string
	Value       string
	// Placeholder is the prompt shown when empty.
	Placeholder string
	// Rows sets the rows attribute. Zero omits it and lets the
	// stylesheet size the box, so an unspecified height stays the
	// system's decision rather than the caller's guess.
	Rows     int
	Required bool
	Disabled bool
	Invalid  bool

	ID    string
	Extra html.Attrs
}

// Textarea renders the multiline control. The value is the content,
// not an attribute: setting both is what makes some browsers show the
// stale attribute after a reset.
func Textarea(p TextareaProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Textarea requires Name — a control with no name submits nothing")
	}
	attrs := Safe(p.Extra)
	attrs["name"] = p.Name
	if p.Rows > 0 {
		attrs["rows"] = strconv.Itoa(p.Rows)
	}
	attrsSet(attrs, "placeholder", p.Placeholder)
	attrsSet(attrs, "id", p.ID)
	attrsSet(attrs, "aria-describedby", p.DescribedBy)
	Flag(attrs, "required", p.Required)
	Flag(attrs, "disabled", p.Disabled)
	if p.Invalid {
		attrs["aria-invalid"] = "true"
	}
	return El("textarea", s, PartRoot, attrs, render.Text(p.Value))
}

// Option is one entry in a Select's list.
type Option struct {
	// Value is what the option submits; Label is what it shows. They
	// are separate so the submitted key stays stable while the visible
	// text is edited.
	Value string
	Label string
}

// SelectProps configures a dropdown.
type SelectProps struct {
	Name string
	// DescribedBy is the aria-describedby a Field hands down. It is
	// the whole reason a hint or an error reaches assistive tech: the
	// text is on screen either way, but without this attribute a
	// screen reader user hears the label and nothing else — no
	// format hint, and no reason the control is red.
	DescribedBy string
	// Options is the list of choices.
	Options []Option
	// Selected marks the option whose Value it matches. Empty selects
	// the placeholder (or the first option when there is none).
	Selected string
	// Placeholder renders a disabled first option with an empty value.
	// That empty value is also what makes Required enforceable: the
	// browser reads it as unfilled.
	Placeholder string
	Required    bool
	Disabled    bool
	Invalid     bool

	ID    string
	Extra html.Attrs
}

// Select renders the native dropdown. The chevron is the stylesheet's
// (appearance: none plus a drawn arrow), so the markup stays a plain
// select — no wrapper div to align against its neighbours.
func Select(p SelectProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Select requires Name — a select with no name submits nothing")
	}
	kids := make([]render.HTML, 0, len(p.Options)+1)
	if p.Placeholder != "" {
		// Disabled so it cannot be re-chosen once left; selected only
		// while nothing else is, so it is the prompt and not a value.
		ph := html.Attrs{"value": "", "disabled": ""}
		if p.Selected == "" {
			ph["selected"] = ""
		}
		kids = append(kids, El("option", s, PartOption, ph, render.Text(p.Placeholder)))
	}
	for _, o := range p.Options {
		a := html.Attrs{"value": o.Value}
		if p.Selected != "" && o.Value == p.Selected {
			a["selected"] = ""
		}
		kids = append(kids, El("option", s, PartOption, a, render.Text(o.Label)))
	}

	attrs := Safe(p.Extra)
	attrs["name"] = p.Name
	attrsSet(attrs, "id", p.ID)
	attrsSet(attrs, "aria-describedby", p.DescribedBy)
	Flag(attrs, "required", p.Required)
	Flag(attrs, "disabled", p.Disabled)
	if p.Invalid {
		attrs["aria-invalid"] = "true"
	}
	return El("select", s, PartRoot, attrs, kids...)
}

// PasswordProps configures a password control.
type PasswordProps struct {
	Name        string
	Placeholder string
	Required    bool
	Disabled    bool
	Invalid     bool

	// ID lands on the inner input, not the shell: the shell is
	// styling, and a Field label's for= must point at the control.
	ID string
	// Extra attrs land on the inner input too (autocomplete,
	// inputmode): they are attributes of the control, and putting them
	// on the shell would swallow them.
	Extra       html.Attrs
	DescribedBy string

	// Parts: attrs and binds on the shell, the input and the
	// reveal button. Strings carry the reveal button's two
	// names).
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Password renders the affix shell: a div carrying the runtime's
// data-hui-affix hook, with a borderless input and a reveal button
// inside. The shell owns the one border, so nothing stacks a border
// on a border.
//
// The reveal button is complete, correct markup — type=button so it
// never submits, an aria-label — but it is INERT until a runtime
// module exists to toggle the input's type and the button's own label.
// Nothing is wired on purpose: no inline script, no dead onclick, and
// no pretending it works before it does.
func Password(p PasswordProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Name == "" {
		panic("headless: Password requires Name — a control with no name submits nothing")
	}
	input := Merge(Safe(p.Extra), html.Attrs{
		"data-hui-affix-input": "",
		"type":                 "password",
		"name":                 p.Name,
	})
	attrsSet(input, "placeholder", p.Placeholder)
	attrsSet(input, "id", p.ID)
	attrsSet(input, "aria-describedby", p.DescribedBy)
	Flag(input, "required", p.Required)
	Flag(input, "disabled", p.Disabled)
	if p.Invalid {
		input["aria-invalid"] = "true"
	}

	// The module that binds data-hui-reveal retypes the input and swaps
	// these four strings, so both the label and the accessible name
	// stay true to what the button will do next. They travel as data-*
	// rather than being hardcoded in that module so a caller can
	// localise them.
	reveal := html.Attrs{
		"type":                "button",
		"data-hui-reveal":     "",
		"aria-pressed":        "false",
		"aria-label":          p.Strings.Resolve().ShowPassword,
		"data-hui-show-label": p.Strings.Resolve().ShowPassword,
		"data-hui-hide-label": p.Strings.Resolve().HidePassword,
		"data-hui-show-text":  p.Strings.Resolve().RevealShow,
		"data-hui-hide-text":  p.Strings.Resolve().RevealHide,
	}
	Flag(reveal, "disabled", p.Disabled)

	shell := html.Attrs{"data-hui-affix": ""}
	if p.Invalid {
		// The class map colours the shell's border off data-invalid; the
		// input inside has no border of its own to colour.
		shell["data-invalid"] = ""
	}

	return b.El("div", PartRoot, shell,
		b.El("input", PartControl, input),
		b.El("button", PartAffixButton, reveal, render.Text(p.Strings.Resolve().RevealShow)),
	)
}

// ColorProps configures a colour control.
type ColorProps struct {
	Name string
	// Value is the colour as text, usually but not always #rrggbb.
	Value    string
	Disabled bool
	Invalid  bool

	// ID and extra attrs land on the hex text input, the control that
	// submits.
	ID          string
	Extra       html.Attrs
	DescribedBy string

	// Parts: attrs and binds on the shell, the hex input and the
	// swatch. Strings carry the swatch's name.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Color renders the colour control as an affix shell: the native
// picker reduced to a swatch beside a readonly hex readout, so the
// field reads as one control-height input instead of a bare OS
// widget. The swatch carries no name and never submits; the hex text
// is the value and the swatch is a picker bound to it, which is also
// why the two never swap roles. The hex readout's tabindex stays 0
// (it IS the control); the swatch is taken out of the tab order
// (tabindex -1) so there is exactly one focus target and one
// submitted value. There is no Required notion here because
// type=color always has a value; a required field that cannot bite
// would be a lie.
func Color(p ColorProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Name == "" {
		panic("headless: Color requires Name — the hex field is what submits, and without a name it sends nothing")
	}
	// The swatch needs #rrggbb; the text does not. A value the picker
	// cannot show — a token reference, a CSS colour name — is kept
	// verbatim in the text and the shell is marked, rather than being
	// rewritten to black on the way in. Rejecting it outright would
	// make the component unusable for exactly the config it exists to
	// edit.
	value, pickable := swatchValue(p.Value)
	if !pickable {
		value = "#000000"
	}

	// The TEXT input carries the name, not the swatch.
	//
	// A colour in this app's config is not always a colour the OS
	// picker can show: "var(--color-primary)" is a legitimate value
	// and type=color would silently rewrite it to #000000 on submit.
	swatch := html.Attrs{
		"data-hui-affix-swatch": "",
		"type":                  "color",
		"value":                 value,
		"tabindex":              "-1",
		"aria-label":            fmt.Sprintf(p.Strings.Resolve().PickColor, p.Name),
	}
	if p.ID != "" {
		swatch["id"] = p.ID + "-picker"
	}
	Flag(swatch, "disabled", p.Disabled)

	hex := Merge(Safe(p.Extra), html.Attrs{
		"data-hui-affix-input": "",
		"type":                 "text",
		"name":                 p.Name,
		"value":                p.Value,
		"spellcheck":           "false",
		"autocapitalize":       "off",
		"autocomplete":         "off",
	})
	attrsSet(hex, "id", p.ID)
	attrsSet(hex, "aria-describedby", p.DescribedBy)
	Flag(hex, "disabled", p.Disabled)
	if p.Invalid {
		hex["aria-invalid"] = "true"
	}

	shell := html.Attrs{"data-hui-affix": "", "data-hui-color": ""}
	if p.Invalid || (p.Value != "" && !pickable) {
		shell["data-invalid"] = ""
	}

	return b.El("div", PartRoot, shell,
		b.El("input", PartAffixSwatch, swatch),
		b.El("input", PartControl, hex),
	)
}

// attrsSet sets key only when val is non-empty, the "zero value means
// absent" rule for the string attributes a component owns.
func attrsSet(a html.Attrs, key, val string) {
	if val != "" {
		a[key] = val
	}
}

// swatchValue returns the #rrggbb the picker can show for s, and
// whether s is a colour it can show at all. The short form is a colour:
// the module that binds this component expands #abc the same way on
// every keystroke, so refusing it here would open a legal value in the
// error state and clear the error the moment its owner retyped it.
func swatchValue(s string) (string, bool) {
	if !isHexColor(s) {
		return s, false
	}
	if len(s) == 4 {
		return "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]}), true
	}
	return s, true
}

// isHexColor reports whether s is #rgb or #rrggbb, the value forms
// input type=color accepts.
func isHexColor(s string) bool {
	if len(s) != 7 && len(s) != 4 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func init() {
	Register(Spec{
		Name:    "Input",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			// The hint and the error are rendered beside the control
			// rather than left implied. A fixture whose
			// aria-describedby points outside itself is a fixture that
			// cannot prove the relationship works, and proving it is
			// the whole reason the attribute is there.
			return []Case{{
				Name: "described",
				Why:  "the description it is handed is written out: six of the eight text controls once accepted DescribedBy and dropped it, so the field wired a relationship to an attribute nobody rendered — here the control sits in a Field the way a page would use it, and the hint arrives through that relationship",
				HTML: Field(FieldProps{Label: "Port", For: "input-port", Hint: "1–65535"}, k.For("Field"),
					func(c FieldControl) render.HTML {
						return Input(InputProps{Name: "port", ID: c.ID, DescribedBy: c.DescribedBy, Value: "8080"}, s)
					}),
			}, {
				Name: "invalid",
				Why:  "aria-invalid is the state; the red border is the picture of it",
				HTML: Field(FieldProps{Label: "Port", For: "input-port-bad", Error: "Already in use."}, k.For("Field"),
					func(c FieldControl) render.HTML {
						return Input(InputProps{Name: "port", ID: c.ID, Invalid: c.Invalid, DescribedBy: c.DescribedBy}, s)
					}),
			}}
		},
	})

	Register(Spec{
		Name:    "Textarea",
		Anatomy: []Part{PartRoot},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "with content",
				Why:  "the value is the element's text, not an attribute — which is why it is the one control whose content must be escaped rather than quoted",
				HTML: Textarea(TextareaProps{Name: "notes", ID: "notes", Value: "Restarted after the 4am alert."}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Select",
		Anatomy: []Part{PartRoot, PartOption},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "options",
				Why:  "a real select, because the platform's own picker is the one thing that already works on a phone, with a keyboard, and under every screen reader",
				HTML: Select(SelectProps{Name: "region", ID: "region", Options: []Option{
					{Value: "us-east", Label: "US East"},
					{Value: "eu-west", Label: "EU West"},
				}, Selected: "eu-west"}, s),
			}}
		},
	})

	Register(Spec{
		Name:    "Password",
		Anatomy: []Part{PartRoot, PartControl, PartAffixButton},
		Hooks: []string{"data-hui-affix", "data-hui-affix-input", "data-hui-reveal",
			"data-hui-show-label", "data-hui-hide-label", "data-hui-show-text", "data-hui-hide-text"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Password(PasswordProps{Name: "token", ID: "token", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "revealable",
				Why:  "the reveal button says what it will do next rather than what the field is doing now, and the labels it swaps between are published for the runtime instead of built from a class",
				HTML: Field(FieldProps{Label: "Registry token", For: "password-token"}, k.For("Field"),
					func(c FieldControl) render.HTML {
						return Password(PasswordProps{Name: "token", ID: c.ID}, s)
					}),
			}}
		},
	})

	Register(Spec{
		Name:    "Color",
		Anatomy: []Part{PartRoot, PartControl, PartAffixSwatch},
		Hooks:   []string{"data-hui-affix", "data-hui-color", "data-hui-affix-input", "data-hui-affix-swatch"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Color(ColorProps{Name: "accent", ID: "accent", Value: "#10b981", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "swatch",
				Why:  "a colour input is useless alone to anyone who cannot see it, so the hex value stays beside it as text that can be read and typed — inside a Field, so the value is a labelled setting and not a bare control",
				HTML: Field(FieldProps{Label: "Accent colour", For: "color-accent"}, k.For("Field"),
					func(c FieldControl) render.HTML {
						return Color(ColorProps{Name: "accent", ID: c.ID, Value: "#10b981"}, s)
					}),
			}}
		},
	})
}
