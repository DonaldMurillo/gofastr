package ui

import (
	"strings"
	"testing"
)

func TestTOCRequiresTarget(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TableOfContents without Target should panic")
		}
	}()
	TableOfContents(TOCConfig{})
}

func TestTOCEmitsNavAndMarkers(t *testing.T) {
	h := string(TableOfContents(TOCConfig{Target: "main"}))
	if !strings.Contains(h, "<nav ") {
		t.Errorf("TOC should render as <nav>:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-toc="main"`) {
		t.Errorf("expected data-fui-toc=Target:\n%s", h)
	}
	if !strings.Contains(h, "ui-toc__list") {
		t.Errorf("expected empty .ui-toc__list ready for runtime fill:\n%s", h)
	}
}

func TestTOCDefaultLabel(t *testing.T) {
	h := string(TableOfContents(TOCConfig{Target: "main"}))
	if !strings.Contains(h, `aria-label="On this page"`) {
		t.Errorf("default Label should be 'On this page':\n%s", h)
	}
}

func TestTOCLevelsAttr(t *testing.T) {
	cases := map[int]string{0: "2,3", 2: "2", 3: "3", 5: "2,3"}
	for in, want := range cases {
		h := string(TableOfContents(TOCConfig{Target: "main", Levels: in}))
		if !strings.Contains(h, `data-fui-toc-levels="`+want+`"`) {
			t.Errorf("Levels=%d should emit %q:\n%s", in, want, h)
		}
	}
}

func TestTOCStickyClass(t *testing.T) {
	on := string(TableOfContents(TOCConfig{Target: "main", Sticky: true}))
	if !strings.Contains(on, "ui-toc--sticky") {
		t.Errorf("Sticky=true should add modifier class:\n%s", on)
	}
}
