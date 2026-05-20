package nestedlist

import (
	"strings"
	"testing"
)

func TestRender_FlatItems(t *testing.T) {
	got := string(Render(Config{
		Items: []Item{
			{Label: "Alpha"},
			{Label: "Beta"},
		},
	}))
	if !strings.Contains(got, "<ul") {
		t.Errorf("expected <ul>, got: %s", got)
	}
	if !strings.Contains(got, "Alpha") || !strings.Contains(got, "Beta") {
		t.Errorf("expected items rendered, got: %s", got)
	}
	if strings.Contains(got, "<details") {
		t.Errorf("flat items should not use <details>, got: %s", got)
	}
}

func TestRender_Ordered(t *testing.T) {
	got := string(Render(Config{
		Ordered: true,
		Items:   []Item{{Label: "Step 1"}},
	}))
	if !strings.Contains(got, "<ol") {
		t.Errorf("expected <ol> for Ordered=true, got: %s", got)
	}
}

func TestRender_LeafWithHref(t *testing.T) {
	got := string(Render(Config{
		Items: []Item{
			{Label: "Docs", Href: "/docs"},
		},
	}))
	if !strings.Contains(got, `href="/docs"`) {
		t.Errorf("expected anchor link, got: %s", got)
	}
	if !strings.Contains(got, ">Docs<") {
		t.Errorf("expected link text, got: %s", got)
	}
}

func TestRender_NestedItemUsesDetails(t *testing.T) {
	got := string(Render(Config{
		Items: []Item{
			{Label: "Parent", Children: []Item{
				{Label: "Child"},
			}},
		},
	}))
	if !strings.Contains(got, "<details") {
		t.Errorf("expected <details> for parent with children, got: %s", got)
	}
	if !strings.Contains(got, "<summary") {
		t.Errorf("expected <summary>, got: %s", got)
	}
	if !strings.Contains(got, "Child") {
		t.Errorf("expected nested child label, got: %s", got)
	}
}

func TestRender_ExpandedInitiallyOpen(t *testing.T) {
	got := string(Render(Config{
		Items: []Item{
			{Label: "P", Expanded: true, Children: []Item{{Label: "C"}}},
		},
	}))
	if !strings.Contains(got, "open") {
		t.Errorf("expected open attribute on <details> when Expanded=true, got: %s", got)
	}
}

func TestRender_AriaLabelOnWrapper(t *testing.T) {
	got := string(Render(Config{
		AriaLabel: "Settings",
		Items:     []Item{{Label: "x"}},
	}))
	if !strings.Contains(got, `aria-label="Settings"`) {
		t.Errorf("expected aria-label on wrapper, got: %s", got)
	}
}

func TestRender_EmptyItemsReturnsEmptyList(t *testing.T) {
	got := string(Render(Config{}))
	if !strings.Contains(got, "<ul") {
		t.Errorf("expected empty <ul>, got: %s", got)
	}
}

func TestBaseCSS_DefinesNestedListClasses(t *testing.T) {
	css := BaseCSS()
	for _, cls := range []string{
		".nested-list",
		".nested-list details",
		".nested-list summary",
	} {
		if !strings.Contains(css, cls) {
			t.Errorf("BaseCSS missing rule for %s", cls)
		}
	}
}
