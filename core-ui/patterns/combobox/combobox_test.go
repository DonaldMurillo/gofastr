package combobox

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

type stubTheme = style.Theme

func TestRenderShape(t *testing.T) {
	out := string(Render(Config{
		ID:          "city",
		Name:        "city",
		Label:       "City",
		RPCPath:     "/cities/search",
		SignalName:  "city-results",
		Placeholder: "Type a city",
	}))
	wants := []string{
		`<label`,
		`for="city"`,
		`>City</label>`,
		`<input`,
		`id="city"`,
		`name="city"`,
		`role="combobox"`,
		`aria-controls="city-listbox"`,
		`aria-autocomplete="list"`,
		`aria-expanded="false"`,
		`aria-activedescendant=""`,
		`autocomplete="off"`,
		`placeholder="Type a city"`,
		`<form`,
		`data-fui-rpc="/cities/search"`,
		`data-fui-rpc-method="POST"`,
		`data-fui-rpc-trigger="input"`,
		`data-fui-rpc-debounce-ms="250"`,
		`data-fui-rpc-signal="city-results"`,
		`id="city-listbox"`,
		`role="listbox"`,
		`aria-label="City suggestions"`,
		`data-fui-signal="city-results"`,
		`data-fui-signal-mode="html"`,
		`hidden`, // listbox starts hidden
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("Combobox missing %q\nout: %s", w, out)
		}
	}
}

func TestRenderCustomDebounce(t *testing.T) {
	out := string(Render(Config{
		ID: "x", Name: "x", Label: "X",
		RPCPath: "/x", SignalName: "x",
		DebounceMs: 500,
	}))
	if !strings.Contains(out, `data-fui-rpc-debounce-ms="500"`) {
		t.Errorf("expected custom debounce, got: %s", out)
	}
}

func TestRenderInitialOptions(t *testing.T) {
	out := string(Render(Config{
		ID: "x", Name: "x", Label: "X",
		RPCPath: "/x", SignalName: "x",
		EmptyHTML: `<li role="option" id="x-1" data-value="alpha">Alpha</li>`,
	}))
	if !strings.Contains(out, `id="x-1"`) {
		t.Errorf("expected initial option rendered, got: %s", out)
	}
	// Listbox must NOT be hidden when initial options exist.
	listboxIdx := strings.Index(out, `id="x-listbox"`)
	listboxTag := out[listboxIdx:]
	if i := strings.Index(listboxTag, ">"); i > 0 {
		listboxTag = listboxTag[:i]
	}
	if strings.Contains(listboxTag, "hidden") {
		t.Errorf("listbox should not be hidden when EmptyHTML provides options, got: %s", listboxTag)
	}
}

func TestRenderHiddenLabel(t *testing.T) {
	out := string(Render(Config{
		ID: "x", Name: "x", Label: "Search",
		RPCPath: "/x", SignalName: "x",
		LabelHidden: true,
	}))
	if !strings.Contains(out, "ui-visually-hidden") {
		t.Errorf("expected label visually hidden, got: %s", out)
	}
}

func TestRenderPanics(t *testing.T) {
	cases := []Config{
		{Name: "x", Label: "X", RPCPath: "/x", SignalName: "x"},
		{ID: "x", Label: "X", RPCPath: "/x", SignalName: "x"},
		{ID: "x", Name: "x", RPCPath: "/x", SignalName: "x"},
		{ID: "x", Name: "x", Label: "X", SignalName: "x"},
		{ID: "x", Name: "x", Label: "X", RPCPath: "/x"},
	}
	for i, c := range cases {
		t.Run("missing-"+string(rune('A'+i)), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			Render(c)
		})
	}
}

func TestStyleScopedToComponent(t *testing.T) {
	css := styleFn(stubTheme{})
	for _, w := range []string{
		`[data-fui-comp="combobox"]`,
		`combobox__listbox`,
		`is-active`,
		`aria-disabled="true"`,
		`pointer: coarse`,
	} {
		if !strings.Contains(css, w) {
			t.Errorf("styleFn missing %q", w)
		}
	}
}

func TestRenderEmitsDataFuiComp(t *testing.T) {
	out := string(Render(Config{
		ID: "x", Name: "x", Label: "X",
		RPCPath: "/x", SignalName: "x",
	}))
	if !strings.Contains(out, `data-fui-comp="combobox"`) {
		t.Errorf("Render must emit data-fui-comp for auto-load, got: %s", out)
	}
}
