package sqllex

import "testing"

func TestSkipQuoted(t *testing.T) {
	for _, c := range []struct {
		in      string
		escapes bool
		want    int
	}{
		{`'a''b' x`, false, 6},
		{`'a\'b' x`, true, 6},
		{`'a\'b' x`, false, 4},
		{`"q""r" x`, false, 6},
		{`'open`, false, 5},
	} {
		if got := SkipQuoted(c.in, 0, c.escapes); got != c.want {
			t.Errorf("SkipQuoted(%q, escapes=%v) = %d, want %d", c.in, c.escapes, got, c.want)
		}
	}
}

func TestSkipBlockComment(t *testing.T) {
	in := "/* a /* b */ c */ d"
	if got := SkipBlockComment(in, 0, true); got != 17 {
		t.Errorf("nested = %d, want 17", got)
	}
	if got := SkipBlockComment(in, 0, false); got != 12 {
		t.Errorf("flat = %d, want 12", got)
	}
	if got := SkipBlockComment("/* open", 0, true); got != 7 {
		t.Errorf("unterminated = %d, want 7", got)
	}
}

func TestDollarTag(t *testing.T) {
	for in, want := range map[string]string{
		"$$ x":      "$$",
		"$tag$ x":   "$tag$",
		"$t1$ x":    "$t1$",
		"$é$ x":     "$é$",
		"$1 x":      "",
		"$1$":       "",
		"$a b$":     "",
		"$unclosed": "",
	} {
		if got := DollarTag(in); got != want {
			t.Errorf("DollarTag(%q) = %q, want %q", in, got, want)
		}
	}
}
