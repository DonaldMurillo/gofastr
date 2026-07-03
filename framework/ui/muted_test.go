package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestMutedMarksAndWraps(t *testing.T) {
	out := string(Muted(render.Text("3 drafts")))
	if !strings.Contains(out, `data-fui-comp="ui-muted"`) {
		t.Fatalf("Muted must carry the style marker: %s", out)
	}
	if !strings.Contains(out, `class="ui-muted"`) || !strings.Contains(out, "3 drafts") {
		t.Fatalf("Muted output wrong: %s", out)
	}
}

func TestEmptyValueIsMutedDash(t *testing.T) {
	out := string(EmptyValue())
	if !strings.Contains(out, "—") || !strings.Contains(out, "ui-muted") {
		t.Fatalf("EmptyValue should be a muted em dash: %s", out)
	}
}

func TestMutedCSSUsesToken(t *testing.T) {
	css := mutedCSS(style.Theme{})
	if !strings.Contains(css, "var(--color-text-muted") {
		t.Fatalf("muted color must come from the theme token: %s", css)
	}
}
