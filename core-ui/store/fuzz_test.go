package store

import (
	"strings"
	"testing"
)

// FuzzScanReferenced asserts the scanner never panics and never invents
// a name absent from the input.
func FuzzScanReferenced(f *testing.F) {
	f.Add(`<span data-cui-signal="a">x</span>`)
	f.Add(`<button data-cui-signal-set="b:1">s</button>`)
	f.Add(`<pre>data-cui-signal="ghost"</pre>`)
	f.Add(`data-cui-signal="`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, html string) {
		got := ScanReferenced(html)
		for _, n := range got {
			if !strings.Contains(html, n) {
				t.Fatalf("scan returned name %q not present in input", n)
			}
		}
	})
}

// FuzzValidateName asserts that any name validateName accepts contains
// only attribute-safe characters, it can never break out of a
// data-cui-* attribute value.
func FuzzValidateName(f *testing.F) {
	f.Add("org.companyName")
	f.Add(`a"b`)
	f.Add("a<b")
	f.Add("a b")
	f.Fuzz(func(t *testing.T, name string) {
		accepted := true
		func() {
			defer func() {
				if recover() != nil {
					accepted = false
				}
			}()
			validateName(name)
		}()
		if !accepted {
			return
		}
		// Accepted ⇒ must be attr-safe.
		for _, r := range name {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			case r == '.' || r == '_' || r == '-':
			default:
				t.Fatalf("validateName accepted unsafe rune %q in %q", r, name)
			}
		}
	})
}
