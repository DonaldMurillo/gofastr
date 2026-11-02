package textsafe

import (
	"strings"
	"testing"
)

// mandatoryBreaks is every codepoint Unicode makes a mandatory line
// break (UAX #14 classes BK, CR, LF, NL). A value carrying any of them
// reads as two lines to some log reader: LF/CR to all of them, NEL and
// U+2028/U+2029 to Python's splitlines and JavaScript's line
// terminators. IsUnsafe had NEL (as a C1 control) and not the other two.
var mandatoryBreaks = []rune{0x0A, 0x0B, 0x0C, 0x0D, 0x85, 0x2028, 0x2029}

func TestUnsafeCoversEveryMandatoryLineBreak(t *testing.T) {
	for _, r := range mandatoryBreaks {
		if !IsUnsafe(r) {
			t.Errorf("IsUnsafe(%U) = false; a line break forges a log line", r)
		}
		s := "GET /a" + string(r) + "level=ERROR msg=forged"
		if !ContainsUnsafe(s) {
			t.Errorf("ContainsUnsafe misses %U", r)
		}
		if got := StripUnsafe(s); strings.ContainsRune(got, r) {
			t.Errorf("StripUnsafe kept %U: %q", r, got)
		}
		if got := ScrubControlBytes(s); strings.ContainsRune(got, r) {
			t.Errorf("ScrubControlBytes kept %U raw: %q", r, got)
		}
	}
	if got := ScrubControlBytes("a b"); got != "a%e2%80%a8b" {
		t.Errorf("ScrubControlBytes(U+2028) = %q, want percent-encoded bytes", got)
	}
}
