package headless

import (
	"strings"
	"testing"
)

func renderTOC(p TableOfContentsProps) string { return string(TableOfContents(p, nil)) }

func TestTableOfContentsRendersTheServerList(t *testing.T) {
	h := renderTOC(TableOfContentsProps{TargetSelector: "main", Items: []TOCItem{
		{ID: "modeling", Label: "Modeling"},
		{ID: "tuning", Label: "Tuning", Level: 3},
	}})
	for _, want := range []string{
		`<nav aria-label="On this page" data-hui-toc="" data-hui-toc-target="main">`,
		`<a href="#modeling">Modeling</a>`,
		`<a href="#tuning">Tuning</a>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("toc missing %q:\n%s", want, h)
		}
	}
	// The no-script contract is the rendered list: every entry is an
	// anchor the server wrote, and no entry waits for the module.
	if n := strings.Count(h, "<a href=\"#"); n != 2 {
		t.Errorf("toc rendered %d links, want 2:\n%s", n, h)
	}
}

func TestTableOfContentsIndentsByHeadingLevelVariant(t *testing.T) {
	classes := Classes{
		PartRoot:    "fui-toc",
		PartTOCItem: "fui-toc__item",
		PartTOCLink: "fui-toc__link",
	}
	classes[Part(string(PartTOCItem)+"--h3")] = "fui-toc__item--h3"
	h := string(TableOfContents(TableOfContentsProps{Items: []TOCItem{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B", Level: 3},
		{ID: "c", Label: "C", Level: 0}, // zero takes h2
	}}, classes))
	if !strings.Contains(h, `class="fui-toc__item fui-toc__item--h3"`) {
		t.Errorf("the h3 item carries no indent variant:\n%s", h)
	}
	if strings.Count(h, `class="fui-toc__item"`) != 2 {
		t.Errorf("the h2 items should carry the plain item class:\n%s", h)
	}
}

func TestTableOfContentsSaysItsLabelThroughStrings(t *testing.T) {
	h := string(TableOfContents(TableOfContentsProps{
		Items:   []TOCItem{{ID: "a", Label: "A"}},
		Strings: &Strings{TableOfContentsLabel: "Sur cette page"},
	}, nil))
	if !strings.Contains(h, `aria-label="Sur cette page"`) {
		t.Errorf("the translated label never arrived:\n%s", h)
	}
	// A partial translation falls back to the English default.
	h = string(TableOfContents(TableOfContentsProps{
		Items:   []TOCItem{{ID: "a", Label: "A"}},
		Strings: &Strings{},
	}, nil))
	if !strings.Contains(h, `aria-label="On this page"`) {
		t.Errorf("an empty Strings dropped the default label:\n%s", h)
	}
}

func TestTableOfContentsScrubsCarriedLabels(t *testing.T) {
	h := renderTOC(TableOfContentsProps{Items: []TOCItem{{ID: "a", Label: "Get\r\nting started"}}})
	if !strings.Contains(h, ">Getting started<") {
		t.Errorf("a CR LF in a carried label reached the reader:\n%s", h)
	}
}

func TestTableOfContentsRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    TableOfContentsProps
	}{
		{"no items — target alone is not a contents list", TableOfContentsProps{TargetSelector: "main"}},
		{"no id", TableOfContentsProps{Items: []TOCItem{{Label: "A"}}}},
		{"no label", TableOfContentsProps{Items: []TOCItem{{ID: "a"}}}},
		{"whitespace-only item label", TableOfContentsProps{Items: []TOCItem{{ID: "a", Label: "   "}}}},
		{"duplicate ids", TableOfContentsProps{Items: []TOCItem{{ID: "a", Label: "A"}, {ID: "a", Label: "B"}}}},
		{"level above six", TableOfContentsProps{Items: []TOCItem{{ID: "a", Label: "A", Level: 7}}}},
		{"negative level", TableOfContentsProps{Items: []TOCItem{{ID: "a", Label: "A", Level: -1}}}},
		{"control bytes in target", TableOfContentsProps{TargetSelector: "ma\rin", Items: []TOCItem{{ID: "a", Label: "A"}}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			TableOfContents(tc.p, nil)
		}()
	}
}
