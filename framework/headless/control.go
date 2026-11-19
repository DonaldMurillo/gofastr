package headless

import (
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Control parts. The input inside an affix shell is PartControl.
const (
	PartOption      Part = "option"
	PartAffixButton Part = "affix-button"
	PartAffixSwatch Part = "affix-swatch"
)

// InputProps is the single-line control every Input flavour renders.
type InputProps struct {
	Type string
	// DescribedBy is the aria-describedby a Field hands down.
	DescribedBy string
	Name        string
	Value       string
	Placeholder string
	Required    bool
	Disabled    bool
	Invalid     bool
	// AriaLabel names the control where a visible label cannot go.
	AriaLabel string

	ID    string
	Extra html.Attrs
	// Owned holds min, max and step only, set over Extra.
	Owned html.Attrs
}

func Input(p InputProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Input requires Name — a control with no name submits nothing")
	}
	attrs := Safe(p.Extra)
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
	Name        string
	DescribedBy string
	Value       string
	Placeholder string
	Rows        int
	Required    bool
	Disabled    bool
	Invalid     bool
	// Autogrow renders data-cui-autogrow, the runtime's auto-resize.
	Autogrow bool
	// JSON is the sentence said when the text is not JSON.
	JSON string

	ID    string
	Extra html.Attrs
}

// Textarea renders the multiline control, with the value as its content.
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
	Flag(attrs, "data-cui-autogrow", p.Autogrow)
	if p.JSON != "" {
		attrs["data-hui-json"] = scrubControlBytes(p.JSON)
	}
	Flag(attrs, "required", p.Required)
	Flag(attrs, "disabled", p.Disabled)
	if p.Invalid {
		attrs["aria-invalid"] = "true"
	}
	value := p.Value
	if strings.HasPrefix(value, "\n") || strings.HasPrefix(value, "\r") {
		value = "\n" + value
	}
	return El("textarea", s, PartRoot, attrs, render.Text(value))
}

// Option is one entry in a Select's list.
type Option struct {
	Value string
	Label string
	// ExtraAttrs is option metadata; it cannot set value, selected or disabled.
	ExtraAttrs html.Attrs
}

// SelectProps configures a dropdown.
type SelectProps struct {
	Name        string
	DescribedBy string
	Options     []Option
	// Selected marks the matching option; an unlisted value draws its own.
	Selected string
	// Placeholder renders a disabled first option with an empty value.
	Placeholder string
	Required    bool
	Disabled    bool
	Invalid     bool

	ID    string
	Extra html.Attrs
}

// Select renders the native dropdown.
func Select(p SelectProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: Select requires Name — a select with no name submits nothing")
	}
	kids := make([]render.HTML, 0, len(p.Options)+1)
	if p.Placeholder != "" {
		ph := html.Attrs{"value": "", "disabled": ""}
		if p.Selected == "" {
			ph["selected"] = ""
		}
		kids = append(kids, El("option", s, PartOption, Internal(ph), render.Text(p.Placeholder)))
	}
	for _, o := range p.Options {
		a := Safe(o.ExtraAttrs, "value", "selected", "disabled")
		a["value"] = o.Value
		if o.Value == p.Selected && (p.Selected != "" || p.Placeholder == "") {
			a["selected"] = ""
		}
		kids = append(kids, El("option", s, PartOption, Internal(a), render.Text(o.Label)))
	}
	if p.Selected != "" && !slices.ContainsFunc(p.Options, func(o Option) bool { return o.Value == p.Selected }) {
		kids = append(kids, El("option", s, PartOption, Internal(html.Attrs{"value": p.Selected, "selected": ""}), render.Text(p.Selected)))
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

	// ID and Extra land on the inner input, not the shell.
	ID          string
	Extra       html.Attrs
	DescribedBy string

	Parts Parts
	// Strings are the words it says; nil means the English defaults.
	Strings *Strings
}

