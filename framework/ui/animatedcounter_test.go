package ui

import (
	"strings"
	"testing"
)

func TestAnimatedCounterSSRRendersFinalValue(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{To: 1234}))
	// Final value MUST be in SSR — no-JS users + reduced-motion users
	// see the target without any animation.
	if !strings.Contains(h, ">1234<") {
		t.Errorf("SSR should render the target value:\n%s", h)
	}
}

func TestAnimatedCounterEmitsRuntimeMarkers(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{To: 99, From: 10, DurationMs: 800}))
	if !strings.Contains(h, `data-fui-animated-counter="99"`) {
		t.Errorf("expected data-fui-animated-counter=99:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-animated-counter-from="10"`) {
		t.Errorf("expected data-fui-animated-counter-from=10:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-animated-counter-ms="800"`) {
		t.Errorf("expected data-fui-animated-counter-ms=800:\n%s", h)
	}
}

func TestAnimatedCounterDurationDefaults(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{To: 1}))
	if !strings.Contains(h, `data-fui-animated-counter-ms="1200"`) {
		t.Errorf("expected default ms=1200:\n%s", h)
	}
}

func TestAnimatedCounterPrefixSuffix(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{
		To: 100, Prefix: "$", Suffix: "+",
	}))
	if !strings.Contains(h, "ui-animated-counter__prefix") || !strings.Contains(h, "$") {
		t.Errorf("Prefix should render:\n%s", h)
	}
	if !strings.Contains(h, "ui-animated-counter__suffix") || !strings.Contains(h, "+") {
		t.Errorf("Suffix should render:\n%s", h)
	}
}
