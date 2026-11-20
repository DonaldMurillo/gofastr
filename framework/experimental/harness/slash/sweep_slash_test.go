package slash

import "testing"

func TestParseEscapeSequences(t *testing.T) {
	c, err := Parse(`/x "a\nb\tc\\d\"e" \q`)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Args) != 2 || c.Args[0] != "a\nb\tc\\d\"e" || c.Args[1] != "q" {
		t.Fatalf("Args = %q", c.Args)
	}
}
