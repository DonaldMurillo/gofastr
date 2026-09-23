package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestCommandPaletteTrigger(t *testing.T) {
	trigger, _ := CommandPalette(CommandPaletteConfig{RPCPath: "/commands/search", FallbackHref: "/search"})
	out := string(trigger)
	wants := []string{
		`data-fui-open="command-palette"`,
		`data-hui-shortcut-click="Meta+K"`,
		`aria-label="Open command palette"`,
		`class="fui-visually-hidden"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("CommandPalette trigger missing %q\nout: %s", w, out)
		}
	}
}

func TestCommandPaletteCustomShortcut(t *testing.T) {
	trigger, _ := CommandPalette(CommandPaletteConfig{
		RPCPath:      "/cmds",
		Shortcut:     "Ctrl+/",
		FallbackHref: "/search",
	})
	if !strings.Contains(string(trigger), `data-hui-shortcut-click="Ctrl+/"`) {
		t.Errorf("expected custom shortcut, got: %s", trigger)
	}
}

func TestCommandPaletteSlotRendersCombobox(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{
		Name:         "cp",
		RPCPath:      "/commands/search",
		Placeholder:  "Search…",
		DebounceMs:   100,
		FallbackHref: "/search",
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
		RPCPath:      "/search",
		ExtraAttrs:   map[string]string{"data-test": "hook"},
		FallbackHref: "/search",
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
	_, b := CommandPalette(CommandPaletteConfig{Name: "cp", RPCPath: "/commands/search", FallbackHref: "/search"})
	h := string(b.Definition().Slots[0].Component.Render())

	for _, w := range []string{
		`data-fui-action="close"`,
		`aria-label="Close"`,
		`class="fui-cmd-palette__close"`,
		`type="button"`,
		`class="fui-icon fui-cmd-palette__close-icon"`,
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
	// The marker is a class value, so the tag it belongs to starts at the
	// nearest "<" BEFORE it and ends at the next ">". Both bounds matter:
	//
	//   - slicing from the document start (h[:i+...]) drags in every earlier
	//     element, which makes the hints assertion vacuous — it passes the
	//     moment anything earlier carries aria-hidden.
	//   - slicing forward from the marker misses attributes sorted ahead of
	//     class, and serializeExtraAttrs sorts, so aria-hidden lands before
	//     it. That direction reports the attribute missing when it is there.
	//
	// Both mistakes were made here in turn; this reads the whole tag.
	openTag := func(marker string) string {
		i := strings.Index(h, marker)
		if i == -1 {
			t.Fatalf("marker %q not found in body:\n%s", marker, h)
		}
		start := strings.LastIndex(h[:i], "<")
		end := strings.Index(h[i:], ">")
		if start == -1 || end == -1 {
			t.Fatalf("marker %q is not inside a tag:\n%s", marker, h)
		}
		return h[start : i+end+1]
	}
	if foot := openTag(`fui-cmd-palette__footer`); strings.Contains(foot, "aria-hidden") {
		t.Errorf("footer must not be aria-hidden (it hosts the close button):\n%s", foot)
	}
	if hints := openTag(`fui-cmd-palette__hints`); !strings.Contains(hints, "aria-hidden") {
		t.Errorf("hints row must stay decorative (aria-hidden) now that the footer is exposed:\n%s", hints)
	}
}

func TestCommandPaletteFallbackHrefRefusals(t *testing.T) {
	for name, href := range map[string]string{
		"#": "#",
		// `/\evil.example` starts with / and so reads same-origin to a
		// prefix check; the URL parser normalises the backslash to a
		// slash and the trigger navigates cross-origin.
		"backslash after the leading slash": `/\evil.example`,
		"backslash anywhere":                `/x\evil.example`,
		"cross-origin":                      "//evil.example/x",
		"scheme":                            "javascript:alert(1)",
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s FallbackHref should have been refused", name)
				}
			}()
			CommandPalette(CommandPaletteConfig{RPCPath: "/s", FallbackHref: href})
		}()
	}
}

func TestCommandPaletteCSSPadsTheInputRow(t *testing.T) {
	css := commandPaletteCSS(style.Theme{})
	// Item 22's contract: the row that directly wraps the input (the
	// carrier — the no-script FORM wears the same combobox class, so
	// the sheet selects by :has) carries the padding and the seam, and
	// the input keeps its touch-target height. The retired rules
	// targeted .combobox__* classes nothing renders and matched nothing.
	for _, want := range []string{
		"[data-fui-comp=\"ui-cmd-palette\"] .fui-cmd-palette__combobox:has(> .fui-cmd-palette__input) {",
		"padding: var(--spacing-md, 8px);",
		"[data-fui-comp=\"ui-cmd-palette\"] .fui-cmd-palette__input {",
		"min-block-size: var(--spacing-touch-target, 44px);",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("commandPaletteCSS lost %q — the input row lost its chrome:\n%s", want, css)
		}
	}
	if strings.Contains(css, ".combobox__") {
		t.Errorf("the palette sheet still targets retired .combobox__* classes nothing renders:\n%s", css)
	}
}
