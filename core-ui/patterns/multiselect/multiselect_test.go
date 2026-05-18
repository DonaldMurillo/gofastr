package multiselect

import (
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
