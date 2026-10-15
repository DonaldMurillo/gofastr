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
		`data-cui-comp="ui-toc"`,
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
	if strings.Contains(h, "data-cui-toc") {
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

// PhoneSheet wraps the nav in a sheet the pill opens: the sheet takes
// its id from the TOC's ID (a fixed one without), its title from the
// Label, and its swap from the Breakpoint.
func TestTOCPhoneSheetWrapsNav(t *testing.T) {
	items := []TOCItem{{ID: "a", Label: "A"}}
	h := string(TableOfContents(TOCConfig{Items: items, Target: "main", PhoneSheet: true}))
	for _, want := range []string{
		`class="cui-phone-sheet cui-phone-sheet--swap-md"`,
		`popovertarget="fui-toc-sheet"`,
		`<span class="cui-sheet-pill__label">On this page</span>`,
		`<div class="cui-phone-sheet__body"><nav aria-label="On this page"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
	h = string(TableOfContents(TOCConfig{Items: items, ID: "doc-toc", Label: "Sections", Breakpoint: StackBelowLG, PhoneSheet: true}))
	for _, want := range []string{
		`cui-phone-sheet--swap-lg`,
		`id="doc-toc-sheet"`,
		`<span class="cui-sheet-head__title" id="doc-toc-sheet-title">Sections</span>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
	if h := string(TableOfContents(TOCConfig{Items: items})); strings.Contains(h, "cui-phone-sheet") {
		t.Errorf("without PhoneSheet the nav renders alone:\n%s", h)
	}
}

func TestTOCPhoneSheetRefusesUnknownBreakpoint(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an unknown Breakpoint should panic at render")
		}
	}()
	TableOfContents(TOCConfig{Items: []TOCItem{{ID: "a", Label: "A"}}, PhoneSheet: true, Breakpoint: "xl"})
}
