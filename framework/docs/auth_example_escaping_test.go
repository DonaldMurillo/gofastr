package docs

import (
	"strings"
	"testing"
)

// The documented ConfirmPage marks render.HTML as trusted. Dynamic email and
// token values must go through escaping builders before entering that type.
func TestConfirmPageExampleEscapes(t *testing.T) {
	section := sectionAfter(t, readDoc(t, "auth.md"), "MagicLinkConfig.ConfirmPage", 2000)
	for _, bad := range []struct {
		value   string
		pattern string
	}{
		{"Email", `render.HTML("<p>Continue as "+d.Email+"?</p>")`},
		{"Token", "`+d.Token+`"},
	} {
		if strings.Contains(section, bad.pattern) {
			t.Fatalf("auth.md concatenates ConfirmPageData.%s into trusted render.HTML; copied code emits unescaped attacker-controlled markup", bad.value)
		}
	}
}
