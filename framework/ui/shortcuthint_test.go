package ui

import (
	"strings"
	"testing"
)

func TestShortcutHintModK(t *testing.T) {
	out := string(ShortcutHint(ShortcutHintConfig{Chord: "Mod+K"}))
	wants := []string{
		`<kbd `,
		`ui-shortcut-hint__key--mod`,
		`ui-shortcut-hint__mod-mac`,
		`ui-shortcut-hint__mod-other`,
		`>⌘<`,
		`>Ctrl<`,
		`>K<`,
		`aria-hidden="true"`,
		`Shortcut: Mod-K`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("ShortcutHint missing %q\nout: %s", w, out)
		}
	}
}

func TestShortcutHintSlashAndEsc(t *testing.T) {
	cases := map[string]string{
		"/":     `>/<`,
		"Esc":   `>Esc<`,
		"Enter": `>↵<`,
		"Tab":   `>Tab<`,
		"Up":    `>↑<`,
	}
	for chord, want := range cases {
		out := string(ShortcutHint(ShortcutHintConfig{Chord: chord}))
		if !strings.Contains(out, want) {
			t.Errorf("Chord %q: expected %q in output, got: %s", chord, want, out)
		}
	}
}

func TestShortcutHintShiftAlt(t *testing.T) {
	out := string(ShortcutHint(ShortcutHintConfig{Chord: "Mod+Shift+Alt+P"}))
	for _, w := range []string{`>⇧<`, `>⌥<`, `>P<`} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in: %s", w, out)
		}
	}
	if !strings.Contains(out, "Shortcut: Mod-Shift-Alt-P") {
		t.Errorf("missing SR label, got: %s", out)
	}
}

func TestShortcutHintCustomSRLabel(t *testing.T) {
	out := string(ShortcutHint(ShortcutHintConfig{
		Chord:       "Mod+K",
		SROnlyLabel: "Open command palette",
	}))
	if !strings.Contains(out, "Shortcut: Open command palette") {
		t.Errorf("expected custom SR label, got: %s", out)
	}
}

func TestShortcutHintPanicsOnEmptyChord(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on empty Chord")
		}
	}()
	ShortcutHint(ShortcutHintConfig{})
}
