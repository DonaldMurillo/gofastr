package html

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// The HTML parser drops one newline straight after <textarea>, so a
// value that opened on a line break lost it on every render and save.
func TestTextAreaKeepsLeadingNewline(t *testing.T) {
	for value, want := range map[string]string{
		"\nafter a blank line": "\nafter a blank line",
		"\n\ntwo blank lines":  "\n\ntwo blank lines",
		"\r\nwindows":          "\nwindows",
		"\rold mac":            "\nold mac",
		"plain":                "plain",
	} {
		markup := string(TextArea(TextAreaConfig{Name: "body", Content: value}))
		z := xhtml.NewTokenizer(strings.NewReader(markup))
		z.Next() // the <textarea> start tag
		var got string
		if z.Next() == xhtml.TextToken {
			got = string(z.Text())
		}
		// The tokenizer keeps the newline the tree builder drops.
		got = strings.TrimPrefix(got, "\n")
		if got != want {
			t.Errorf("content %q: the browser's textarea holds %q, want %q (markup %q)", value, got, want, markup)
		}
	}
}
