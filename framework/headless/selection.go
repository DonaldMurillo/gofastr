package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Selection ──────────────────────────────────────────────────────

// SelectionProps configures a Selection: a body of selectable rows and
// the bar that acts on the rows checked in it. Showing the bar only
// while a row is checked is the class map's (a :has rule); this layer
// owns the order, the count slot and the clear control.
type SelectionProps struct {
	// Bar acts on the selection: a bulk-action form whose checkboxes
	// live in Body (the inputs name it through form=). Required.
	Bar render.HTML
	// Body holds the selectable rows and their checkboxes. Required.
	Body render.HTML
	// Floating draws the bar after the rows, so it can be held to the
	// bottom of the screen, and gives it the count of rows checked,
	// which the behaviour writes into data-hui-selection-count. A
	// select-all box (data-hui-table-select-all) is not a row.
	Floating bool
	// Form is the id of the form the row checkboxes join. With
	// Floating, the bar ends with a reset button for it: resetting the
	// form clears every row joined to it.
	Form string
	ID   string
	// ExtraAttrs land on the root; class, id and the hooks are the
	// component's.
	ExtraAttrs html.Attrs
	Parts      Parts
	// Strings are the strings this component says. Nil means the
	// English defaults.
	Strings *Strings
}

// Selection renders the rows and their bar.
func Selection(p SelectionProps, s Classes) render.HTML {
	if strings.TrimSpace(string(p.Bar)) == "" {
		panic("headless: Selection requires Bar — with nothing to act on the selection, render Body alone")
	}
	if strings.TrimSpace(string(p.Body)) == "" {
		panic("headless: Selection requires Body — the rows the bar acts on")
	}
	b := p.Parts.Box(s)
	root := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	body := b.El("div", PartBody, nil, p.Body)
	if !p.Floating {
		return b.El("div", PartRoot, root, b.El("div", PartActions, nil, p.Bar), body)
	}
	root = Mark(root, "data-hui-selection")
	w := p.Strings.Resolve()
	before, after, _ := strings.Cut(w.SelectionCount, "{n}")
	bar := []render.HTML{
		b.El("span", PartStatus, Internal(nil),
			render.Text(before),
			render.Tag("span", map[string]string{"data-hui-selection-count": ""}, render.Text("0")),
			render.Text(after)),
		p.Bar,
	}
	if p.Form != "" {
		bar = append(bar, b.El("button", PartControl, Internal(Attrs(map[string]string{
			"type": "reset", "form": p.Form, "aria-label": w.SelectionClear,
		})), render.Tag("span", map[string]string{"aria-hidden": "true"}, render.Text("×"))))
	}
	return b.El("div", PartRoot, root, body, b.El("div", PartActions, nil, bar...))
}

func init() {
	Register(Spec{
		Name:    "Selection",
		Anatomy: []Part{PartRoot, PartBody, PartActions, PartStatus, PartControl},
		Hooks:   []string{"data-hui-selection", "data-hui-selection-count"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Selection(SelectionProps{Floating: true, Form: "bulk", Parts: parts,
				Bar:  render.HTML(`<form id="bulk"></form>`),
				Body: render.HTML(`<p>rows</p>`)}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "above",
				Why:  "the bar sits above the rows it acts on and shows only while one is checked, so a list nobody is selecting from carries no action form",
				HTML: Selection(SelectionProps{
					Bar:  render.HTML(`<form id="bulk"></form>`),
					Body: render.HTML(`<p>rows</p>`)}, s),
			}, {
				Name: "floating",
				Why:  "a floating bar comes after the rows so it can stay in view at the bottom of the screen, says how many rows it acts on, and clears them by resetting their form, which needs no script",
				HTML: Selection(SelectionProps{Floating: true, Form: "bulk",
					Bar:  render.HTML(`<form id="bulk"></form>`),
					Body: render.HTML(`<p>rows</p>`)}, s),
			}}
		},
	})
}
