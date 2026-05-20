package tabs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestRequiresName(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Name empty")
		}
	}()
	New(Config{}, Tab{Label: "x", Content: render.Text("y")})
}

func TestRequiresAtLeastOneTab(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with no tabs")
		}
	}()
	New(Config{Name: "g"})
}

func TestTabRequiresLabel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with empty Label")
		}
	}()
	New(Config{Name: "g"}, Tab{Content: render.Text("x")})
}

func TestEveryTabHasNameAttr(t *testing.T) {
	h := string(New(Config{Name: "main"},
		Tab{Label: "A", Content: render.Text("a")},
		Tab{Label: "B", Content: render.Text("b")},
		Tab{Label: "C", Content: render.Text("c")},
	))
	if strings.Count(h, `name="main"`) != 3 {
		t.Errorf("expected name=main thrice, got %d in: %s",
			strings.Count(h, `name="main"`), h)
	}
}

func TestFirstTabDefaultsOpenWhenNoneSet(t *testing.T) {
	h := string(New(Config{Name: "g"},
		Tab{Label: "A", Content: render.Text("a")},
		Tab{Label: "B", Content: render.Text("b")},
	))
	if strings.Count(h, `open=""`) != 1 {
		t.Errorf("expected exactly one open tab, got %d in: %s",
			strings.Count(h, `open=""`), h)
	}
}

func TestExplicitOpenRespected(t *testing.T) {
	h := string(New(Config{Name: "g"},
		Tab{Label: "A", Content: render.Text("a")},
		Tab{Label: "B", Content: render.Text("b"), Open: true},
		Tab{Label: "C", Content: render.Text("c")},
	))
	idxA := strings.Index(h, `>A<`)
	idxB := strings.Index(h, `>B<`)
	if !strings.Contains(h, `open=""`) {
		t.Errorf("expected open tab, got: %s", h)
	}
	// B should be the open one — find which <details> contains open=""
	// Roughly: the open="" should be closer to B than A.
	openIdx := strings.Index(h, `open=""`)
	if openIdx < idxA || openIdx > idxB {
		t.Errorf("expected open on second tab, idxA=%d openIdx=%d idxB=%d", idxA, openIdx, idxB)
	}
}

func TestAriaLabel(t *testing.T) {
	h := string(New(Config{Name: "g", Label: "Settings tabs"},
		Tab{Label: "x", Content: render.Text("y")},
	))
	if !strings.Contains(h, `aria-label="Settings tabs"`) {
		t.Errorf("expected aria-label, got: %s", h)
	}
}

func TestStructure(t *testing.T) {
	h := string(New(Config{Name: "g"},
		Tab{Label: "x", Content: render.Text("y")},
	))
	for _, want := range []string{
		`class="tabs"`, `tabs-tab`, `tabs-summary`, `tabs-panel`,
		`<details`, `<summary`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestBaseCSSContainsKeySelectors(t *testing.T) {
	css := Style.Entry().CSSFor(style.Theme{})
	for _, want := range []string{
		".tabs", ".tabs-summary", "details[open]",
		".tabs-panel", ".tabs-panels", ":has(",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("BaseCSS missing %q", want)
		}
	}
}
