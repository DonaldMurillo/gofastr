package headless

import (
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Combobox parts.
const (
	PartComboboxForm    Part = "combobox-form"
	PartComboboxInput   Part = "combobox-input"
	PartComboboxListbox Part = "combobox-listbox"
	PartComboboxOption  Part = "combobox-option"
	PartComboboxStatus  Part = "combobox-status"
	PartComboboxMeta    Part = "combobox-meta"
)

// ComboboxOption is one row in the listbox.
type ComboboxOption struct {
	// ID is the option's stable id. Empty takes "<listboxID>-opt-<n>".
	ID string
	// Value is what a pick writes into the input. Empty takes Label.
	Value string
	// Label is the option's visible text. Required.
	Label string
	// Meta is secondary text beside the label (a path, a kind).
	Meta string
	// Href turns the option into an anchor; an unsafe one is dropped.
	Href string
	// Disabled removes the option from keyboard navigation.
	Disabled bool
}

// ComboboxPick is a picker's submitted value.
type ComboboxPick struct {
	// Name is the hidden input's form name. Required.
	Name string
	// Value is the picked option's value and Label the input's text; empty when none.
	Value, Label string
	// Form is the hidden input's form attribute, when not the enclosing form.
	Form string
}

// ComboboxProps configures the combobox.
type ComboboxProps struct {
	// ID is the input's id; the listbox takes "<ID>-listbox". Required.
	ID string
	// Name is the form-submit name on the input. Required.
	Name string
	// Label names the input. Required.
	Label string

	Placeholder string

	// Island is the endpoint that re-renders the listbox and its signal.
	Island *Island

	// NoScriptAction is the same-origin GET destination; required with Island.
	NoScriptAction string

	// Options is a static list filtered client-side; it takes precedence over Island.
	Options []ComboboxOption

	// Pick makes a picker inside a host form; it requires Island and refuses NoScriptAction.
	Pick *ComboboxPick

	// Control hands the input to a host Field, which labels and describes it.
	Control *FieldControl

	// DebounceMS is the input debounce. Zero takes 250; negative is refused.
	DebounceMS int

	// Inline lays the listbox out in flow, and Escape goes on to the surface around it.
	Inline bool

	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings
}

// Combobox renders the search input with its listbox.
func Combobox(p ComboboxProps, s Classes) render.HTML {
	if p.ID == "" {
		panic("headless: Combobox requires ID — the listbox is paired to the input by it")
	}
	if p.Name == "" {
		panic("headless: Combobox requires Name — the no-script form submits the query under it")
	}
	checkLabel("Combobox", "Label", p.Label)
	hasStatic := len(p.Options) > 0 && p.Pick == nil
	if p.Pick != nil {
		if p.Island == nil {
			panic("headless: Combobox Pick requires Island — a picker searches the server")
		}
		if p.NoScriptAction != "" {
			panic("headless: Combobox Pick refuses NoScriptAction — its form would nest inside the host form the picker submits with")
		}
		if p.Pick.Name == "" {
			panic("headless: Combobox Pick requires Name — the hidden input submits the picked value under it")
		}
	}
	if p.Island == nil && !hasStatic {
		panic("headless: Combobox requires Island or Options — a combobox whose results come from nowhere is a text field")
	}
	if p.Island != nil {
		p.Island.check()
		if p.NoScriptAction == "" && p.Pick == nil {
			panic("headless: Combobox requires NoScriptAction when Island is set — a reader without script must still reach the results")
		}
	}
	if p.NoScriptAction != "" {
		if p.NoScriptAction == "#" {
			panic("headless: Combobox NoScriptAction must be a real same-origin destination, not #")
		}
		checkSameOrigin("Combobox", "NoScriptAction", p.NoScriptAction)
	}
	if p.DebounceMS < 0 {
		panic("headless: Combobox DebounceMS " + strconv.Itoa(p.DebounceMS) + " is negative — a negative debounce fires before the keystroke")
	}
	w := p.Strings.Resolve()
	debounce := p.DebounceMS
	if debounce == 0 {
		debounce = 250
	}
	listboxID := p.ID + "-listbox"

	p.Options = slices.Clone(p.Options)
	ids := make([]string, 0, len(p.Options))
	for i := range p.Options {
		if p.Options[i].ID == "" {
			p.Options[i].ID = listboxID + "-opt-" + strconv.Itoa(i)
		}
		checkFragmentID("Combobox option", "ID", p.Options[i].ID)
		ids = append(ids, p.Options[i].ID)
	}
	checkNoDuplicateIDs("Combobox", ids)

	b := p.Parts.Box(s)
	inputAttrs := Attrs(map[string]string{
		"type":              "text",
		"id":                p.ID,
		"name":              p.Name,
		"role":              "combobox",
		"aria-autocomplete": "list",
		"aria-controls":     listboxID,
		"aria-expanded":     "false",
		"autocomplete":      "off",
		"spellcheck":        "false",
	})
	Mark(inputAttrs, "data-hui-combobox-input")
	if p.Inline {
		Mark(inputAttrs, "data-hui-combobox-inline")
	}
	if p.Placeholder != "" {
		inputAttrs["placeholder"] = scrubControlBytes(p.Placeholder)
	}
	if c := p.Control; c != nil {
		if c.ID != p.ID {
			panic("headless: Combobox Control.ID " + c.ID + " is not the input's ID " + p.ID + " — the field's label would name nothing")
		}
		if c.DescribedBy != "" {
			inputAttrs["aria-describedby"] = c.DescribedBy
		}
		if c.Invalid {
			inputAttrs["aria-invalid"] = "true"
		}
		if c.Required {
			inputAttrs["aria-required"] = "true"
		}
	}
	if p.Pick != nil {
		inputAttrs["form"] = p.ID + "-search"
		inputAttrs["value"] = scrubControlBytes(p.Pick.Label)
	}

	carrierAttrs := Attrs(map[string]string{})
	if p.Island != nil && !hasStatic {
		carrierAttrs["data-cui-rpc"] = p.Island.Endpoint
		carrierAttrs["data-cui-rpc-method"] = "POST"
		carrierAttrs["data-cui-rpc-trigger"] = "input"
		carrierAttrs["data-cui-rpc-debounce-ms"] = strconv.Itoa(debounce)
		carrierAttrs["data-cui-rpc-signal"] = p.Island.Signal
		carrierAttrs["data-hui-combobox-loading"] = w.ComboboxLoading
		Mark(carrierAttrs, "data-hui-combobox-loader")
	}

	listboxAttrs := Attrs(map[string]string{
		"id":         listboxID,
		"role":       "listbox",
		"aria-label": scrubControlBytes(p.Label) + " " + w.ComboboxResultsLabel,
	})
	Mark(listboxAttrs, "data-hui-combobox-listbox")
	listboxAttrs["data-hui-combobox-count"] = w.ComboboxResultCount
	if p.Island != nil && !hasStatic {
		listboxAttrs["data-cui-signal"] = p.Island.Signal
		listboxAttrs["data-cui-signal-mode"] = "html"
		Mark(listboxAttrs, "hidden")
	}
	var rows []render.HTML
	if hasStatic {
		Mark(listboxAttrs, "data-hui-combobox-static")
		Mark(listboxAttrs, "hidden")
		for _, o := range p.Options {
			rows = append(rows, comboboxOptionEl(b, o, false, false))
		}
	}
	if p.Pick != nil {
		for _, o := range p.Options {
			rows = append(rows, comboboxOptionEl(b, o, true, false))
		}
	}

	statusAttrs := Attrs(map[string]string{"role": "status"})
	Mark(statusAttrs, "data-hui-combobox-status")
	statusAttrs["data-hui-combobox-no-results"] = w.ComboboxNoResults

	wrapped := p.NoScriptAction != ""
	labelAttrs := Attrs(map[string]string{"for": p.ID})
	if !wrapped {
		labelAttrs = Internal(labelAttrs)
		carrierAttrs = Internal(carrierAttrs)
		listboxAttrs = Internal(listboxAttrs)
		statusAttrs = Internal(statusAttrs)
	}
	status := b.El("span", PartComboboxStatus, statusAttrs, render.HTML(""))

	var body []render.HTML
	if p.Control == nil {
		body = append(body, b.El("label", PartLabel, labelAttrs,
			render.Text(scrubControlBytes(p.Label))))
	}
	if p.Pick != nil {
		value := Attrs(map[string]string{"type": "hidden", "name": p.Pick.Name, "value": p.Pick.Value})
		if p.Pick.Form != "" {
			value["form"] = p.Pick.Form
		}
		Mark(value, "data-hui-combobox-value")
		body = append(body, render.VoidTag("input", Internal(value)))
	}
	body = append(body,
		b.El("div", PartComboboxForm, carrierAttrs,
			b.El("input", PartComboboxInput, inputAttrs)),
		b.El("ul", PartComboboxListbox, listboxAttrs, rows...),
		status,
	)

	rootAttrs := Merge(Safe(p.ExtraAttrs, "role"), nil)
	Mark(rootAttrs, "data-hui-combobox")
	if p.Pick != nil {
		Mark(rootAttrs, "data-hui-combobox-pick")
	}
	if wrapped {
		rootAttrs = Internal(rootAttrs)
	}
	inner := b.El("div", PartRoot, rootAttrs, body...)
	if wrapped {
		form := b.El("form", PartComboboxForm, Attrs(map[string]string{
			"action": p.NoScriptAction,
			"method": "GET",
			"role":   "none",
		}), inner)
		return form
	}
	return inner
}

// ComboboxRows renders the option rows an island endpoint answers for
// the listbox listboxID, ids "<listboxID>-opt-<n>" unless set.
func ComboboxRows(listboxID string, opts []ComboboxOption, s Classes) render.HTML {
	b := Parts{}.Box(s)
	rows := make([]render.HTML, 0, len(opts))
	ids := make([]string, 0, len(opts))
	for i, o := range opts {
		if o.ID == "" {
			o.ID = listboxID + "-opt-" + strconv.Itoa(i)
		}
		checkFragmentID("Combobox option", "ID", o.ID)
		ids = append(ids, o.ID)
		rows = append(rows, comboboxOptionEl(b, o, true, true))
	}
	checkNoDuplicateIDs("ComboboxRows", ids)
	return render.Join(rows...)
}

func comboboxOptionEl(b Box, o ComboboxOption, labelled, root bool) render.HTML {
	attrs := Attrs(map[string]string{
		"id": o.ID,
	})
	attrs["role"] = "option"
	if o.Value == "" {
		o.Value = o.Label
	}
	attrs["data-value"] = scrubControlBytes(o.Value)
	if labelled {
		attrs["data-label"] = scrubControlBytes(o.Label)
	}
	if o.Disabled {
		attrs["aria-disabled"] = "true"
	}
	href := urlsafe.Clean(o.Href, urlsafe.Anchor)
	linked := href != "" && !o.Disabled
	if linked {
		attrs["data-cui-push-state"] = href
		attrs["href"] = href
		attrs["tabindex"] = "-1"
	}
	var own html.Attrs
	if root && !linked {
		own = Internal(Attrs(map[string]string{}))
	}
	kids := []render.HTML{b.El("span", PartText, own, render.Text(scrubControlBytes(o.Label)))}
	if o.Meta != "" {
		kids = append(kids, b.El("span", PartComboboxMeta, own, render.Text(scrubControlBytes(o.Meta))))
	}
	if linked {
		wrapperAttrs := Attrs(map[string]string{"role": "none"})
		if root {
			attrs = Internal(attrs)
		} else {
			wrapperAttrs = Internal(wrapperAttrs)
		}
		return render.Tag("li", wrapperAttrs,
			b.El("a", PartComboboxOption, attrs, kids...))
	}
	return b.El("li", PartComboboxOption, attrs, kids...)
}

func init() {
	Register(Spec{
		Name: "Combobox",
		Anatomy: []Part{PartRoot, PartLabel, PartComboboxForm, PartComboboxInput,
			PartComboboxListbox, PartComboboxOption, PartComboboxStatus, PartText, PartComboboxMeta},
		Hooks: []string{"data-hui-combobox", "data-hui-combobox-input",
			"data-hui-combobox-listbox", "data-hui-combobox-static",
			"data-hui-combobox-loader", "data-hui-combobox-status",
			"data-hui-combobox-count", "data-hui-combobox-no-results",
			"data-hui-combobox-loading", "data-hui-combobox-pick",
			"data-hui-combobox-value", "data-hui-combobox-inline"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Combobox(ComboboxProps{ID: "q", Name: "q", Label: "Search",
				Options: []ComboboxOption{{Label: "Docs"}, {Label: "Examples"}}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a static combobox",
				Why:  "the options are real content the server rendered, the input owns the keyboard, and no script is needed to read the list",
				HTML: Combobox(ComboboxProps{ID: "q", Name: "q", Label: "Search",
					Options: []ComboboxOption{
						{Label: "Docs", Meta: "/docs"},
						{Label: "Examples", Href: "/examples"},
					}}, s),
			}, {
				Name: "an island combobox",
				Why:  "the results region is bound to the island's signal, and the no-script path is the same-origin GET form wrapping the whole combobox",
				HTML: Combobox(ComboboxProps{ID: "site-q", Name: "q", Label: "Search",
					Island:         &Island{Endpoint: "/island/search", Signal: "search"},
					NoScriptAction: "/search"}, s),
			}, {
				Name: "a picker",
				Why:  "the search input is detached from the host form and a hidden input submits the picked value, so a picker sits inside a record form",
				HTML: Combobox(ComboboxProps{ID: "f-customer", Name: "q", Label: "Customer",
					Island:  &Island{Endpoint: "/api/invoices/_options/customer_id", Signal: "pick-customer"},
					Pick:    &ComboboxPick{Name: "customer_id", Value: "c1", Label: "Ada"},
					Options: []ComboboxOption{{Value: "c1", Label: "Ada"}}}, s),
			}, {
				Name: "an inline combobox",
				Why:  "the listbox is the surface's own content, a command palette's result list, so Escape goes on to close the dialog instead of stopping at the list",
				HTML: Combobox(ComboboxProps{ID: "cmd", Name: "q", Label: "Commands", Inline: true,
					Options: []ComboboxOption{{Label: "New invoice"}}}, s),
			}}
		},
	})
	Register(Spec{
		Name:    "ComboboxRows",
		Anatomy: []Part{PartComboboxOption, PartText, PartComboboxMeta},
		Cases: func(k Kit) []Case {
			return []Case{{
				Name: "an island answer",
				Why:  "the rows an endpoint swaps into a listbox carry the listbox's ids and each label a picker writes back",
				HTML: render.Tag("ul", map[string]string{"role": "listbox", "id": "f-listbox", "aria-label": "Customers"},
					ComboboxRows("f-listbox", []ComboboxOption{{Value: "c1", Label: "Ada", Meta: "ada@example.com"}}, k.Classes)),
			}}
		},
	})
}
