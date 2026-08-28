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

// ExtraAttrs land on the <nav> root but never override what the
// component owns (#262): aria-label keeps its framework value and the
// data-fui-toc / data-fui-toc-levels runtime wiring cannot be spoofed.
func TestTOCExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(TableOfContents(TOCConfig{
		Target: "main", Label: "Contents", Sticky: true, Class: "mine",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "aria-label": "evil", "Class": "evil",
			"data-fui-toc": "spoof", "data-fui-toc-levels": "spoof",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `aria-label="Contents"`, `data-fui-toc="main"`,
		`data-fui-toc-levels="2,3"`, `class="ui-toc ui-toc--sticky mine"`,
	} {
		if !strings.Contains(root, want) {
			t.Errorf("nav missing %q:\n%s", want, root)
		}
	}
}
