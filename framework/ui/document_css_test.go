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
	if strings.Index(out, ".fui-button") > strings.Index(out, "</head>") {
		t.Fatal("component CSS was not placed in head")
	}
}
