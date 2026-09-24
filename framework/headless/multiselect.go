package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The multiselect: a checkbox group inside a disclosure, with a chips
// strip above it summarising what is picked. The submit contract is
// the plain form: every checkbox shares the field Name and the form
// receives the repeated key for each checked option, with no script
// at all — the chips and their remove buttons are the enhancement,
// rebuilt by the registered headless-multiselect module on the
// data-hui-multiselect marker from the checkboxes' own state. The
// disclosure itself is headless.Disclosure's (Escape to close with
// focus returned, the aria mirror), required by the module, so there
// is one disclosure implementation under this, not a second one.

// MultiSelect parts. The summary and the panel are the Disclosure's
// shared parts; the row is the label that wraps its checkbox.
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
	Label string
	// Selected checks the option on first paint.
	Selected bool
	// Disabled greys the option out and keeps it unsubmitting.
	Disabled bool
}

// MultiSelectProps configures one multiselect.
type MultiSelectProps struct {
	// Name is the form-field name every checkbox shares; the form
	// receives the repeated key for each checked option. Required.
	Name string
	// Label is the group's accessible name and the disclosure's
	// summary. Required.
	Label string
	// Options are the choices, in order. Required and non-empty.
	Options []MultiSelectOption
	// Open renders the disclosure expanded.
	Open bool
	// Placeholder is what the chips strip says when nothing is
	// picked. Empty takes Strings.MultiSelectPlaceholder.
	Placeholder string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root, the chips strip, the summary, the
	// panel, the group and the rows. No part is fillable: the
	// checkbox group is the contract.
	Parts   Parts
	Strings *Strings
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
	id := p.ID
	if id == "" {
		id = p.Name
	}
	placeholder := p.Placeholder
	if placeholder == "" {
		placeholder = w.MultiSelectPlaceholder
	}

	b := p.Parts.Box(s, PartSummary, PartPanel)

	// The rows: a label wrapping its checkbox. Option ids are
	// <instance id>-opt-<index>: the index keeps symbol-heavy values
	// ("C++" vs "C#") collision-free, and the instance id scopes them
	// across multiselects on one page.
	rows := make([]render.HTML, 0, len(p.Options))
	for i, opt := range p.Options {
		optID := id + "-opt-" + strconv.Itoa(i)
		inputAttrs := Attrs(map[string]string{
			"type": "checkbox",
			"name": p.Name,
			"id":   optID,
			// The value is data the form submits back: escaped at
			// the attribute and scrubbed of control bytes, never
			// refused — the same posture as the row's label.
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

	group := b.El("fieldset", PartMultiSelectGroup,
		Attrs(map[string]string{"role": "group", "aria-label": p.Label}), rows...)

	details := Disclosure(DisclosureProps{
		Summary: render.Text(scrubControlBytes(p.Label)),
		Content: group,
		Open:    p.Open,
		// The disclosure's own fillable parts (summary, panel) are
		// this component's: a caller's slot, attr or bind for either
		// routes through the Disclosure's Box, one implementation of
		// the disclosure under this instead of a second one.
		Parts: p.Parts,
	}, s)

	rootAttrs := Merge(Safe(p.ExtraAttrs, "id"), Attrs(map[string]string{"id": p.ID}))
	Mark(rootAttrs, "data-hui-multiselect")
	chipsAttrs := Attrs(map[string]string{
		"aria-live": "polite",
		// The placeholder is read by the sheet (::before content on
		// the empty strip), the remove label by the module. The
		// placeholder is data-shaped — a caller may derive it from a
		// stored value — so it is scrubbed like the labels.
		"data-hui-multiselect-placeholder":  scrubControlBytes(placeholder),
		"data-hui-multiselect-remove-label": w.MultiSelectRemoveLabel,
	})
	Mark(chipsAttrs, "data-hui-multiselect-chips")
	return b.El("div", PartRoot, rootAttrs,
		b.El("div", PartMultiSelectChips, chipsAttrs),
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
