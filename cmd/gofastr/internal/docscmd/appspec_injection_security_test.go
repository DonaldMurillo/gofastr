package docscmd

import (
	"testing"
	"unicode/utf8"
)

// Property: CLI text formatting never slices a string mid-rune.
//
// Not an attacker surface: `gofastr docs --grep <term>` takes the
// operator's own argv against embedded first-party markdown, but the
// failure mode was a hard panic of the shipped binary on ordinary Unicode
// input, so the invariant is worth pinning where the code lives.

// TestHighlightSurvivesFoldingRunes pins that highlight neither panics nor
// emits invalid UTF-8 when strings.ToLower changes a rune's byte width.
// U+0130 'İ' lowers to a one-byte 'i' (2 bytes -> 1), so locating the match
// in the lowered copy and slicing the original ran past the end.
func TestHighlightSurvivesFoldingRunes(t *testing.T) {
	for _, tc := range []struct{ s, term string }{
		{"I", "İ"},                   // shrinking fold, match at end
		{"the letter I", "İ"},        // shrinking fold, mid-string
		{"İx", "x"},                  // wide rune before the match
		{"İabc", "abc"},              // wide rune before a long match
		{"straße STRASSE", "straße"}, // multi-byte term
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("highlight(%q, %q) panicked: %v", tc.s, tc.term, r)
				}
			}()
			out := highlight(tc.s, tc.term)
			if !utf8.ValidString(out) {
				t.Errorf("highlight(%q, %q) = %q, not valid UTF-8", tc.s, tc.term, out)
			}
			// bold() is a no-op off a TTY, so under `go test` highlight
			// must return the input byte-for-byte. That is exactly the
			// invariant that failed: the old slicing corrupted the text.
			if out != tc.s {
				t.Errorf("highlight(%q, %q) altered the text: %q", tc.s, tc.term, out)
			}
		}()
	}
}

// TestFoldMatchStillFindsTerms guards against a fix that stops matching.
// bold() is TTY-gated, so the match itself is asserted on the folding
// helper rather than on highlight's (uncolored) output.
func TestFoldMatchStillFindsTerms(t *testing.T) {
	for _, tc := range []struct {
		s, term string
		want    int
	}{
		{"Entity declaration", "entity", 6},
		{"ENTITY", "entity", 6},
		{"straße", "STRASSE", 0}, // fold is per-rune, not full case-folding
		{"İx", "i", 2},           // wide rune folds to a one-byte match
		{"other", "entity", 0},
	} {
		got, ok := foldPrefixLen(tc.s, tc.term)
		if tc.want == 0 {
			if ok {
				t.Errorf("foldPrefixLen(%q, %q) matched %d, want no match", tc.s, tc.term, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("foldPrefixLen(%q, %q) = %d,%v; want %d,true", tc.s, tc.term, got, ok, tc.want)
		}
	}
}
