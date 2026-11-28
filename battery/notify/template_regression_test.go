package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestInterpolationCapsAtUTF8Boundary(t *testing.T) {
	data := map[string]any{"message": strings.Repeat("x", MaxInterpolatedOutputBytes-1) + "é"}
	html, err := renderHTMLBody("{{message}}suffix", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{interpolate("{{message}}suffix", data), html} {
		if !utf8.ValidString(rendered) || len(rendered) != MaxInterpolatedOutputBytes-1 {
			t.Fatalf("rendered malformed or overlong output of %d bytes", len(rendered))
		}
	}
}

func TestZeroMapTemplaterStoresTemplates(t *testing.T) {
	var templates MapTemplater
	templates.Set("message", "log", Template{Subject: "hello"})
	rendered, err := templates.Render(t.Context(), "message", "log", nil)
	if err != nil || rendered.Subject != "hello" {
		t.Fatalf("rendered = %+v, %v", rendered, err)
	}
}