// Password renders a password input and a reveal toggle in an affix shell.
func Password(p PasswordProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Name == "" {
		panic("headless: Password requires Name — a control with no name submits nothing")
	}
	// The reveal retypes the input to text, where a spell checker (some
	// send the text off the device) and auto-capitalisation would read
	// or change the secret: both stay off whichever type it shows.
	input := Merge(Safe(p.Extra), html.Attrs{
		"data-hui-affix-input": "",
		"type":                 "password",
		"name":                 p.Name,
		"spellcheck":           "false",
		"autocapitalize":       "off",
	})
	attrsSet(input, "placeholder", p.Placeholder)
	attrsSet(input, "id", p.ID)
	attrsSet(input, "aria-describedby", p.DescribedBy)
	Flag(input, "required", p.Required)
	Flag(input, "disabled", p.Disabled)
	if p.Invalid {
		input["aria-invalid"] = "true"
	}

	reveal := html.Attrs{
		"type":            "button",
		"data-hui-reveal": "",
		"aria-pressed":    "false",
		"aria-label":      p.Strings.Resolve().ShowPassword,
	}
	Flag(reveal, "disabled", p.Disabled)

	shell := html.Attrs{"data-hui-affix": ""}
	if p.Invalid {
		shell["data-invalid"] = ""
	}

	return b.El("div", PartRoot, shell,
		b.El("input", PartControl, Internal(input)),
		b.El("button", PartAffixButton, Internal(reveal), render.Text(p.Strings.Resolve().RevealShow)),
	)
}

// ColorProps configures a colour control.
type ColorProps struct {
	Name     string
	Value    string
	Disabled bool
	Invalid  bool
	Required bool

	// ID and Extra land on the hex text input.
	ID          string
	Extra       html.Attrs
	DescribedBy string

	Parts Parts
	// Strings are the words it says; nil means the English defaults.
	Strings *Strings
}

// Color renders an unnamed swatch beside the hex text input that submits.
func Color(p ColorProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	if p.Name == "" {
		panic("headless: Color requires Name — the hex field is what submits, and without a name it sends nothing")
	}
	value, pickable := swatchValue(p.Value)
	if !pickable {
		value = "#000000"
	}

	w := p.Strings.Resolve()
	swatch := html.Attrs{
		"data-hui-affix-swatch": "",
		"type":                  "color",
		"value":                 value,
		"tabindex":              "-1",
		"aria-label":            w.fill(w.PickColor, "name", p.Name),
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
	Flag(hex, "required", p.Required)
	if p.Invalid {
		hex["aria-invalid"] = "true"
	}

	shell := html.Attrs{"data-hui-affix": "", "data-hui-color": ""}
	if p.Invalid || (p.Value != "" && !pickable) {
		shell["data-invalid"] = ""
	}

	return b.El("div", PartRoot, shell,
		b.El("input", PartAffixSwatch, Internal(swatch)),
		b.El("input", PartControl, Internal(hex)),
	)
}

func attrsSet(a html.Attrs, key, val string) {
	if val != "" {
		a[key] = val
	}
}

func swatchValue(s string) (string, bool) {
	if !isHexColor(s) {
		return s, false
	}
	if len(s) == 4 {
		return "#" + string([]byte{s[1], s[1], s[2], s[2], s[3], s[3]}), true
	}
	return s, true
}

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
		Hooks:   []string{"data-hui-json"},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "with content",
				Why:  "the value is the element's text, not an attribute — which is why it is the one control whose content must be escaped rather than quoted",
				HTML: Textarea(TextareaProps{Name: "notes", ID: "notes", Value: "Restarted after the 4am alert."}, s),
			}, {
				Name: "growing",
				Why:  "autogrow is the one core-runtime hook a control here renders — the behaviour belongs to the control, and the prop is the only seam that survives the prefix refusal",
				HTML: Field(FieldProps{Label: "Release notes", For: "notes-grow"}, k.For("Field"),
					func(c FieldControl) render.HTML {
						return Textarea(TextareaProps{Name: "notes", ID: c.ID, Value: "Grows to fit whatever is typed.", Autogrow: true}, s)
					}),
			}, {
				Name: "json",
				Why:  "a JSON field says the text is not JSON as it is typed and before the submit, where the server would only say so after",
				HTML: Textarea(TextareaProps{Name: "meta", ID: "meta", Value: `{"plan": "pro"}`, JSON: "Enter valid JSON"}, s),
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
		Hooks:   []string{"data-hui-affix", "data-hui-affix-input", "data-hui-reveal"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Password(PasswordProps{Name: "token", ID: "token", Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "revealable",
				Why:  "the reveal button is a toggle button: one name (Show password) and one word that never change, with aria-pressed saying whether the password is shown — a name that flipped to Hide password read as Hide password, pressed",
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
