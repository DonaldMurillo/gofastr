package progress

import (
	"strings"
	"testing"
)

func TestRequiresLabel(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Label empty")
		}
	}()
	New(Config{Value: 50})
}

func TestDeterminateRendersValueAndMax(t *testing.T) {
	h := string(New(Config{Value: 30, Max: 100, Label: "Upload"}))
	for _, want := range []string{
		`<progress`, `class="progress-bar"`, `max="100"`,
		`value="30"`, `aria-label="Upload"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestIndeterminateOmitsValue(t *testing.T) {
	h := string(New(Config{Value: -1, Label: "Working"}))
	if strings.Contains(h, "value=") {
		t.Errorf("expected no value= for indeterminate, got: %s", h)
	}
	if !strings.Contains(h, "max=") {
		t.Errorf("expected max= present, got: %s", h)
	}
}

func TestDefaultMaxIs100(t *testing.T) {
	h := string(New(Config{Value: 50, Label: "x"}))
	if !strings.Contains(h, `max="100"`) {
		t.Errorf("expected default max=100, got: %s", h)
	}
}

func TestDescriptionRenders(t *testing.T) {
	h := string(New(Config{Value: 7, Max: 10, Label: "x", Description: "7 of 10"}))
	if !strings.Contains(h, "progress-description") || !strings.Contains(h, "7 of 10") {
		t.Errorf("expected description to render, got: %s", h)
	}
}

func TestNoDescriptionWhenEmpty(t *testing.T) {
	h := string(New(Config{Value: 1, Label: "x"}))
	if strings.Contains(h, "progress-description") {
		t.Errorf("did not expect description, got: %s", h)
	}
}

func TestBaseCSSContainsKeySelectors(t *testing.T) {
	css := BaseCSS()
	for _, want := range []string{
		".progress", ".progress-bar", "::-webkit-progress-value", "::-moz-progress-bar",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("BaseCSS missing %q", want)
		}
	}
}
