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

func TestCommandPaletteExtraAttrsOnRoot(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{
		RPCPath:    "/search",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	h := b.Definition().Slots[0].Component.Render()
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("palette root missing data-test:\n%s", root)
	}
}

// TestCommandPaletteCloseControl pins the visible close affordance
// (#325): a real <button> wired through the framework's declarative
// widget-dismiss hook (data-fui-action="close", the same wiring the
// section-menu drawer uses), named for assistive tech, decorative
// icon — and not swallowed by an aria-hidden footer.
func TestCommandPaletteCloseControl(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{Name: "cp", RPCPath: "/commands/search"})
	h := string(b.Definition().Slots[0].Component.Render())

	for _, w := range []string{
		`data-fui-action="close"`,
		`aria-label="Close"`,
		`class="ui-cmd-palette__close"`,
		`type="button"`,
		`ui-icon ui-cmd-palette__close-icon`,
	} {
		if !strings.Contains(h, w) {
			t.Errorf("close control missing %q\nbody: %s", w, h)
		}
	}
	if n := strings.Count(h, `data-fui-action="close"`); n != 1 {
		t.Errorf("expected exactly one dismiss hook, found %d\nbody: %s", n, h)
	}

	// The footer hosts the close button now, so it must stay in the
	// accessibility tree; the decorative kbd hints carry aria-hidden
	// on their own row instead.
	openTag := func(marker string) string {
		i := strings.Index(h, marker)
		if i == -1 {
			t.Fatalf("marker %q not found in body:\n%s", marker, h)
		}
		return h[:i+strings.Index(h[i:], ">")+1]
	}
	if foot := openTag(`ui-cmd-palette__footer`); strings.Contains(foot, "aria-hidden") {
		t.Errorf("footer must not be aria-hidden (it hosts the close button):\n%s", foot)
	}
	if hints := openTag(`ui-cmd-palette__hints`); !strings.Contains(hints, "aria-hidden") {
		t.Errorf("hints row must stay decorative (aria-hidden) now that the footer is exposed:\n%s", hints)
	}
}
