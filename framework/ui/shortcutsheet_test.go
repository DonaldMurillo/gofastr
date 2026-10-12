package ui

import (
	"strings"
	"testing"
)

// The sheet's trigger opens its modal on "?" and is a plain link without
// script; the modal lists each chord as keycaps beside its label, under
// a heading that names the dialog, with a close button.
func TestShortcutSheet(t *testing.T) {
	trigger, b := ShortcutSheet(ShortcutSheetConfig{
		FallbackHref: "/admin/shortcuts",
		Items:        []ShortcutItem{{Chord: "Mod+K", Label: "Open the command palette"}, {Chord: "/", Label: "Search the list"}},
	})
	tr := string(trigger)
	for _, want := range []string{`href="/admin/shortcuts"`, `data-cui-open="shortcut-sheet"`, `data-hui-shortcut-click="?"`} {
		if !strings.Contains(tr, want) {
			t.Errorf("trigger misses %q: %s", want, tr)
		}
	}
	def := b.Build()
	if def.Role != "dialog" || def.LabelledBy != "shortcut-sheet-title" {
		t.Errorf("modal role %q labelled by %q", def.Role, def.LabelledBy)
	}
	body := string(ShortcutList([]ShortcutItem{{Chord: "Mod+K", Label: "Open the command palette"}}))
	if !strings.Contains(body, "Open the command palette") || !strings.Contains(body, `data-cui-comp="ui-shortcut-hint"`) {
		t.Errorf("list: %s", body)
	}
	slot := string((&shortcutSheetSlot{name: "shortcut-sheet", title: "Keyboard shortcuts", close: "Close", items: []ShortcutItem{{Chord: "?", Label: "Show these"}}}).Render())
	for _, want := range []string{`id="shortcut-sheet-title"`, `data-cui-action="close"`, "Show these"} {
		if !strings.Contains(slot, want) {
			t.Errorf("sheet misses %q: %s", want, slot)
		}
	}
}
