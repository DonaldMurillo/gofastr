package pluginhost

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// TestBrokerThemeTokenListMatchesTheme pins #271's fix: the broker
// bridges the theme's CANONICAL token vocabulary read from computed
// style (no stylesheet-walk discovery, which raced sheet parsing and
// was blind to @media-nested declarations). The JS list is hand-written
// in host/pluginhost.js; this test keeps it byte-equal to what the
// theme actually emits, so a token added to style.Theme cannot silently
// go unbridged — the exact partial-palette failure the walker had.
func TestBrokerThemeTokenListMatchesTheme(t *testing.T) {
	js := string(brokerJSBytes)
	start := strings.Index(js, "var THEME_TOKENS = [")
	if start == -1 {
		t.Fatal("THEME_TOKENS array missing from broker JS")
	}
	end := strings.Index(js[start:], "];")
	if end == -1 {
		t.Fatal("THEME_TOKENS array unterminated")
	}
	block := js[start : start+end]
	re := regexp.MustCompile(`"--([A-Za-z0-9_-]+)"`)
	inJS := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		inJS[m[1]] = true
	}

	canonical := style.TokenNames()
	for _, name := range canonical {
		if !inJS[name] {
			t.Errorf("theme token %q missing from broker THEME_TOKENS: frames would get a partial palette", name)
		}
		delete(inJS, name)
	}
	for extra := range inJS {
		t.Errorf("broker THEME_TOKENS has %q, which the theme does not emit; remove it", extra)
	}
	if len(canonical) == 0 {
		t.Fatal("style.TokenNames() returned nothing; the pin is vacuous")
	}
}
