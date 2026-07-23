package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestCollapsedSidebarLabelsStayAccessible(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	// The collapsed rail must hide labels with the visually-hidden clip
	// pattern, never display:none — focusable links would lose their
	// accessible names (WCAG 4.1.2).
	start := strings.Index(css, `[data-collapsed="true"] .ui-sidebar__label`)
	if start == -1 {
		t.Fatal("no collapsed-state label rule found")
	}
	block := css[start:]
	if end := strings.Index(block, "}"); end != -1 {
		block = block[:end]
	}
	if strings.Contains(block, "display: none") {
		t.Fatalf("collapsed label rule uses display:none:\n%s", block)
	}
	if !strings.Contains(block, "clip: rect(0, 0, 0, 0)") {
		t.Fatalf("collapsed label rule should use the clip pattern:\n%s", block)
	}
}
