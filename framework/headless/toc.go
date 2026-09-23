package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The table of contents: a navigation landmark whose entries are real
// links the SERVER rendered from explicit items. The items are
// required on purpose: a server-rendered component cannot discover
// headings that only exist in a future browser DOM, and a TOC built
// only at hydration is an empty landmark a reader without script
// never fills. The module's whole job is the active state —
// aria-current and a class on the entry whose heading is in view —
// through the observer headless-rail owns.

// TableOfContents parts.
const (
	PartTOCList Part = "toc-list"
	PartTOCItem Part = "toc-item"
	PartTOCLink Part = "toc-link"
)

// TOCItem is one entry.
type TOCItem struct {
	// ID is the fragment id of the heading this entry links to,
	// without the leading #. Required.
	ID string
	// Label is the entry's visible text. Required.
	Label string
	// Level is the heading's level, 1 to 6; 0 takes 2. It names the
	// item's variant (h2, h3, …) so a class map can indent by depth.
	Level int
}

// TableOfContentsProps configures the contents navigation.
type TableOfContentsProps struct {
	// Label names the landmark. Empty takes
	// Strings.TableOfContentsLabel.
	Label string
	// Items are the entries, in document order. Required and
	// non-empty: an empty contents landmark is a name with nothing
	// under it, and the no-script contract is the rendered list.
	Items []TOCItem
	// TargetSelector is the CSS selector of the content region whose
	// headings the module watches to mark the active entry. Empty
	// renders a purely static list: every link works, nothing is
	// marked.
	TargetSelector string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and the entries. No part is fillable:
	// the list is the contract.
	Parts   Parts
	Strings *Strings
}

// TableOfContents renders the contents navigation.
func TableOfContents(p TableOfContentsProps, s Classes) render.HTML {
	if len(p.Items) == 0 {
		panic("headless: TableOfContents requires Items — the server cannot promise a no-script contents list by inspecting a browser DOM it never sees; pass the headings you rendered")
	}
	if p.TargetSelector != "" {
		checkSelector("TableOfContents", "TargetSelector", p.TargetSelector)
	}
	w := p.Strings.Resolve()
	ids := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		checkFragmentID("TableOfContents item", "ID", it.ID)
		checkLabel("TableOfContents item with ID "+it.ID, "Label", it.Label)
		if it.Level < 0 || it.Level > 6 {
			panic("headless: TableOfContents item " + it.ID + " Level " + strconv.Itoa(it.Level) + " is not a heading level (1 to 6)")
		}
		ids = append(ids, it.ID)
	}
	checkNoDuplicateIDs("TableOfContents", ids)
	label := p.Label
	if label == "" {
		label = w.TableOfContentsLabel
	}

	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "aria-label"), Attrs(map[string]string{
		"aria-label": label,
		"id":         p.ID,
	}))
	Mark(own, "data-hui-toc")
	if p.TargetSelector != "" {
		own["data-hui-toc-target"] = p.TargetSelector
	}

	items := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		level := it.Level
		if level == 0 {
			level = 2
		}
		// The heading level names the item's variant (h2, h3, …) so a
		// class map can indent by depth; El joins it onto the part's
		// own class the way Button joins a size.
		own := html.Attrs{}
		if cls := s.Variant(PartTOCItem, "h"+strconv.Itoa(level)); cls != "" {
			own["class"] = cls
		}
		items = append(items, b.El("li", PartTOCItem, own,
			b.El("a", PartTOCLink, Attrs(map[string]string{"href": "#" + it.ID}),
				render.Text(scrubControlBytes(it.Label)))))
	}

	return b.El("nav", PartRoot, own,
		b.El("ol", PartTOCList, nil, items...),
	)
}

func init() {
	Register(Spec{
		Name:    "TableOfContents",
		Anatomy: []Part{PartRoot, PartTOCList, PartTOCItem, PartTOCLink},
		Hooks:   []string{"data-hui-toc", "data-hui-toc-target"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return TableOfContents(TableOfContentsProps{
				Items: []TOCItem{
					{ID: "overview", Label: "Overview"},
					{ID: "details", Label: "Details", Level: 3},
				},
				TargetSelector: "main", Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "an observed contents list",
				Why:  "the entries are real links the server rendered, so the no-script reader gets the whole list, and the module only marks which entry is active",
				HTML: TableOfContents(TableOfContentsProps{
					Items: []TOCItem{
						{ID: "modeling", Label: "Modeling"},
						{ID: "serving", Label: "Serving"},
						{ID: "tuning", Label: "Tuning", Level: 3},
					},
					TargetSelector: "main",
				}, s),
			}, {
				Name: "a static contents list",
				Why:  "with no target the links still work and nothing is marked — the active state is enhancement over a complete list, never the list's only source",
				HTML: TableOfContents(TableOfContentsProps{Items: []TOCItem{
					{ID: "one", Label: "One"},
				}}, s),
			}, {
				Name: "the unlabelled landmark names itself",
				Why:  "a contents navigation with no label of its own still names its landmark — the default word is the one Strings carries, so a translated page says it in the reader's language",
				HTML: TableOfContents(TableOfContentsProps{Items: []TOCItem{
					{ID: "one", Label: "One"},
				}}, s),
			}}
		},
	})
}
