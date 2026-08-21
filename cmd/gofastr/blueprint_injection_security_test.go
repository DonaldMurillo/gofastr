package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework"
)

// bpWithEnum builds a minimal one-entity blueprint whose status field
// carries the given enum value.
func bpWithEnum(v string) Blueprint {
	return Blueprint{Entities: []framework.EntityDeclaration{{
		Name: "tickets",
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "string", Values: []string{v}},
		},
	}}}
}

// bpWithField builds the same shape with an attacker-shaped field name.
func bpWithField(name string) Blueprint {
	return Blueprint{Entities: []framework.EntityDeclaration{{
		Name:   "tickets",
		Fields: []framework.FieldDeclaration{{Name: name, Type: "string"}},
	}}}
}

// Property: no spec-derived string may terminate the Go literal it is
// emitted into.
//
// The e2e emitters used a backtick raw literal, which has NO escape
// mechanism: an enum value or field name containing a backtick closed
// it and the remainder became real Go source in the generated
// e2e_test.go. `format.Source` did not catch it because the injected
// code is valid Go, so it ran at `go test` time with nobody reading the
// generated file. The blueprint is normally developer-authored, but the
// documented workflow has an AGENT authoring gofastr.yml from
// natural-language requirements: enum values and field names are
// exactly what gets transcribed.
func TestBlueprintSpecCannotEscapeGoLiteral(t *testing.T) {
	if err := validateBlueprint(bpWithEnum("open`+PWN()+`")); err == nil {
		t.Error("backtick in an enum value was accepted")
	}
	if err := validateBlueprint(bpWithEnum(`open"+PWN()+"`)); err == nil {
		t.Error("double quote in an enum value was accepted")
	}
	if err := validateBlueprint(bpWithEnum("open\nfunc PWN()")); err == nil {
		t.Error("newline in an enum value was accepted")
	}
	if err := validateBlueprint(bpWithField("title`+PWN()+`")); err == nil {
		t.Error("backtick in a field name was accepted")
	}
	// The ordinary spec still validates.
	if err := validateBlueprint(bpWithEnum("open")); err != nil {
		t.Errorf("legitimate enum value rejected: %v", err)
	}
}

// Property: a spec-derived font family must not escape the CSS string
// literal it is interpolated into. Edge-trimming quotes missed an
// interior quote, which closed `font-family: '…'` and appended
// attacker-chosen rules to the app-wide stylesheet.
func TestFontFamilyCannotInjectCSS(t *testing.T) {
	for _, in := range []string{
		`Evil'} body{background:url('https://attacker.test/exfil')}@font-face{font-family:'z`,
		`Inter"; } body { display:none } .x{`,
		"Inter\\3c/style\\3e",
	} {
		got := blueprintFontFamilyName(in)
		if strings.ContainsAny(got, `'"{}();\/<>:`) {
			t.Errorf("font family %q survived sanitisation as %q", in, got)
		}
	}
	// Real families are untouched.
	for in, want := range map[string]string{
		"Inter":                     "Inter",
		"'Playfair Display', serif": "Playfair Display",
		"IBM Plex Sans":             "IBM Plex Sans",
	} {
		if got := blueprintFontFamilyName(in); got != want {
			t.Errorf("blueprintFontFamilyName(%q) = %q, want %q", in, got, want)
		}
	}
}

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
