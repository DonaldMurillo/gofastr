package ui

import (
	"strings"
	"testing"
)

func TestIcon_BuiltInsRegistered(t *testing.T) {
	for _, name := range []string{
		"check", "close", "chevron-up", "chevron-down",
		"chevron-left", "chevron-right",
		"info", "warning", "danger", "success",
	} {
		if !IconRegistered(name) {
			t.Errorf("expected built-in icon %q to be registered", name)
		}
	}
}

func TestIcon_RendersInlineSVG(t *testing.T) {
	got := string(Icon("check", IconConfig{}))
	if !strings.Contains(got, "<svg") {
		t.Errorf("expected <svg>, got: %s", got)
	}
	if !strings.Contains(got, `stroke="currentColor"`) && !strings.Contains(got, `fill="currentColor"`) {
		t.Errorf("expected currentColor on stroke or fill, got: %s", got)
	}
	if !strings.Contains(got, `viewBox=`) {
		t.Errorf("expected viewBox, got: %s", got)
	}
}

func TestIcon_DefaultIsAriaHidden(t *testing.T) {
	got := string(Icon("check", IconConfig{}))
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("expected aria-hidden=\"true\" without AriaLabel, got: %s", got)
	}
}

func TestIcon_AriaLabelSetsRoleImg(t *testing.T) {
	got := string(Icon("check", IconConfig{AriaLabel: "Saved"}))
	if !strings.Contains(got, `role="img"`) {
		t.Errorf("expected role=\"img\" with AriaLabel, got: %s", got)
	}
	if !strings.Contains(got, `aria-label="Saved"`) {
		t.Errorf("expected aria-label value, got: %s", got)
	}
	if strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("must not set aria-hidden when an explicit label is given, got: %s", got)
	}
}

func TestIcon_SizeOverridesDefault(t *testing.T) {
	got := string(Icon("check", IconConfig{Size: "1.25rem"}))
	if !strings.Contains(got, `width="1.25rem"`) || !strings.Contains(got, `height="1.25rem"`) {
		t.Errorf("expected width/height = 1.25rem, got: %s", got)
	}
}

func TestIcon_CustomClass(t *testing.T) {
	got := string(Icon("check", IconConfig{Class: "my-icon"}))
	if !strings.Contains(got, "ui-icon my-icon") {
		t.Errorf("expected base + custom class, got: %s", got)
	}
}

func TestIcon_UnknownNameRendersEmpty(t *testing.T) {
	got := string(Icon("does-not-exist", IconConfig{}))
	if got != "" {
		t.Errorf("expected empty render for unknown icon, got: %s", got)
	}
}

func TestRegisterIcon_AddsCustomIcon(t *testing.T) {
	RegisterIcon("test-square",
		`<rect x="2" y="2" width="20" height="20" stroke="currentColor" stroke-width="2"/>`)
	if !IconRegistered("test-square") {
		t.Fatal("expected test-square to be registered")
	}
	got := string(Icon("test-square", IconConfig{}))
	if !strings.Contains(got, "<rect") {
		t.Errorf("expected registered SVG body to render, got: %s", got)
	}
}
