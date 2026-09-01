package a

import "testing"

func TestDocType(t *testing.T) {
	cases := []struct {
		ext  string
		want bool
	}{
		{"html", true},
		{"svg", true},
		{"nope", false},
	}
	for _, c := range cases {
		if got := isAllowedDocType(c.ext); got != c.want {
			t.Fatalf("ext %q: got %v", c.ext, got)
		}
	}
}

func TestExtOK(t *testing.T) {
	if !extOK("png") {
		t.Fatal("png must be ok")
	}
}

func TestSchemes(t *testing.T) {
	for _, s := range []string{"http", "https", "mailto"} {
		if !schemeAllowed(s) {
			t.Fatalf("scheme %s must be allowed", s)
		}
	}
}
