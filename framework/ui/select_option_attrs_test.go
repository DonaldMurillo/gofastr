package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

func TestSelectOptionMetadata(t *testing.T) {
	out := string(Select(SelectConfig{Name: "locale", Label: "Language", Options: []SelectOption{
		{Value: "/es", Text: "Español", Selected: true, ExtraAttrs: html.Attrs{
			"lang": "es", "data-short-label": "ES", "VALUE": "/forged", "selected": "false", "data-cui-open": "forged",
		}},
	}}))
	for _, want := range []string{`lang="es"`, `data-short-label="ES"`, `value="/es"`, `selected=""`} {
		if !strings.Contains(out, want) {
			t.Errorf("option metadata missing %s: %s", want, out)
		}
	}
	if strings.Contains(out, "forged") || strings.Contains(out, `selected="false"`) {
		t.Fatalf("option metadata overrode framework-owned state: %s", out)
	}
}
