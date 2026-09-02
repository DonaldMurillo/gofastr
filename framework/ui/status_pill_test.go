package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
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

func TestStatusPillExtraAttrsOnRoot(t *testing.T) {
	h := StatusPill(StatusPillConfig{Label: "v0.1.0", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("StatusPill root missing data-test:\n%s", root)
	}
}

// A pill hidden by script (el.hidden = true, the pattern a page uses
// to show one of a pair) must actually disappear: the component's
// author-origin display rule outranks the UA's [hidden] rule, so the
// stylesheet has to restate it.
func TestStatusPillHonorsHidden(t *testing.T) {
	css := statusPillCSS(style.Theme{})
	// The plain hidden state is display: none; hidden="until-found"
	// stays out of the rule so the UA's revealable state survives.
	if !strings.Contains(css, `[data-fui-comp="ui-status-pill"][hidden]:not([hidden="until-found"]) {
  display: none;
}`) {
		t.Fatalf("status pill CSS has no [hidden] rule that spares until-found:\n%s", css)
	}
}
