package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestSignalToggleBasic(t *testing.T) {
	s := string(SignalToggle(SignalToggleConfig{SignalName: "dark"}))
	for _, want := range []string{
		`data-fui-signal-toggle="dark"`,
		`data-fui-signal="dark"`,
		`data-fui-signal-mode="attr"`,
		`data-fui-signal-attr="aria-checked"`,
		`role="switch"`,
		`aria-checked="false"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("toggle missing %q:\n%s", want, s)
		}
	}
}

func TestSignalToggleLabelFallsBackToSignal(t *testing.T) {
	s := string(SignalToggle(SignalToggleConfig{SignalName: "wifi"}))
	if !strings.Contains(s, `aria-label="wifi"`) {
		t.Fatalf("label should fall back to signal name: %s", s)
	}
}

func TestSignalToggleCustomLabel(t *testing.T) {
	s := string(SignalToggle(SignalToggleConfig{SignalName: "wifi", Label: "Wi-Fi"}))
	if !strings.Contains(s, `aria-label="Wi-Fi"`) {
		t.Fatalf("custom label not applied: %s", s)
	}
}

func TestSignalTogglePanicsMissingSignal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty SignalName")
		}
	}()
	SignalToggle(SignalToggleConfig{})
}

// TestSignalToggleRegistersCSS guards that SignalToggle ships its own
// scoped CSS — it stamps data-fui-comp="fui-toggle" but had no
// registered style, so the track/thumb rendered unstyled.
func TestSignalToggleRegistersCSS(t *testing.T) {
	css := signalToggleStyle.Entry().CSSFor(style.Theme{})
	for _, sel := range []string{
		`[data-fui-comp="fui-toggle"]`,
		".fui-toggle__track",
		".fui-toggle__thumb",
	} {
		if !strings.Contains(css, sel) {
			t.Errorf("toggle CSS missing %q:\n%s", sel, css)
		}
	}
}

// TestSignalToggleCarriesCompMarker ensures the host can find the
// component to emit its CSS.
func TestSignalToggleCarriesCompMarker(t *testing.T) {
	s := string(SignalToggle(SignalToggleConfig{SignalName: "x"}))
	if !strings.Contains(s, `data-fui-comp="fui-toggle"`) {
		t.Fatalf("toggle missing comp marker: %s", s)
	}
}
