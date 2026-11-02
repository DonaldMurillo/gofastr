package headless

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// parsedTextareaValue is what a browser's textarea holds after parsing
// markup: the HTML parser drops one newline straight after <textarea>.
func parsedTextareaValue(t *testing.T, markup string) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader("<!doctype html><body>" + markup))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "textarea" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				got += c.Data
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return got
}

// Textarea wrote the Value straight after the start tag, so a value
// that begins with a newline (a markdown body opening on a blank line,
// a code block) lost it every time the form was rendered and saved.
func TestTextareaKeepsLeadingNewline(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"\nfirst line after a blank one", "\nfirst line after a blank one"},
		{"\n\ntwo blank lines", "\n\ntwo blank lines"},
		// The tokenizer folds CRLF and a lone CR to LF before the drop.
		{"\r\nwindows", "\nwindows"},
		{"\rold mac", "\nold mac"},
		{"no newline", "no newline"},
		{"", ""},
	} {
		markup := string(Textarea(TextareaProps{Name: "body", Value: tc.value}, nil))
		if got := parsedTextareaValue(t, markup); got != tc.want {
			t.Errorf("value %q: the browser's textarea holds %q, want %q (markup %q)", tc.value, got, tc.want, markup)
		}
	}
}
