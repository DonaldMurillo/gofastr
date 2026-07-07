package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestStickyEmitsZTierAttr(t *testing.T) {
	h := string(Sticky(StickyConfig{ZIndexTier: "modal"}))
	if !strings.Contains(h, `data-fui-z-tier="modal"`) {
		t.Errorf("expected data-fui-z-tier=modal, got:\n%s", h)
	}
}

func TestStickyZTierWiredToCSS(t *testing.T) {
	css := stickyCSS(style.Theme{})
	// ZIndexTier must not be a silent no-op: each non-default tier
	// needs a CSS rule mapping the attribute to its theme z token.
	for _, tier := range []string{"dropdown", "modal", "popover", "toast"} {
		if !strings.Contains(css, `[data-fui-z-tier="`+tier+`"]`) {
			t.Errorf("sticky CSS missing rule for z-tier %q:\n%s", tier, css)
		}
		if !strings.Contains(css, "var(--z-"+tier) {
			t.Errorf("sticky CSS should reference var(--z-%s):\n%s", tier, css)
		}
	}
	// The theme emits --z-<name> custom properties (style/tokens.go);
	// --z-index-sticky never existed.
	if strings.Contains(css, "--z-index-sticky") {
		t.Errorf("sticky CSS references nonexistent --z-index-sticky token:\n%s", css)
	}
	if !strings.Contains(css, "var(--z-sticky") {
		t.Errorf("default tier should read var(--z-sticky):\n%s", css)
	}
}

func TestStickyUnknownTierPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Sticky with an unknown ZIndexTier must panic — a typo would silently fall back")
		}
	}()
	Sticky(StickyConfig{ZIndexTier: "overlay"})
}
