package accordion

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/render"
)

func mustContain(t *testing.T, h render.HTML, sub string) {
	t.Helper()
	if !strings.Contains(string(h), sub) {
		t.Fatalf("expected HTML to contain %q\ngot: %s", sub, h)
	}
}

func mustNotContain(t *testing.T, h render.HTML, sub string) {
	t.Helper()
	if strings.Contains(string(h), sub) {
		t.Fatalf("expected HTML NOT to contain %q\ngot: %s", sub, h)
	}
}

func TestGroupRequiresName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Name is empty")
		}
	}()
	Group(GroupConfig{}, Item{Summary: "x", Content: render.Text("y")})
}

func TestItemRequiresSummary(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Summary is empty")
		}
	}()
	Group(GroupConfig{Name: "g"}, Item{Content: render.Text("y")})
}

func TestItemRequiresContent(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Content is empty")
		}
	}()
	Group(GroupConfig{Name: "g"}, Item{Summary: "x"})
}

func TestGroupAppliesNameToEveryItem(t *testing.T) {
	h := Group(GroupConfig{Name: "faq"},
		Item{Summary: "Q1", Content: render.Text("A1")},
		Item{Summary: "Q2", Content: render.Text("A2")},
	)
	got := string(h)
	if strings.Count(got, `name="faq"`) != 2 {
		t.Fatalf("expected name=\"faq\" twice (once per item), got %d in %s",
			strings.Count(got, `name="faq"`), got)
	}
	mustContain(t, h, "<details")
	mustContain(t, h, "<summary")
	mustContain(t, h, "Q1")
	mustContain(t, h, "Q2")
	mustContain(t, h, "A1")
	mustContain(t, h, "A2")
	mustContain(t, h, `class="accordion accordion-group"`)
}

func TestStackOmitsName(t *testing.T) {
	h := Stack(StackConfig{},
		Item{Summary: "Section A", Content: render.Text("body")},
		Item{Summary: "Section B", Content: render.Text("body")},
	)
	mustNotContain(t, h, `name=`)
	mustContain(t, h, `class="accordion accordion-stack"`)
}

func TestItemOpenAttribute(t *testing.T) {
	openH := Group(GroupConfig{Name: "g"},
		Item{Summary: "X", Content: render.Text("body"), Open: true},
	)
	mustContain(t, openH, `open=""`)

	closedH := Group(GroupConfig{Name: "g"},
		Item{Summary: "X", Content: render.Text("body"), Open: false},
	)
	mustNotContain(t, closedH, `open=`)
}

func TestStackPreservesItemIDAndClass(t *testing.T) {
	h := Stack(StackConfig{},
		Item{Summary: "X", Content: render.Text("body"), ID: "panel-1", Class: "extra"},
	)
	mustContain(t, h, `id="panel-1"`)
	mustContain(t, h, "accordion-item extra")
}

func TestGroupAriaLabel(t *testing.T) {
	h := Group(GroupConfig{Name: "g", AriaLabel: "Frequently asked questions"},
		Item{Summary: "X", Content: render.Text("body")},
	)
	mustContain(t, h, `aria-label="Frequently asked questions"`)
	mustContain(t, h, `role="group"`)
}

func TestSummaryStructureIncludesMarker(t *testing.T) {
	h := Stack(StackConfig{},
		Item{Summary: "Click me", Content: render.Text("body")},
	)
	mustContain(t, h, "accordion-summary")
	mustContain(t, h, "accordion-label")
	mustContain(t, h, "accordion-marker")
	mustContain(t, h, `aria-hidden="true"`)
}

func TestEscapesUserContent(t *testing.T) {
	h := Stack(StackConfig{},
		Item{Summary: "<script>alert(1)</script>", Content: render.Text("safe")},
	)
	mustNotContain(t, h, "<script>alert(1)</script>")
	mustContain(t, h, "&lt;script&gt;")
}

func TestBaseCSSContainsModernFeatures(t *testing.T) {
	css := BaseCSS()
	for _, must := range []string{
		"interpolate-size",
		"::details-content",
		"transition-behavior",
		"allow-discrete",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(css, must) {
			t.Errorf("BaseCSS missing %q", must)
		}
	}
}
