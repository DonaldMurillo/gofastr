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

// Textarea must preserve leading LF and CR through HTML parsing.
// The full newline case table lives in core-ui/html.
func TestTextareaKeepsLeadingNewline(t *testing.T) {
	const want = "\nfirst line after a blank one"
	for _, value := range []string{want, "\r" + want[1:]} {
		markup := string(Textarea(TextareaProps{Name: "body", Value: value}, nil))
		if got := parsedTextareaValue(t, markup); got != want {
			t.Errorf("value %q: the browser's textarea holds %q, want %q (markup %q)", value, got, want, markup)
		}
	}
}
