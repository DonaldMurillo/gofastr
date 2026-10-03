package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Screen is the footer-at-bottom option: the class must ride the stack
// root and the sheet must own both halves (the viewport minimum and
// the last-child push), or a short page's footer rides up under the
// content instead of anchoring to the screen bottom.
func TestStackScreenOption(t *testing.T) {
	out := string(Stack(StackConfig{Screen: true}, render.Text("a"), render.Text("b")))
	if !strings.Contains(out, "fui-stack--screen") {
		t.Errorf("Screen should carry its class on the stack root:\n%s", out)
	}
	out = string(Stack(StackConfig{}, render.Text("a")))
	if strings.Contains(out, "fui-stack--screen") {
		t.Errorf("zero value must change nothing:\n%s", out)
	}
	css := layoutCSS(style.Theme{})
	if !strings.Contains(css, `.fui-stack--screen { min-block-size: 100dvh; }`) {
		t.Errorf("screen stack must be at least one viewport tall:\n%s", css)
	}
	if !strings.Contains(css, `.fui-stack--screen > :last-child { margin-block-start: auto; }`) {
		t.Errorf("screen stack must push its last child down:\n%s", css)
	}
}
