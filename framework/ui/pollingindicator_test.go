package ui

import (
	"strings"
	"testing"
)

func TestPollingIndicator_DefaultRendersLiveLabelAndDot(t *testing.T) {
	got := string(PollingIndicator(PollingIndicatorConfig{}))
	checks := []string{
		"ui-polling-indicator",
		"ui-polling-indicator__dot",
		"Live", // default label
		`role="status"`,
		`aria-live="polite"`,
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q\nGOT: %s", want, got)
		}
	}
}

func TestPollingIndicator_CustomLabel(t *testing.T) {
	got := string(PollingIndicator(PollingIndicatorConfig{Label: "Syncing"}))
	if !strings.Contains(got, "Syncing") {
		t.Errorf("expected custom label, got: %s", got)
	}
	if strings.Contains(got, ">Live<") {
		t.Errorf("did not expect default label when custom set, got: %s", got)
	}
}

func TestPollingIndicator_PausedVariant(t *testing.T) {
	got := string(PollingIndicator(PollingIndicatorConfig{Paused: true}))
	if !strings.Contains(got, "ui-polling-indicator--paused") {
		t.Errorf("expected paused modifier class, got: %s", got)
	}
}

func TestPollingIndicator_CSSRegistered(t *testing.T) {
	for _, cls := range []string{
		".ui-polling-indicator",
		".ui-polling-indicator__dot",
		".ui-polling-indicator--paused",
		"prefers-reduced-motion",
	} {
		if !strings.Contains(pollingIndicatorCSSText, cls) {
			t.Errorf("pollingIndicatorCSSText missing %s", cls)
		}
	}
}

func TestPollingIndicatorExtraAttrsOnRoot(t *testing.T) {
	h := PollingIndicator(PollingIndicatorConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("PollingIndicator root missing data-test:\n%s", root)
	}
}
