package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SelectionProps configures a body of selectable rows and the bar that acts on the checked ones.
type SelectionProps struct {
	Bar        render.HTML // required: acts on the selection, e.g. a bulk-action form
	Body       render.HTML // required: the selectable rows and their checkboxes
	Floating   bool        // the bar follows the rows and counts the checked ones
	Form       string      // with Floating, the bar ends with a reset button for this form
	Copy       render.HTML // with Floating and CopyURL, a control that copies the checked rows as CSV
	CopyURL    string      // same-origin route answering CSV for repeated _id parameters
	ID         string
	ExtraAttrs html.Attrs
	Parts      Parts
	Strings    *Strings // nil means the English defaults
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
	before, after, _ := strings.Cut(w.SelectionCount, "{{n}}")
	bar := []render.HTML{
		b.El("span", PartStatus, Internal(nil),
			render.Text(before),
			render.Tag("span", map[string]string{"data-hui-selection-count": ""}, render.Text(w.Number(0))),
			render.Text(after)),
		p.Bar,
	}
	if strings.TrimSpace(string(p.Copy)) != "" && p.CopyURL != "" {
		bar = append(bar, b.El("span", PartSelectionCopy, Internal(Attrs(map[string]string{
			"data-hui-selection-copy":        p.CopyURL,
			"data-hui-selection-copied":      w.SelectionCopied,
			"data-hui-selection-copy-failed": w.SelectionCopyFailed,
		})), p.Copy))
	}
	if p.Form != "" {
		bar = append(bar, b.El("button", PartControl, Internal(Attrs(map[string]string{
			"type": "reset", "form": p.Form, "aria-label": w.SelectionClear,
		})), render.Tag("span", map[string]string{"aria-hidden": "true"}, render.Text("×"))))
	}
	return b.El("div", PartRoot, root, body, b.El("div", PartActions, nil, bar...))
}

// PartSelectionCopy wraps a Selection's Copy control in its hooks.
const PartSelectionCopy Part = "selection-copy"

func init() {
	Register(Spec{
		Name:    "Selection",
		Anatomy: []Part{PartRoot, PartBody, PartActions, PartStatus, PartControl, PartSelectionCopy},
		Hooks: []string{"data-hui-selection", "data-hui-selection-count",
			"data-hui-selection-copy", "data-hui-selection-copied", "data-hui-selection-copy-failed"},
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
				Name: "copy",
				Why:  "a floating bar can copy the checked rows as CSV: the wrapper around the control names the export route and the toasts' words, and the behaviour gathers the checked rows' ids when it is pressed, since only the page knows them",
				HTML: Selection(SelectionProps{Floating: true, Form: "bulk",
					Copy: render.Tag("button", Attrs(map[string]string{"type": "button", "class": s[PartControl]}),
						render.Text((*Strings)(nil).Resolve().SelectionCopy)),
					CopyURL: "/api/apps/_export.csv",
					Bar:     render.HTML(`<form id="bulk"></form>`),
					Body:    render.HTML(`<p>rows</p>`)}, s),
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
