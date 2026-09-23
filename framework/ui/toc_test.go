package ui

import (
	"strings"
	"testing"
)

func TestTOCRequiresItems(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TableOfContents without Items should panic — a runtime-filled TOC is an empty landmark without script")
		}
	}()
	TableOfContents(TOCConfig{Target: "main"})
}

func TestTOCEmitsNavAndServerRenderedList(t *testing.T) {
	h := string(TableOfContents(TOCConfig{
		Target: "main",
		Items:  []TOCItem{{ID: "overview", Label: "Overview"}, {ID: "details", Label: "Details", Level: 3}},
	}))
	for _, want := range []string{
		"<nav ",
		`data-fui-comp="ui-toc"`,
		`aria-label="On this page"`,
		`data-hui-toc="" data-hui-toc-target="main"`,
		`<a class="fui-toc__link" href="#overview">Overview</a>`,
		`class="fui-toc__item fui-toc__item--h3"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("TOC missing %q:\n%s", want, h)
		}
	}
	// The whole list is server-rendered: no runtime hook fills it.
	if strings.Contains(h, "data-fui-toc") {
		t.Errorf("the retired runtime-filled TOC wiring is still rendered:\n%s", h)
	}
}

func TestTOCDefaultLabel(t *testing.T) {
	h := string(TableOfContents(TOCConfig{Items: []TOCItem{{ID: "a", Label: "A"}}}))
	if !strings.Contains(h, `aria-label="On this page"`) {
		t.Errorf("default Label should be 'On this page':\n%s", h)
	}
}

func TestTOCStickyClass(t *testing.T) {
	on := string(TableOfContents(TOCConfig{Sticky: true, Items: []TOCItem{{ID: "a", Label: "A"}}}))
	if !strings.Contains(on, "fui-toc--sticky") {
		t.Errorf("Sticky=true should add modifier class:\n%s", on)
	}
	off := string(TableOfContents(TOCConfig{Items: []TOCItem{{ID: "a", Label: "A"}}}))
	if strings.Contains(off, "fui-toc--sticky") {
		t.Errorf("Sticky=false should not add the modifier:\n%s", off)
	}
}

func TestTOCExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(TableOfContents(TOCConfig{
		Items: []TOCItem{{ID: "a", Label: "A"}}, Label: "Contents", Sticky: true, Class: "mine",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "aria-label": "evil", "Class": "evil",
			"data-hui-toc": "forged",
		},
	}))
	for _, want := range []string{`data-test="hook"`, `aria-label="Contents"`, "fui-toc--sticky", "mine"} {
		if !strings.Contains(h, want) {
			t.Errorf("TOC lost %q:\n%s", want, h)
		}
	}
	for _, refuse := range []string{`aria-label="evil"`, `class="evil"`, `data-hui-toc="forged"`} {
		if strings.Contains(h, refuse) {
			t.Errorf("TOC let a caller forge %q:\n%s", refuse, h)
		}
	}
}
