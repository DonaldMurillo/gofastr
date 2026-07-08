package multiselect

import (
	"regexp"
	"strings"
	"testing"
)

func TestRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MultiSelect without Name should panic")
		}
	}()
	Render(Config{Label: "x", Options: []Option{{Value: "a", Label: "A"}}})
}

func TestRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MultiSelect without Label should panic")
		}
	}()
	Render(Config{Name: "x", Options: []Option{{Value: "a", Label: "A"}}})
}

func TestRequiresAtLeastOneOption(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MultiSelect without Options should panic")
		}
	}()
	Render(Config{Name: "x", Label: "x"})
}

func TestEmitsCheckboxPerOption(t *testing.T) {
	h := string(Render(Config{
		Name: "langs", Label: "Languages",
		Options: []Option{
			{Value: "go", Label: "Go"},
			{Value: "rust", Label: "Rust"},
			{Value: "ts", Label: "TypeScript"},
		},
	}))
	if c := strings.Count(h, `type="checkbox"`); c != 3 {
		t.Errorf("expected 3 checkboxes, got %d:\n%s", c, h)
	}
	if c := strings.Count(h, ` name="langs"`); c != 3 {
		t.Errorf("expected all checkboxes to share name=langs, got %d:\n%s", c, h)
	}
}

func TestSelectedOptionsRenderChecked(t *testing.T) {
	h := string(Render(Config{
		Name: "x", Label: "x",
		Options: []Option{
			{Value: "a", Label: "A", Selected: true},
			{Value: "b", Label: "B"},
			{Value: "c", Label: "C", Selected: true},
		},
	}))
	if c := strings.Count(h, " checked"); c != 2 {
		t.Errorf("expected 2 checked checkboxes, got %d:\n%s", c, h)
	}
}

func TestChipsContainerEmitted(t *testing.T) {
	h := string(Render(Config{
		Name: "x", Label: "x", Placeholder: "Pick…",
		Options: []Option{{Value: "a", Label: "A"}},
	}))
	if !strings.Contains(h, "data-fui-multiselect-chips") {
		t.Errorf("chips container should carry data-fui-multiselect-chips:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-multiselect-placeholder="Pick…"`) {
		t.Errorf("placeholder should be on the chips element:\n%s", h)
	}
	if !strings.Contains(h, `aria-live="polite"`) {
		t.Errorf("chips container should be aria-live=polite:\n%s", h)
	}
}

func TestOpenAttrEmittedWhenOpen(t *testing.T) {
	on := string(Render(Config{
		Name: "x", Label: "x", Open: true,
		Options: []Option{{Value: "a", Label: "A"}},
	}))
	if !strings.Contains(on, "<details") {
		t.Errorf("expected <details> wrapper:\n%s", on)
	}
	// Walk the <details> attrs only.
	if !strings.Contains(on, "open") {
		t.Errorf("Open=true should emit the open attribute on the disclosure:\n%s", on)
	}
}

func TestDisclosureCarriesMultiselectMarker(t *testing.T) {
	h := string(Render(Config{
		Name: "x", Label: "x",
		Options: []Option{{Value: "a", Label: "A"}},
	}))
	if !strings.Contains(h, `data-fui-multiselect="true"`) {
		t.Errorf("disclosure should carry data-fui-multiselect=true:\n%s", h)
	}
}

func TestOptionRequiresValueAndLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Option without Value+Label should panic")
		}
	}()
	Render(Config{
		Name: "x", Label: "x",
		Options: []Option{{Value: "", Label: ""}},
	})
}

func TestLabelForMatchesInputID(t *testing.T) {
	h := string(Render(Config{
		Name: "langs", Label: "Languages",
		Options: []Option{{Value: "go", Label: "Go"}},
	}))
	// The runtime resolves chip text via label[for="<checkbox id>"] —
	// the row label must carry a for= matching the input id, or chips
	// degrade to showing the raw option Value.
	m := regexp.MustCompile(`<input[^>]* id="([^"]+)"`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no checkbox id found:\n%s", h)
	}
	if !strings.Contains(h, `for="`+m[1]+`"`) {
		t.Errorf("row label must reference the checkbox via for=%q:\n%s", m[1], h)
	}
}

func TestSymbolValuesGetUniqueIDs(t *testing.T) {
	h := string(Render(Config{
		Name: "langs", Label: "Languages",
		Options: []Option{
			{Value: "C++", Label: "C++"},
			{Value: "C#", Label: "C Sharp"},
		},
	}))
	ids := regexp.MustCompile(`<input[^>]* id="([^"]+)"`).FindAllStringSubmatch(h, -1)
	if len(ids) != 2 {
		t.Fatalf("expected 2 checkbox ids, got %d:\n%s", len(ids), h)
	}
	if ids[0][1] == ids[1][1] {
		t.Errorf("symbol-only values must not collide: both got id %q", ids[0][1])
	}
}

func TestOptionIDsPrefixedWithInstanceID(t *testing.T) {
	h := string(Render(Config{
		ID: "ms-a", Name: "langs", Label: "Languages",
		Options: []Option{{Value: "go", Label: "Go"}},
	}))
	m := regexp.MustCompile(`<input[^>]* id="([^"]+)"`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no checkbox id found:\n%s", h)
	}
	if !strings.HasPrefix(m[1], "ms-a-opt-") {
		t.Errorf("option ids must be scoped by the instance ID, got %q", m[1])
	}
}
