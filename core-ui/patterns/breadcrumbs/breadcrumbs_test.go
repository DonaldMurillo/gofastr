package breadcrumbs

import (
	"strings"
	"testing"
)

func TestRequiresAtLeastOneCrumb(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with no crumbs")
		}
	}()
	New(Config{})
}

func TestCrumbRequiresText(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with empty Crumb.Text")
		}
	}()
	New(Config{}, Crumb{Href: "/"})
}

func TestRendersNavWithDefaultLabel(t *testing.T) {
	h := string(New(Config{}, Crumb{Text: "Home", Href: "/"}))
	if !strings.Contains(h, `aria-label="Breadcrumb"`) {
		t.Errorf("expected default nav aria-label, got: %s", h)
	}
}

func TestCustomLabel(t *testing.T) {
	h := string(New(Config{Label: "You are here"}, Crumb{Text: "Home"}))
	if !strings.Contains(h, `aria-label="You are here"`) {
		t.Errorf("expected custom label, got: %s", h)
	}
}

func TestNonLastCrumbsRenderAsLinks(t *testing.T) {
	h := string(New(Config{},
		Crumb{Text: "Home", Href: "/"},
		Crumb{Text: "Docs", Href: "/docs/"},
		Crumb{Text: "Page"},
	))
	if strings.Count(h, "<a ") != 2 {
		t.Errorf("expected 2 links, got %d in: %s", strings.Count(h, "<a "), h)
	}
}

func TestLastCrumbAriaCurrent(t *testing.T) {
	h := string(New(Config{},
		Crumb{Text: "Home", Href: "/"},
		Crumb{Text: "Page"},
	))
	if !strings.Contains(h, `aria-current="page"`) {
		t.Errorf("expected aria-current on last crumb, got: %s", h)
	}
	if strings.Count(h, `aria-current`) != 1 {
		t.Errorf("expected exactly one aria-current, got %d", strings.Count(h, `aria-current`))
	}
}

func TestExplicitCurrentEvenWithHref(t *testing.T) {
	h := string(New(Config{},
		Crumb{Text: "Home", Href: "/"},
		Crumb{Text: "Self", Href: "/self", Current: true},
	))
	if !strings.Contains(h, `aria-current="page"`) {
		t.Errorf("expected aria-current with explicit Current=true, got: %s", h)
	}
}

func TestEscapesText(t *testing.T) {
	h := string(New(Config{},
		Crumb{Text: "<script>", Href: "/"},
		Crumb{Text: "page"},
	))
	if strings.Contains(h, "<script>") {
		t.Errorf("expected escaped text, got: %s", h)
	}
}
