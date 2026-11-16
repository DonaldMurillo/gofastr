package headless

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// MultiSelect parts; the summary and panel are the Disclosure's.
const (
	PartMultiSelectChips Part = "multiselect-chips"
	PartMultiSelectGroup Part = "multiselect-group"
	PartMultiSelectRow   Part = "multiselect-row"
)

// MultiSelectOption is one checkbox option.
type MultiSelectOption struct {
	// Value is the form-submit value. Required.
	Value string
	// Label is the option's visible text. Required.
	Label    string
	Selected bool
	Disabled bool
}

// MultiSelectProps configures one multiselect.
type MultiSelectProps struct {
	// Name is the form-field name every checkbox shares. Required.
	Name string
	// Label is the group's name and the disclosure's summary. Required.
	Label string
	// Options are the choices, in order. Required and non-empty.
	Options []MultiSelectOption
	// Open renders the disclosure expanded.
	Open bool
	// Placeholder is the empty chips strip's text; empty takes Strings.MultiSelectPlaceholder.
	Placeholder string

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts
	Strings *Strings

	// Ctx scopes the option ids derived from Name when ID is empty.
	Ctx context.Context
}

// MultiSelect renders the checkbox-group disclosure.
func MultiSelect(p MultiSelectProps, s Classes) render.HTML {
	if p.Name == "" {
		panic("headless: MultiSelect requires Name — the checkbox group submits under one field name; a group with none submits nothing")
	}
	checkLabel("MultiSelect", "Label", p.Label)
	if len(p.Options) == 0 {
		panic("headless: MultiSelect requires at least one Option — a picker with nothing to pick is a button that opens an empty panel")
	}
	for i, opt := range p.Options {
		if opt.Value == "" {
			panic("headless: MultiSelect option " + strconv.Itoa(i) + " requires Value — an empty value would submit as the field's name with nothing after the equals")
		}
		checkLabel("MultiSelect option "+strconv.Itoa(i), "Label", opt.Label)
	}
	w := p.Strings.Resolve()
	family := make([]string, len(p.Options))
	for i := range p.Options {
		family[i] = "-opt-" + strconv.Itoa(i)
	}
	id := instanceID(p.Ctx, p.ID, p.Name, family...)
	placeholder := p.Placeholder
	if placeholder == "" {
		placeholder = w.MultiSelectPlaceholder
	}

	b := p.Parts.Box(s, PartSummary, PartPanel)

	rows := make([]render.HTML, 0, len(p.Options))
	for i, opt := range p.Options {
		optID := id + "-opt-" + strconv.Itoa(i)
		inputAttrs := Attrs(map[string]string{
			"type":  "checkbox",
			"name":  p.Name,
			"id":    optID,
			"value": scrubControlBytes(opt.Value),
		})
		if opt.Selected {
			Mark(inputAttrs, "checked")
		}
		if opt.Disabled {
			Mark(inputAttrs, "disabled")
		}
		rowAttrs := Attrs(map[string]string{"for": optID})
		rows = append(rows, b.El("label", PartMultiSelectRow, rowAttrs,
			b.El("input", PartControl, inputAttrs),
			b.El("span", PartLabel, nil, render.Text(scrubControlBytes(opt.Label))),
		))
	}

	neitherFilled := !b.Filled(PartSummary) && !b.Filled(PartPanel)
	group := b.El("fieldset", PartMultiSelectGroup,
		Internal(Attrs(map[string]string{"role": "group", "aria-label": p.Label})), rows...)

	details := Disclosure(DisclosureProps{
		Summary: render.Text(scrubControlBytes(p.Label)),
		Content: group,
		Open:    p.Open,
		Parts:   p.Parts,
	}, s)
	if neitherFilled {
		details = Own(details)
	}

	rootAttrs := Merge(Safe(p.ExtraAttrs, "id"), Attrs(map[string]string{"id": p.ID}))
	Mark(rootAttrs, "data-hui-multiselect")
	chipsAttrs := Attrs(map[string]string{
		"aria-live":                         "polite",
		"data-hui-multiselect-placeholder":  scrubControlBytes(placeholder),
		"data-hui-multiselect-remove-label": w.RemoveLabelled,
	})
	Mark(chipsAttrs, "data-hui-multiselect-chips")
	return b.El("div", PartRoot, rootAttrs,
		b.El("div", PartMultiSelectChips, Internal(chipsAttrs)),
		details,
	)
}

func init() {
	Register(Spec{
		Name:     "MultiSelect",
		Anatomy:  []Part{PartRoot, PartMultiSelectChips, PartSummary, PartPanel, PartMultiSelectGroup, PartMultiSelectRow, PartControl, PartLabel},
		Hooks:    []string{"data-hui-multiselect", "data-hui-multiselect-chips", "data-hui-multiselect-placeholder", "data-hui-multiselect-remove-label"},
		Fillable: []Part{PartSummary, PartPanel},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return MultiSelect(MultiSelectProps{
				Name: "langs", Label: "Pick languages",
				Options: []MultiSelectOption{
					{Value: "go", Label: "Go", Selected: true},
					{Value: "rust", Label: "Rust"},
				},
				Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a picker with one option checked",
				Why:  "the form contract is the checkboxes: the field name repeats for each checked option and the page submits with no script — the chips above are an enhancement the module rebuilds from the checkboxes' own state",
				HTML: MultiSelect(MultiSelectProps{
					Name: "langs", Label: "Pick languages",
					Options: []MultiSelectOption{
						{Value: "go", Label: "Go", Selected: true},
						{Value: "cpp", Label: "C++"},
						{Value: "csharp", Label: "C Sharp"},
					},
				}, s),
			}, {
				Name: "an open picker with nothing picked",
				Why:  "the disclosure is open and the chips strip carries the placeholder — the empty state is a sentence the Strings table owns, so a translated page says it in the reader's language",
				HTML: MultiSelect(MultiSelectProps{
					Name: "tags", Label: "Tags", Open: true,
					Options: []MultiSelectOption{{Value: "a", Label: "Alpha"}},
				}, s),
			}, {
				Name: "a disabled option",
				Why:  "a disabled checkbox stays in the tab-free, unsubmitting state the platform gives it; the label wraps it so the state is announced with the name",
				HTML: MultiSelect(MultiSelectProps{
					Name: "plan", Label: "Plan",
					Options: []MultiSelectOption{
						{Value: "free", Label: "Free"},
						{Value: "pro", Label: "Pro", Disabled: true},
					},
				}, s),
			}}
		},
	})
}
