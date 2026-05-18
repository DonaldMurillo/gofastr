package ui

import (
	"strings"
	"testing"
)

func TestCommandPaletteTrigger(t *testing.T) {
	trigger, _ := CommandPalette(CommandPaletteConfig{RPCPath: "/commands/search"})
	out := string(trigger)
	wants := []string{
		`data-fui-open="command-palette"`,
		`data-fui-shortcut-click="Meta+K"`,
		`aria-label="Open command palette"`,
		`class="ui-visually-hidden"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("CommandPalette trigger missing %q\nout: %s", w, out)
		}
	}
}

func TestCommandPaletteCustomShortcut(t *testing.T) {
	trigger, _ := CommandPalette(CommandPaletteConfig{
		RPCPath:  "/cmds",
		Shortcut: "Ctrl+/",
	})
	if !strings.Contains(string(trigger), `data-fui-shortcut-click="Ctrl+/"`) {
		t.Errorf("expected custom shortcut, got: %s", trigger)
	}
}

func TestCommandPaletteSlotRendersCombobox(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{
		Name:        "cp",
		RPCPath:     "/commands/search",
		Placeholder: "Search…",
		DebounceMs:  100,
	})
	d := b.Definition()
	if d.Role != "dialog" {
		t.Errorf("expected Role=dialog, got %q", d.Role)
	}
	if d.LabelledBy != "cp-title" {
		t.Errorf("expected LabelledBy=cp-title, got %q", d.LabelledBy)
	}
	if !d.Hidden {
		t.Error("expected Hidden")
	}
	if !d.Backdrop {
		t.Error("expected Backdrop (Modal preset)")
	}
	body := string(d.Slots[0].Component.Render())
	wants := []string{
		`role="combobox"`,
		`role="listbox"`,
		`id="cp-input"`,
		`id="cp-input-listbox"`,
		`data-fui-rpc="/commands/search"`,
		`data-fui-rpc-debounce-ms="100"`,
		`data-fui-rpc-signal="cp-results"`,
		`placeholder="Search…"`,
		`>Command palette</h2>`,
		`>Navigate<`,
		`>Select<`,
		`>Close<`,
		`>↑↓<`,
		`>↵<`,
		`>Esc<`,
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("CommandPalette body missing %q\nbody: %s", w, body)
		}
	}
}

func TestCommandPalettePanicsWithoutRPC(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic")
		}
	}()
	CommandPalette(CommandPaletteConfig{})
}
