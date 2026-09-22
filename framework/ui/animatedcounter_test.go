package ui

import (
	"strings"
	"testing"
)

func TestAnimatedCounterSSRRendersFinalValue(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{To: 1234}))
	// Final value MUST be in SSR. No-JS users + reduced-motion users
	// see the target without any animation.
	if !strings.Contains(h, ">1234<") {
		t.Errorf("SSR should render the target value:\n%s", h)
	}
}

func TestAnimatedCounterEmitsRuntimeMarkers(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{To: 99, From: 10, DurationMs: 800}))
	if !strings.Contains(h, `data-hui-counter-animate=""`) {
		t.Errorf("expected the animate hook:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-counter-from="10"`) {
		t.Errorf("expected the count the animation starts from:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-counter-ms="800"`) {
		t.Errorf("expected the duration bound:\n%s", h)
	}
}

func TestAnimatedCounterDurationDefaults(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{To: 1}))
	if !strings.Contains(h, `data-hui-counter-ms="1200"`) {
		t.Errorf("expected default ms=1200:\n%s", h)
	}
}

func TestAnimatedCounterPrefixSuffix(t *testing.T) {
	h := string(AnimatedCounter(AnimatedCounterConfig{
		To: 100, Prefix: "$", Suffix: "+",
	}))
	if !strings.Contains(h, "fui-animated-counter__prefix") || !strings.Contains(h, "$") {
		t.Errorf("Prefix should render:\n%s", h)
	}
	if !strings.Contains(h, "fui-animated-counter__suffix") || !strings.Contains(h, "+") {
		t.Errorf("Suffix should render:\n%s", h)
	}
}

func TestAnimatedCounterExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := AnimatedCounter(AnimatedCounterConfig{To: 42, ExtraAttrs: map[string]string{
		"data-test": "hook", "data-hui-counter-from": "999", "Class": "evil",
	}})
	root := string(h)
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `data-hui-counter-from="0"`) {
		t.Errorf("the animation's start value lost its framework value:\n%s", root)
	}
	for _, banned := range []string{"999", "evil"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
}
