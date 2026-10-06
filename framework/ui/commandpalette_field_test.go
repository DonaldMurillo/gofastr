package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// The field trigger is a visible link to the fallback that opens the
// palette and binds the shortcut, with keycaps spelled as glyphs.
func TestPaletteFieldTriggerDraws(t *testing.T) {
	trigger, _ := CommandPalette(CommandPaletteConfig{
		Name: "admin-palette", RPCPath: "/admin/_palette", FallbackHref: "/admin/search",
		Trigger: PaletteTriggerField, TriggerText: "Search or jump to…",
	})
	h := string(trigger)
	for _, want := range []string{
		`href="/admin/search"`,
		`data-cui-open="admin-palette"`,
		`data-hui-shortcut-click="Meta+K"`,
		`data-cui-comp="ui-cmd-palette-trigger"`,
		`class="fui-cmd-trigger"`,
		"Search or jump to…",
		"⌘K",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("field trigger lacks %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "fui-visually-hidden") {
		t.Errorf("field trigger is visually hidden:\n%s", h)
	}
}

// The default trigger stays the hidden link.
func TestPaletteDefaultTriggerHidden(t *testing.T) {
	trigger, _ := CommandPalette(CommandPaletteConfig{RPCPath: "/x", FallbackHref: "/docs"})
	if !strings.Contains(string(trigger), `class="fui-visually-hidden"`) {
		t.Fatalf("default trigger is not hidden: %s", trigger)
	}
}

func TestPaletteUnknownTriggerPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unknown Trigger did not panic")
		}
	}()
	CommandPalette(CommandPaletteConfig{RPCPath: "/x", FallbackHref: "/docs", Trigger: "pill"})
}

func TestShortcutGlyphs(t *testing.T) {
	for in, want := range map[string]string{"Meta+K": "⌘K", "Shift+Meta+p": "⇧⌘P", "Ctrl+K": "Ctrl K"} {
		if got := shortcutGlyphs(in); got != want {
			t.Errorf("shortcutGlyphs(%q) = %q, want %q", in, got, want)
		}
	}
}

// Result rows are options the combobox picks: escaped text, a
// navigation only for a safe href, unique ids per palette.
func TestPaletteResultsRows(t *testing.T) {
	h := string(PaletteResults(context.Background(), "admin-palette", []PaletteCommand{
		{Label: "<b>Ada</b>", Href: "/admin/entities/customers/1", Meta: "Customer"},
		{Label: "Evil", Href: "javascript:alert(1)"},
	}, ""))
	for _, want := range []string{
		`role="option"`,
		`id="admin-palette-input-listbox-res-0"`,
		`id="admin-palette-input-listbox-res-1"`,
		`data-cui-push-state="/admin/entities/customers/1"`,
		"&lt;b&gt;Ada&lt;/b&gt;",
		"Customer",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("results lack %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "javascript:") {
		t.Errorf("SECURITY: an unsafe href reached a row:\n%s", h)
	}
	if strings.Contains(h, "<b>") {
		t.Errorf("SECURITY: a label was not escaped:\n%s", h)
	}
}

func TestPaletteResultsEmptyRow(t *testing.T) {
	h := string(PaletteResults(context.Background(), "", nil, "Nothing here"))
	if !strings.Contains(h, `aria-disabled="true"`) || !strings.Contains(h, "Nothing here") {
		t.Fatalf("empty results = %s", h)
	}
	if h := string(PaletteResults(context.Background(), "", nil, "")); !strings.Contains(h, "No matches") {
		t.Fatalf("empty results default = %s", h)
	}
}

// The palette sheet styles its option rows, including the active row
// the combobox runtime marks.
func TestPaletteSheetStylesOptions(t *testing.T) {
	css := commandPaletteCSS(style.Theme{})
	for _, want := range []string{".fui-cmd-palette__option {", ".fui-cmd-palette__option.is-active", ".fui-cmd-palette__option[hidden]"} {
		if !strings.Contains(css, want) {
			t.Errorf("palette sheet lacks %q", want)
		}
	}
}
