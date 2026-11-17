package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestStandaloneDocumentIncludesOnlyRenderedComponentCSS(t *testing.T) {
	doc := render.Join(render.Raw("<!doctype html><html><head></head><body>"), Button(ButtonConfig{Label: "Save"}), render.Raw("</body></html>"))
	out := string(WithComponentCSS(doc, style.DefaultTheme()))
	if !strings.Contains(out, ".fui-button") || strings.Contains(out, ".fui-carousel") {
		t.Fatalf("incorrect standalone component styles")
	}
	css, head, end := strings.Index(out, ".fui-button"), strings.Index(out, "<head>"), strings.Index(out, "</head>")
	if !strings.HasPrefix(out, "<!doctype html>") || css < head || css > end {
		t.Fatalf("component CSS is not inside head: %.120q", out)
	}
}
