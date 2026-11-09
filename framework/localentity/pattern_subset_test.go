package localentity

import "testing"

// #669 parked bug: the shared subset admitted \s, \S and a bare ., which
// Go and JavaScript read differently — JS u-mode \s is Unicode
// whitespace where Go's is ASCII [ \t\n\f\r], and . skips all four JS
// line terminators (\n \r \u2028 \u2029) but only \n in Go. They are
// refused; the class spelling that names its characters is accepted on
// both sides.
func TestCheckPatternRefusesJSGoAmbiguous(t *testing.T) {
	for _, p := range []string{
		`\s+`,           // JS: Unicode whitespace; Go: ASCII only
		`\S`,            // the complement of a different set
		`a.b`,           // . outside a class: different line-terminator sets
		`.*\s`,          // both in one pattern
		`[[:alpha:]]\s`, // a POSIX class beside \s (the class is its own refusal)
	} {
		if err := checkPattern(p); err == nil {
			t.Errorf("checkPattern(%q) accepted a pattern Go and JS read differently", p)
		}
	}
	for _, p := range []string{
		`[ \t\n\r\f\v]+`, // the explicit whitespace class, identical in both
		`a\.b`,           // an escaped literal dot
		`a[.b]`,          // a dot inside a class is a literal
		`\d{4}-\d{2}`,    // the rest of the subset stands
	} {
		if err := checkPattern(p); err != nil {
			t.Errorf("checkPattern(%q) = %v, want accepted", p, err)
		}
	}
}
