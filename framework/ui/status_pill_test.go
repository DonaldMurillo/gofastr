package ui

import (
	"strings"
	"testing"
)

func TestStatusPillRendersLabelAndMarker(t *testing.T) {
	h := StatusPill(StatusPillConfig{Label: "Get started · v0.0.4"})
	for _, want := range []string{
		`data-fui-comp="ui-status-pill"`,
		"Get started · v0.0.4",
	} {
		if !strings.Contains(string(h), want) {
			t.Errorf("StatusPill missing %q\n%s", want, h)
		}
	}
}

func TestStatusPillDotOptIn(t *testing.T) {
	with := string(StatusPill(StatusPillConfig{Label: "x", Dot: true}))
	if !strings.Contains(with, "ui-status-pill__dot") {
		t.Errorf("Dot:true should emit the dot span:\n%s", with)
	}
	without := string(StatusPill(StatusPillConfig{Label: "x"}))
	if strings.Contains(without, "ui-status-pill__dot") {
		t.Errorf("Dot defaults off; should not emit dot span:\n%s", without)
	}
}

func TestStatusPillAccentToneModifier(t *testing.T) {
	h := string(StatusPill(StatusPillConfig{Label: "x", Tone: StatusPillAccent}))
	if !strings.Contains(h, "ui-status-pill--accent") {
		t.Errorf("accent tone should emit modifier class:\n%s", h)
	}
	neutral := string(StatusPill(StatusPillConfig{Label: "x"}))
	if strings.Contains(neutral, "ui-status-pill--accent") {
		t.Errorf("neutral (default) tone should not emit accent modifier:\n%s", neutral)
	}
}

func TestStatusPillRequiresLabel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("StatusPill with empty Label should panic")
		}
	}()
	StatusPill(StatusPillConfig{})
}

func TestStatusPillRejectsUnknownTone(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("StatusPill with unknown Tone should panic")
		}
	}()
	StatusPill(StatusPillConfig{Label: "x", Tone: "glow"})
}
