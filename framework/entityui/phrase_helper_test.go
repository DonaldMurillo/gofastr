package entityui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// testPhrase defines a test-only phrase whose English is s.
func testPhrase(s string) i18n.Phrase {
	key := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, s)
	return i18n.Define("entityui_test."+key, s)
}
