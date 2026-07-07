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

// TestSignalToggleLabelEscaped pins that an adversarial Label cannot
// break out of the aria-label attribute (it is interpolated into the
// final Sprintf, so escaping is on this component, not render.Tag).
func TestSignalToggleLabelEscaped(t *testing.T) {
	s := string(SignalToggle(SignalToggleConfig{
		SignalName: "dark",
		Label:      `"><img src=x onerror=alert(1)>`,
	}))
	if strings.Contains(s, `<img src=x onerror=alert(1)>`) {
		t.Fatalf("label broke out of aria-label attribute:\n%s", s)
	}
	if !strings.Contains(s, `aria-label="&quot;&gt;&lt;img src=x onerror=alert(1)&gt;"`) {
		t.Fatalf("label not attribute-escaped:\n%s", s)
	}
}

// TestSignalToggleNameEscaped pins the same property for the signal
// name, which is interpolated into three attributes and falls back
// into aria-label.
func TestSignalToggleNameEscaped(t *testing.T) {
	s := string(SignalToggle(SignalToggleConfig{
		SignalName: `x" onmouseover="alert(1)`,
	}))
	if strings.Contains(s, `onmouseover="alert(1)"`) {
		t.Fatalf("signal name broke out of its attribute:\n%s", s)
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
