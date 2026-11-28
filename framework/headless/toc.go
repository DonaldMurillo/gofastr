package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

const (
	PartTOCList Part = "toc-list"
	PartTOCItem Part = "toc-item"
	PartTOCLink Part = "toc-link"
)

// TOCItem is one entry.
type TOCItem struct {
	// ID is the heading's fragment id, without the #. Required.
	ID string
	// Label is the entry's visible text. Required.
	Label string
	// Level is the heading level, 1 to 6, naming the item's variant; 0 takes 2.
	Level int
}

// TableOfContentsProps configures the contents navigation.
type TableOfContentsProps struct {
	// Label names the landmark; empty takes Strings.TableOfContentsLabel.
	Label string
	// Items are the entries in document order. Required.
	Items []TOCItem
	// TargetSelector is the region whose headings mark the active entry; empty renders a static list.
	TargetSelector string

	ID         string
	ExtraAttrs html.Attrs

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
		own := html.Attrs{}
		if cls := s.Variant(PartTOCItem, "h"+strconv.Itoa(level)); cls != "" {
			own["class"] = cls
		}
		items = append(items, b.El("li", PartTOCItem, own,
			b.El("a", PartTOCLink, Attrs(map[string]string{"href": fragmentHref(it.ID)}),
				render.Text(scrubControlBytes(it.Label)))))
	}

	return b.El("nav", PartRoot, own,
		b.El("ol", PartTOCList, Internal(nil), items...),
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
