package headless

import (
	"strings"
	"testing"
)

func renderMultiSelect(p MultiSelectProps) string { return string(MultiSelect(p, nil)) }

func TestMultiSelectRendersTheCheckboxGroup(t *testing.T) {
	h := renderMultiSelect(MultiSelectProps{
		Name: "langs", Label: "Pick languages", ID: "demo",
		Options: []MultiSelectOption{
			{Value: "go", Label: "Go", Selected: true},
			{Value: "cpp", Label: "C++"},
			{Value: "csharp", Label: "C Sharp", Disabled: true},
		},
	})
	for _, want := range []string{
		`data-hui-multiselect=""`,
		`data-hui-multiselect-chips=""`,
		`aria-live="polite"`,
		`data-hui-multiselect-placeholder="Choose…"`,
		`data-hui-multiselect-remove-label="Remove {label}"`,
		`data-hui-disclosure=""`,
		`<summary>Pick languages</summary>`,
		`role="group"`,
		`aria-label="Pick languages"`,
		`type="checkbox"`,
		`name="langs"`,
		`value="go"`,
		`id="demo-opt-0"`,
		`for="demo-opt-0"`,
		`checked`,
		`disabled`,
		`>C++<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("multiselect missing %q:\n%s", want, h)
		}
	}
	// Every option shares the field name: the plain form contract.
	if n := strings.Count(h, `name="langs"`); n != 3 {
		t.Errorf("found %d name=langs attributes, want 3 (one per checkbox):\n%s", n, h)
	}
	if n := strings.Count(h, `type="checkbox"`); n != 3 {
		t.Errorf("rendered %d checkboxes, want 3:\n%s", n, h)
	}
	// The chips strip starts empty: the module fills it, never the
	// server guessing what script will do.
	if strings.Contains(h, "data-hui-multiselect-chip=") {
		t.Errorf("the server must not pre-render chips:\n%s", h)
	}
}

func TestMultiSelectSaysItsWordsThroughStrings(t *testing.T) {
	h := renderMultiSelect(MultiSelectProps{
		Name: "x", Label: "Choisir",
		Options: []MultiSelectOption{{Value: "a", Label: "A"}},
		Strings: &Strings{
			MultiSelectPlaceholder: "Aucun choisi",
			MultiSelectRemoveLabel: "Retirer {label}",
		},
	})
	if !strings.Contains(h, `data-hui-multiselect-placeholder="Aucun choisi"`) {
		t.Errorf("the translated placeholder never arrived:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-multiselect-remove-label="Retirer {label}"`) {
		t.Errorf("the translated remove label never arrived:\n%s", h)
	}
}

func TestMultiSelectPlaceholderPropWins(t *testing.T) {
	h := renderMultiSelect(MultiSelectProps{
		Name: "x", Label: "L", Placeholder: "No languages selected",
		Options: []MultiSelectOption{{Value: "a", Label: "A"}},
	})
	if !strings.Contains(h, `data-hui-multiselect-placeholder="No languages selected"`) {
		t.Errorf("the caller's placeholder should win:\n%s", h)
	}
}

func TestMultiSelectOpenAttr(t *testing.T) {
	on := renderMultiSelect(MultiSelectProps{Name: "x", Label: "L", Open: true,
		Options: []MultiSelectOption{{Value: "a", Label: "A"}}})
	if !strings.Contains(on, `open=""`) {
		t.Errorf("Open should render the details open:\n%s", on)
	}
	off := renderMultiSelect(MultiSelectProps{Name: "x", Label: "L",
		Options: []MultiSelectOption{{Value: "a", Label: "A"}}})
	if strings.Contains(off, `open=""`) {
		t.Errorf("closed by default:\n%s", off)
	}
}

func TestMultiSelectIDScopeAndFallback(t *testing.T) {
	h := renderMultiSelect(MultiSelectProps{
		Name: "langs", Label: "L",
		Options: []MultiSelectOption{
			{Value: "C++", Label: "C plus plus"},
			{Value: "C#", Label: "C sharp"},
		},
	})
	// Symbol-heavy values get index-scoped ids, so "C++" and "C#"
	// cannot collide.
	if !strings.Contains(h, `id="langs-opt-0"`) || !strings.Contains(h, `id="langs-opt-1"`) {
		t.Errorf("option ids should fall back to the field name and the index:\n%s", h)
	}
}

func TestMultiSelectScrubsCarriedLabels(t *testing.T) {
	h := renderMultiSelect(MultiSelectProps{
		Name: "x", Label: "L",
		Options: []MultiSelectOption{{Value: "a", Label: "Ev\r\nil<script>"}},
	})
	if strings.ContainsAny(h, "\r\n") {
		t.Errorf("control bytes reached the DOM:\n%q", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("markup reached the DOM raw:\n%s", h)
	}
}

// TestMultiSelectHostileValueAndPlaceholderAreScrubbed: the checkbox
// value is data the form submits back and the placeholder may derive
// from a stored value; both land escaped in their attributes with
// control bytes scrubbed, the same posture as the labels.
func TestMultiSelectHostileValueAndPlaceholderAreScrubbed(t *testing.T) {
	h := renderMultiSelect(MultiSelectProps{
		Name: "x", Label: "L", Placeholder: "Pi\x00ck \"none\"",
		Options: []MultiSelectOption{{Value: "ev\r\nil\"<script>", Label: "A"}},
	})
	if strings.ContainsAny(h, "\r\n\x00") {
		t.Errorf("control bytes reached the DOM:\n%q", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("markup reached the DOM raw:\n%s", h)
	}
	if !strings.Contains(h, `value="evil&quot;&lt;script&gt;"`) {
		t.Errorf("the hostile value should render escaped, not refused:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-multiselect-placeholder="Pick &quot;none&quot;"`) {
		t.Errorf("the hostile placeholder should render escaped, not refused:\n%s", h)
	}
}

func TestMultiSelectRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"no name", func() {
			MultiSelect(MultiSelectProps{Label: "L", Options: []MultiSelectOption{{Value: "a", Label: "A"}}}, nil)
		}},
		{"blank label", func() {
			MultiSelect(MultiSelectProps{Name: "x", Options: []MultiSelectOption{{Value: "a", Label: "A"}}}, nil)
		}},
		{"no options", func() {
			MultiSelect(MultiSelectProps{Name: "x", Label: "L"}, nil)
		}},
		{"empty value", func() {
			MultiSelect(MultiSelectProps{Name: "x", Label: "L", Options: []MultiSelectOption{{Value: "", Label: "A"}}}, nil)
		}},
		{"blank option label", func() {
			MultiSelect(MultiSelectProps{Name: "x", Label: "L", Options: []MultiSelectOption{{Value: "a", Label: " "}}}, nil)
		}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s should refuse at render", c.name)
				}
			}()
			c.call()
		}()
	}
}
