package ui

import (
	"strings"
	"testing"
)

func TestGlobalSearchRequiresAllCoreFields(t *testing.T) {
	cases := []GlobalSearchConfig{
		{Name: "q", Label: "Search", RPCPath: "/s", SignalName: "search"}, // no ID
		{ID: "s", Label: "Search", RPCPath: "/s", SignalName: "search"},   // no Name
		{ID: "s", Name: "q", RPCPath: "/s", SignalName: "search"},          // no Label
		{ID: "s", Name: "q", Label: "Search", SignalName: "search"},        // no RPCPath
		{ID: "s", Name: "q", Label: "Search", RPCPath: "/s"},               // no SignalName
	}
	for i, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d should panic but didn't", i)
				}
			}()
			GlobalSearch(c)
		}()
	}
}

func TestGlobalSearchEmitsCombobox(t *testing.T) {
	h := string(GlobalSearch(GlobalSearchConfig{
		ID: "global-search", Name: "q",
		Label: "Search the site", RPCPath: "/api/search",
		SignalName: "search-results",
	}))
	if !strings.Contains(h, `id="global-search"`) {
		t.Errorf("expected the combobox input id to surface:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-rpc="/api/search"`) {
		t.Errorf("combobox should fire RPC to RPCPath:\n%s", h)
	}
}

func TestGlobalSearchShortcutDefaultsToSlash(t *testing.T) {
	h := string(GlobalSearch(GlobalSearchConfig{
		ID: "s", Name: "q", Label: "Search", RPCPath: "/s", SignalName: "x",
	}))
	if !strings.Contains(h, `data-fui-shortcut-focus="/"`) {
		t.Errorf("default Shortcut should be '/':\n%s", h)
	}
	if !strings.Contains(h, `data-fui-shortcut-target="#s"`) {
		t.Errorf("wrapper should target the inner input by id selector:\n%s", h)
	}
}

func TestGlobalSearchShortcutDisabled(t *testing.T) {
	// Pass a sentinel space to disable.
	h := string(GlobalSearch(GlobalSearchConfig{
		ID: "s", Name: "q", Label: "Search", RPCPath: "/s", SignalName: "x",
		Shortcut: " ",
	}))
	if strings.Contains(h, "data-fui-shortcut-focus") {
		t.Errorf("Shortcut=\" \" should disable the focus binding:\n%s", h)
	}
}

func TestGlobalSearchShortcutChordCustom(t *testing.T) {
	h := string(GlobalSearch(GlobalSearchConfig{
		ID: "s", Name: "q", Label: "Search", RPCPath: "/s", SignalName: "x",
		Shortcut: "k",
	}))
	if !strings.Contains(h, `data-fui-shortcut-focus="k"`) {
		t.Errorf("Shortcut=k should bind to the k chord:\n%s", h)
	}
}

func TestGlobalSearchStickyClass(t *testing.T) {
	on := string(GlobalSearch(GlobalSearchConfig{
		ID: "s", Name: "q", Label: "Search", RPCPath: "/s", SignalName: "x",
		Sticky: true,
	}))
	if !strings.Contains(on, "ui-global-search--sticky") {
		t.Errorf("Sticky=true should add modifier class:\n%s", on)
	}
}
