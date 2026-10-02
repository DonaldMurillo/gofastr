package ownstyle

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// A keyframes name that is also a keyword of another animation
// property is renamed only where the shorthand reads it as the name:
// the first such keyword goes to its own property (CSS Animations,
// "keywords that are valid for properties other than animation-name
// whose values were not found earlier in the shorthand must be
// accepted for those properties"), and only an identifier left over
// is the name. animation-name reads every identifier as a name.
func TestCompileKeyframeNameMatchingAKeyword(t *testing.T) {
	cases := []struct{ src, want string }{
		// Second "linear" is the name; the first is the timing function.
		{".a { animation: linear 1s linear; } @keyframes linear { to { opacity: 1; } }",
			"animation:linear 1s x-linear"},
		{".a { animation: 1s linear linear; } @keyframes linear { to { opacity: 1; } }",
			"animation:1s linear x-linear"},
		// A lone "linear" is the timing function: no name at all.
		{".a { animation: 1s linear; } @keyframes linear { to { opacity: 1; } }",
			"animation:1s linear"},
		// Every keyword family: each taken once, then the name.
		{".a { animation: infinite 1s infinite; } @keyframes infinite { to { opacity: 1; } }",
			"animation:infinite 1s x-infinite"},
		{".a { animation: alternate 1s alternate; } @keyframes alternate { to { opacity: 1; } }",
			"animation:alternate 1s x-alternate"},
		{".a { animation: both 1s both; } @keyframes both { to { opacity: 1; } }",
			"animation:both 1s x-both"},
		{".a { animation: paused 1s paused; } @keyframes paused { to { opacity: 1; } }",
			"animation:paused 1s x-paused"},
		// A timing function written as a function sets the timing
		// property too, so a later "ease" is the name.
		{".a { animation: cubic-bezier(0, 0, 1, 1) 1s ease; } @keyframes ease { to { opacity: 1; } }",
			"animation:cubic-bezier(0,0,1,1) 1s x-ease"},
		// Each comma-separated animation starts fresh.
		{".a { animation: linear 1s linear, 2s linear; } @keyframes linear { to { opacity: 1; } }",
			"animation:linear 1s x-linear,2s linear"},
		// animation-name: every identifier is a name.
		{".a { animation-name: linear, spin; } @keyframes linear { to { opacity: 1; } } @keyframes spin { to { opacity: 0; } }",
			"animation-name:x-linear,x-spin"},
	}
	tok := style.ThemeToTokens(style.DefaultTheme())
	for _, tc := range cases {
		sheet, diags := Parse(tc.src)
		if len(diags) != 0 {
			t.Fatalf("%q: parse diags: %v", tc.src, diags)
		}
		out, err := Compile(sheet, "x", KindScoped, tok)
		if err != nil {
			t.Fatalf("%q: %v", tc.src, err)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%q:\n got %s\nwant it to contain %s", tc.src, out, tc.want)
		}
	}
}
