package headless

import (
	"strings"
	"testing"
)

func renderCombobox(p ComboboxProps) string { return string(Combobox(p, nil)) }

func TestComboboxRendersTheARIAContract(t *testing.T) {
	h := renderCombobox(ComboboxProps{ID: "q", Name: "q", Label: "Search", Options: []ComboboxOption{
		{Label: "Docs", Meta: "/docs"},
		{Label: "Examples", Href: "/examples"},
		{Value: "ex", Label: "Example row"},
	}})
	for _, want := range []string{
		`<input aria-autocomplete="list" aria-controls="q-listbox" aria-expanded="false" autocomplete="off" data-hui-combobox-input="" id="q" name="q" role="combobox"`,
		`<ul aria-label="Search results" data-hui-combobox-count="{n} results" data-hui-combobox-listbox="" data-hui-combobox-static="" hidden="" id="q-listbox" role="listbox">`,
		`<li data-value="Docs" id="q-listbox-opt-0" role="option"><span>Docs</span><span>/docs</span></li>`,
		`<li data-fui-push-state="/examples" data-value="Examples"`,
		`<li data-value="ex" id="q-listbox-opt-2" role="option">`,
		`<span data-hui-combobox-no-results="No matches" data-hui-combobox-status="" role="status">`,
		`<label for="q">Search</label>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("combobox missing %q:\n%s", want, h)
		}
	}
	// The input keeps focus; the listbox is linked but never a stop.
	if strings.Contains(h, `tabindex`+"=") {
		if !strings.Contains(h, `tabindex="-1"`) {
			t.Errorf("the listbox or its rows joined the tab order:\n%s", h)
		}
	}
}

func TestComboboxIslandRendersRPCAndNoScriptForm(t *testing.T) {
	h := renderCombobox(ComboboxProps{ID: "sq", Name: "q", Label: "Search",
		Island:         &Island{Endpoint: "/island/search", Signal: "search"},
		NoScriptAction: "/search"})
	for _, want := range []string{
		`<form action="/search" method="GET" role="none">`,
		`data-fui-rpc="/island/search" data-fui-rpc-debounce-ms="250" data-fui-rpc-method="POST" data-fui-rpc-signal="search" data-fui-rpc-trigger="input" data-hui-combobox-loader="" data-hui-combobox-loading="Loading…"`,
		`data-fui-signal="search" data-fui-signal-mode="html" data-hui-combobox-count="{n} results" data-hui-combobox-listbox=""`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("island combobox missing %q:\n%s", want, h)
		}
	}
	// A custom debounce travels; a custom action URL that is
	// cross-origin is refused at render.
	fast := renderCombobox(ComboboxProps{ID: "sq", Name: "q", Label: "S",
		Island: &Island{Endpoint: "/i", Signal: "s"}, NoScriptAction: "/s", DebounceMS: 50})
	if !strings.Contains(fast, `data-fui-rpc-debounce-ms="50"`) {
		t.Errorf("the custom debounce never travelled:\n%s", fast)
	}
}

func TestComboboxUnsafeOptionHrefDropsNavAffordance(t *testing.T) {
	h := renderCombobox(ComboboxProps{ID: "q", Name: "q", Label: "S", Options: []ComboboxOption{
		{Label: "Evil", Href: "javascript:alert(1)"},
	}})
	if strings.Contains(h, "data-fui-push-state") {
		t.Errorf("an unsafe option href kept its navigation affordance:\n%s", h)
	}
	if !strings.Contains(h, `data-value="Evil"`) {
		t.Errorf("the option itself must survive (the pick still fills the input):\n%s", h)
	}
}

func TestComboboxRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    ComboboxProps
	}{
		{"no id", ComboboxProps{Name: "q", Label: "S", Options: []ComboboxOption{{Label: "a"}}}},
		{"no name", ComboboxProps{ID: "q", Label: "S", Options: []ComboboxOption{{Label: "a"}}}},
		{"whitespace-only label", ComboboxProps{ID: "q", Name: "q", Label: "   ", Options: []ComboboxOption{{Label: "a"}}}},
		{"no island and no options", ComboboxProps{ID: "q", Name: "q", Label: "S"}},
		{"island without no-script action", ComboboxProps{ID: "q", Name: "q", Label: "S",
			Island: &Island{Endpoint: "/i", Signal: "s"}}},
		{"island with # action", ComboboxProps{ID: "q", Name: "q", Label: "S",
			Island: &Island{Endpoint: "/i", Signal: "s"}, NoScriptAction: "#"}},
		{"cross-origin action", ComboboxProps{ID: "q", Name: "q", Label: "S",
			Island: &Island{Endpoint: "/i", Signal: "s"}, NoScriptAction: "//evil.example/x"}},
		{"partial island", ComboboxProps{ID: "q", Name: "q", Label: "S",
			Island: &Island{Endpoint: "/i"}, NoScriptAction: "/s"}},
		{"negative debounce", ComboboxProps{ID: "q", Name: "q", Label: "S", DebounceMS: -1,
			Options: []ComboboxOption{{Label: "a"}}}},
		{"duplicate option ids", ComboboxProps{ID: "q", Name: "q", Label: "S", Options: []ComboboxOption{
			{ID: "dup", Label: "a"}, {ID: "dup", Label: "b"},
		}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Combobox(tc.p, nil)
		}()
	}
}

func TestComboboxScrubbedCarriedStrings(t *testing.T) {
	h := renderCombobox(ComboboxProps{ID: "q", Name: "q", Label: "Sea\r\nrch", Placeholder: "Find\r\ndocs",
		Options: []ComboboxOption{{Label: "Do\r\ncs"}}})
	for _, want := range []string{
		`<label for="q">Search</label>`,
		`placeholder="Finddocs"`,
		`<span>Docs</span>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("carried text was not scrubbed into %q:\n%s", want, h)
		}
	}
}
