package codetext

import (
	"reflect"
	"testing"
)

func TestStripKeepsGlobStrings(t *testing.T) {
	src := "a := \"*/*\" // tail\nb := `x/*y` /* gone\nstill gone */ c\nr := '\"' // q\nd"
	want := "a := \"*/*\" \nb := `x/*y` \n c\nr := '\"' \nd"
	if got := Strip(src, false); got != want {
		t.Errorf("Strip =\n%q\nwant\n%q", got, want)
	}
}

func TestStripTemplateEscapeInScript(t *testing.T) {
	// In JS a backslash escapes the backtick; in a Go raw string it does
	// not, so the same bytes end the literal in one language only.
	src := "s = `a\\`; // c`\n"
	if got, want := Strip(src, true), "s = `a\\`; `\n"; got != want {
		t.Errorf("script Strip = %q, want %q", got, want)
	}
	if got, want := Strip(src, false), "s = `a\\`; \n"; got != want {
		t.Errorf("Go Strip = %q, want %q", got, want)
	}
}

func TestStripCommentsInsideLiterals(t *testing.T) {
	cases := []struct{ src, want string }{
		// An emitted Go comment, first line and after an escaped newline.
		{`"// holds the @font-face rules\n// for the app\n" + x`, `"" + x`},
		// A stylesheet note in a raw string, across lines.
		{"`a { b: c; } /* knob\n2.25rem */ d`", "`a { b: c; } \n d`"},
		// Not comments: no close in the literal, no space around //.
		{`"image/*" + "*/*" + "https://cdn/x" + '//cdn/x.js'`, `"image/*" + "*/*" + "https://cdn/x" + '//cdn/x.js'`},
	}
	for _, c := range cases {
		if got := Strip(c.src, false); got != c.want {
			t.Errorf("Strip(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestCommentsCarryStartLine(t *testing.T) {
	src := "x = '//not'; // one\n/* two\nlines */ y // three\r\n/* open"
	want := []Comment{
		{Line: 1, Text: "// one", Start: 13, End: 19},
		{Line: 2, Text: "/* two\nlines */", Start: 20, End: 35},
		{Line: 3, Text: "// three", Start: 38, End: 46},
		{Line: 4, Text: "/* open", Start: 48, End: 55},
	}
	if got := Comments(src, true); !reflect.DeepEqual(got, want) {
		t.Errorf("Comments = %#v, want %#v", got, want)
	}
}
