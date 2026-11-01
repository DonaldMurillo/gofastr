package headless

import (
	"strings"
	"testing"
)

// A static combobox's NoScriptAction became the wrapping form's action
// with no check at all (the same-origin and "#" refusals ran only when
// Island was set), so a javascript: URL was the form action.
func TestComboboxStaticNoScriptActionIsChecked(t *testing.T) {
	for _, bad := range []string{"javascript:alert(1)", "https://evil.example/s", "//evil.example/s", `/\evil.example/s`, "#"} {
		mustRefuse(t, "a static combobox with NoScriptAction "+bad, func() {
			Combobox(ComboboxProps{ID: "q", Name: "q", Label: "Search",
				NoScriptAction: bad, Options: []ComboboxOption{{Label: "Docs"}}}, nil)
		})
	}
	h := string(Combobox(ComboboxProps{ID: "q", Name: "q", Label: "Search",
		NoScriptAction: "/search", Options: []ComboboxOption{{Label: "Docs"}}}, nil))
	if !strings.Contains(h, `action="/search"`) {
		t.Fatalf("a same-origin static NoScriptAction no longer renders: %s", h)
	}
}

// An option's Meta line rendered as a second PartComboboxOption, so a
// class map's option class (padding, hover, active fill) also landed
// on the meta span nested inside the option. It is PartComboboxMeta.
func TestComboboxMetaIsItsOwnPart(t *testing.T) {
	cls := Classes{PartComboboxOption: "opt", PartComboboxMeta: "meta"}
	for name, h := range map[string]string{
		"Combobox": string(Combobox(ComboboxProps{ID: "q", Name: "q", Label: "Search",
			Options: []ComboboxOption{{Label: "Docs", Meta: "/docs"}}}, cls)),
		"ComboboxRows": string(ComboboxRows("q-listbox", []ComboboxOption{{Label: "Docs", Meta: "/docs"}}, cls)),
	} {
		if n := strings.Count(h, `class="opt"`); n != 1 {
			t.Errorf("%s: the option class lands on %d elements, want 1 (the li):\n%s", name, n, h)
		}
		if !strings.Contains(h, `<span class="meta"`) || !strings.Contains(h, `/docs</span>`) {
			t.Errorf("%s: the meta line does not carry PartComboboxMeta's class:\n%s", name, h)
		}
	}
}
