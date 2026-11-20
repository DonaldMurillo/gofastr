package dotenv

import (
	"strings"
	"testing"
)

// TestParseStripsLeadingBOM pins that a UTF-8 BOM before the first key is not part of the key.
func TestParseStripsLeadingBOM(t *testing.T) {
	got, err := Parse(strings.NewReader("\ufeffFOO=bar\nBAZ=qux\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got["FOO"] != "bar" || got["BAZ"] != "qux" {
		t.Fatalf("got %q", got)
	}
}
