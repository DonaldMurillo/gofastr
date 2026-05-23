package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func htmlString(t *testing.T, h render.HTML) string {
	t.Helper()
	return string(h)
}

// swapDefault temporarily overrides an i18nui default and restores
// it via t.Cleanup. Used to prove components actually look the key
// up rather than emitting a parallel hardcoded string.
func swapDefault(t *testing.T, key i18nui.Key, val string) {
	t.Helper()
	prev := i18nui.Defaults[key]
	i18nui.Defaults[key] = val
	t.Cleanup(func() { i18nui.Defaults[key] = prev })
}

// Repeater default labels come from i18nui.Defaults.
func TestRepeaterUsesI18nDefaults(t *testing.T) {
	swapDefault(t, i18nui.KeyRepeaterAdd, "PROBE-ADD")
	out := htmlString(t, Repeater(RepeaterConfig{Name: "x"}))
	if !strings.Contains(out, "PROBE-ADD") {
		t.Fatalf("expected PROBE-ADD in output, got:\n%s", out)
	}
}

// Repeater AddLabel override still wins.
func TestRepeaterCustomAddLabelWins(t *testing.T) {
	out := htmlString(t, Repeater(RepeaterConfig{Name: "x", AddLabel: "Custom add"}))
	if !strings.Contains(out, "Custom add") {
		t.Fatalf("expected custom label, got:\n%s", out)
	}
	if strings.Contains(out, "Add item") {
		t.Fatalf("default leaked when override set, got:\n%s", out)
	}
}

// PasswordInput show toggle aria-label uses i18nui default.
func TestPasswordInputShowUsesI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyPasswordInputShow, "PROBE-SHOW")
	out := htmlString(t, PasswordInput(PasswordInputConfig{Name: "p", ID: "p"}))
	if !strings.Contains(out, "PROBE-SHOW") {
		t.Fatalf("expected PROBE-SHOW in output, got:\n%s", out)
	}
}

// StepWizard Back/Submit labels use i18nui defaults.
func TestStepWizardLabelsUseI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyStepWizardBack, "PROBE-BACK")
	swapDefault(t, i18nui.KeyStepWizardSubmit, "PROBE-SUBMIT")
	out := htmlString(t, StepWizard(StepWizardConfig{
		Steps: []StepWizardStep{
			{Heading: "A"},
			{Heading: "B"},
		},
		CurrentStep: 1,
		Action:      "/x",
	}))
	if !strings.Contains(out, "PROBE-BACK") {
		t.Fatalf("missing PROBE-BACK, got:\n%s", out)
	}
	if !strings.Contains(out, "PROBE-SUBMIT") {
		t.Fatalf("missing PROBE-SUBMIT, got:\n%s", out)
	}
}

// Lightbox nav/download aria-labels use i18nui defaults.
func TestLightboxLabelsUseI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyLightboxPrev, "PROBE-PREV")
	swapDefault(t, i18nui.KeyLightboxNext, "PROBE-NEXT")
	swapDefault(t, i18nui.KeyLightboxDownload, "PROBE-DL")
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true, allowDownload: true}
	out := string(slot.Render())
	if !strings.Contains(out, "PROBE-PREV") || !strings.Contains(out, "PROBE-NEXT") {
		t.Fatalf("missing nav probes, got:\n%s", out)
	}
	if !strings.Contains(out, "PROBE-DL") {
		t.Fatalf("missing download probe, got:\n%s", out)
	}
}
