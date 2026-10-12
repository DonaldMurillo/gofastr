package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The combobox: an input that owns a listbox of options. The input
// keeps focus through every interaction (the ARIA pattern's rule); the
// listbox is linked by aria-controls, the active option by
// aria-activedescendant, and the open state by aria-expanded — all
// rendered server-side so a reader with no script gets a labelled
// search form whose results are real content. The module owns the
// keyboard contract and the pick; the RPC debouncing and the signal
// swap are the kernel's data-cui-rpc contract, rendered here beside
// the form's no-script destination.

// Combobox parts.
const (
	PartComboboxForm    Part = "combobox-form"
	PartComboboxInput   Part = "combobox-input"
	PartComboboxListbox Part = "combobox-listbox"
	PartComboboxOption  Part = "combobox-option"
	PartComboboxStatus  Part = "combobox-status"
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
	// Href turns the option into an anchor: picking it navigates.
	// Unsafe schemes drop the navigation affordance entirely (the
	// pick still fills the input).
	Href string
	// Disabled removes the option from keyboard navigation.
	Disabled bool
}

// ComboboxPick is a picker's submitted value.
type ComboboxPick struct {
	// Name is the hidden input's form name. Required.
	Name string
	// Value is the picked option's value; Label is the text the input
	// shows for it. Both empty when nothing is picked.
	Value, Label string
}

// ComboboxProps configures the combobox.
type ComboboxProps struct {
	// ID is the input's element id. Required; the listbox takes
	// "<ID>-listbox".
	ID string
	// Name is the form-submit name on the input. Required.
	Name string
	// Label names the input. Required.
	Label string

	// Placeholder for the input.
	Placeholder string

	// Island is the typed in-page results contract: the endpoint that
	// re-renders the listbox and the signal the region is bound to.
	// Optional — a static Options list needs no island; the no-script
	// path is the same-origin GET form below either way.
	Island *Island

	// NoScriptAction is the form's action URL, the no-script
	// destination: same-origin, a GET that submits the query. Required
	// when Island is set (a reader without script must still reach the
	// results); refused when it is "#".
	NoScriptAction string

	// Options is a static list the module filters client-side. Takes
	// precedence over the Island (no round-trip fires).
	Options []ComboboxOption

	// Pick turns the combobox into a picker inside a host form: the
	// input searches (its Name is the query the Island endpoint reads)
	// and a hidden input submits the picked option's Value. Requires
	// Island; refuses NoScriptAction, whose form would nest inside the
	// host form. Options become the listbox's first rows, replaced by
	// each search.
	Pick *ComboboxPick

	// Control hands the input to a host Field: the field's label
	// names it (the combobox draws none of its own; Label still names
	// the listbox), and its hint and error describe it.
	Control *FieldControl

	// DebounceMS bounds the input debounce. Zero takes 250; negative
	// is refused.
	DebounceMS int

	ExtraAttrs html.Attrs

	// Parts: attrs on the root, the input and the listbox. The
	// listbox's content is the Options' — not fillable.
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
		if p.NoScriptAction == "#" {
			panic("headless: Combobox NoScriptAction must be a real same-origin destination, not #")
		}
		if p.Pick == nil {
			checkSameOrigin("Combobox", "NoScriptAction", p.NoScriptAction)
		}
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

	// Option ids: caller's, checked for duplicates, or derived.
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
		// The search input names a form that does not exist, so the
		// host form never submits the query; the RPC carrier still
		// reads it.
		inputAttrs["form"] = p.ID + "-search"
		inputAttrs["value"] = scrubControlBytes(p.Pick.Label)
	}

	// The carrier is a DIV, not a FORM: the HTML parser drops a nested
	// <form> open tag but honors its </form>, closing any host form the
	// combobox is embedded in. The RPC trigger attributes work on any
	// carrier; the real form (the no-script GET) wraps the whole
	// combobox instead.
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
	// The module announces the result count into the status region
	// through this sentence — on the island listbox after each swap,
	// on the static listbox after each filter.
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

	// Nothing in a combobox is the caller's: every option is static
	// text the server rendered (a ComboboxOption carries only strings).
	// The root div is wrapped in a no-script form whenever there is a
	// NoScriptAction, so it is no longer what this function returns at
	// top level; when wrapped, the div itself is the topmost element of
	// the internal subtree, and its children must not carry a second
	// mark — when it is not wrapped, the div IS the returned root (never
	// marked), so its own children carry the mark instead.
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
		// The no-script GET: the same query, submitted as a form to the
		// same-origin destination. With script, the submit never fires
		// (Enter picks the active option instead).
		form := b.El("form", PartComboboxForm, Attrs(map[string]string{
			"action": p.NoScriptAction,
			"method": "GET",
			"role":   "none",
		}), inner)
		return form
	}
	return inner
}

// comboboxOptionEl renders one static option row.
// ComboboxRows renders the option rows an island endpoint answers for
// the listbox whose id is listboxID: ids "<listboxID>-opt-<n>" unless
// set, and each row's label as data-label, which a picker writes into
// its input.
func ComboboxRows(listboxID string, opts []ComboboxOption, s Classes) render.HTML {
	b := Parts{}.Box(s)
	rows := make([]render.HTML, 0, len(opts))
	for i, o := range opts {
		if o.ID == "" {
			o.ID = listboxID + "-opt-" + strconv.Itoa(i)
		}
		checkFragmentID("Combobox option", "ID", o.ID)
		// Each row is a root of the answer: its parts are the
		// component's own.
		rows = append(rows, comboboxOptionEl(b, o, true, true))
	}
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
	if href := urlsafe.Clean(o.Href, urlsafe.Anchor); href != "" {
		// The module hands the pick to the SPA navigator; an unsafe
		// href drops the navigation affordance entirely.
		attrs["data-cui-push-state"] = href
	}
	var own html.Attrs
	if root {
		own = Internal(Attrs(map[string]string{}))
	}
	kids := []render.HTML{b.El("span", PartText, own, render.Text(scrubControlBytes(o.Label)))}
	if o.Meta != "" {
		kids = append(kids, b.El("span", PartComboboxOption, own, render.Text(scrubControlBytes(o.Meta))))
	}
	return b.El("li", PartComboboxOption, attrs, kids...)
}

func init() {
	Register(Spec{
		Name: "Combobox",
		Anatomy: []Part{PartRoot, PartLabel, PartComboboxForm, PartComboboxInput,
			PartComboboxListbox, PartComboboxOption, PartComboboxStatus, PartText},
		Hooks: []string{"data-hui-combobox", "data-hui-combobox-input",
			"data-hui-combobox-listbox", "data-hui-combobox-static",
			"data-hui-combobox-loader", "data-hui-combobox-status",
			"data-hui-combobox-count", "data-hui-combobox-no-results",
			"data-hui-combobox-loading", "data-hui-combobox-pick",
			"data-hui-combobox-value"},
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
			}}
		},
	})
	Register(Spec{
		Name:    "ComboboxRows",
		Anatomy: []Part{PartComboboxOption, PartText},
		Cases: func(k Kit) []Case {
			return []Case{{
				Name: "an island answer",
				Why:  "the rows an endpoint swaps into a listbox carry the listbox's ids and each label a picker writes back",
				HTML: render.Tag("ul", map[string]string{"role": "listbox", "id": "f-listbox", "aria-label": "Customers"},
					ComboboxRows("f-listbox", []ComboboxOption{{Value: "c1", Label: "Ada"}}, k.Classes)),
			}}
		},
	})
}
