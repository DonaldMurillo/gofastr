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
// swap are the kernel's data-fui-rpc contract, rendered here beside
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
	hasStatic := len(p.Options) > 0
	if p.Island == nil && !hasStatic {
		panic("headless: Combobox requires Island or Options — a combobox whose results come from nowhere is a text field")
	}
	if p.Island != nil {
		p.Island.check()
		if p.NoScriptAction == "" {
			panic("headless: Combobox requires NoScriptAction when Island is set — a reader without script must still reach the results")
		}
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

	// The carrier is a DIV, not a FORM: the HTML parser drops a nested
	// <form> open tag but honors its </form>, closing any host form the
	// combobox is embedded in. The RPC trigger attributes work on any
	// carrier; the real form (the no-script GET) wraps the whole
	// combobox instead.
	carrierAttrs := Attrs(map[string]string{})
	if p.Island != nil && !hasStatic {
		carrierAttrs["data-fui-rpc"] = p.Island.Endpoint
		carrierAttrs["data-fui-rpc-method"] = "POST"
		carrierAttrs["data-fui-rpc-trigger"] = "input"
		carrierAttrs["data-fui-rpc-debounce-ms"] = strconv.Itoa(debounce)
		carrierAttrs["data-fui-rpc-signal"] = p.Island.Signal
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
		listboxAttrs["data-fui-signal"] = p.Island.Signal
		listboxAttrs["data-fui-signal-mode"] = "html"
		Mark(listboxAttrs, "hidden")
	}
	var rows []render.HTML
	if hasStatic {
		Mark(listboxAttrs, "data-hui-combobox-static")
		Mark(listboxAttrs, "hidden")
		for _, o := range p.Options {
			rows = append(rows, comboboxOptionEl(b, o))
		}
	}

	statusAttrs := Attrs(map[string]string{"role": "status"})
	Mark(statusAttrs, "data-hui-combobox-status")
	statusAttrs["data-hui-combobox-no-results"] = w.ComboboxNoResults
	status := b.El("span", PartComboboxStatus, statusAttrs, render.HTML(""))

	body := []render.HTML{
		b.El("label", PartLabel, Attrs(map[string]string{"for": p.ID}),
			render.Text(scrubControlBytes(p.Label))),
		b.El("div", PartComboboxForm, carrierAttrs,
			b.El("input", PartComboboxInput, inputAttrs)),
		b.El("ul", PartComboboxListbox, listboxAttrs, rows...),
		status,
	}

	rootAttrs := Merge(Safe(p.ExtraAttrs, "role"), nil)
	Mark(rootAttrs, "data-hui-combobox")
	inner := b.El("div", PartRoot, rootAttrs, body...)
	if p.NoScriptAction != "" {
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
func comboboxOptionEl(b Box, o ComboboxOption) render.HTML {
	attrs := Attrs(map[string]string{
		"id": o.ID,
	})
	attrs["role"] = "option"
	if o.Value == "" {
		o.Value = o.Label
	}
	attrs["data-value"] = scrubControlBytes(o.Value)
	if o.Disabled {
		attrs["aria-disabled"] = "true"
	}
	if href := urlsafe.Clean(o.Href, urlsafe.Anchor); href != "" {
		// The module hands the pick to the SPA navigator; an unsafe
		// href drops the navigation affordance entirely.
		attrs["data-fui-push-state"] = href
	}
	kids := []render.HTML{b.El("span", PartText, nil, render.Text(scrubControlBytes(o.Label)))}
	if o.Meta != "" {
		kids = append(kids, b.El("span", PartComboboxOption, nil, render.Text(scrubControlBytes(o.Meta))))
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
			"data-hui-combobox-loading"},
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
			}}
		},
	})
}
