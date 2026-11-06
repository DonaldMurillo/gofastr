package ui

import (
	"io"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestForm_CSRFTokenInjection(t *testing.T) {
	for _, token := range []string{`" onclick="alert(1)`, `'><script>alert(2)</script>&`} {
		markup := string(csrfHiddenInput(token))
		parser := html.NewTokenizer(strings.NewReader(markup))
		if parser.Next() != html.StartTagToken {
			t.Fatalf("expected hidden input: %s", markup)
		}
		input := parser.Token()
		attributes := map[string]string{}
		for _, attribute := range input.Attr {
			attributes[attribute.Key] = attribute.Val
		}
		if input.Data != "input" || len(input.Attr) != 3 || attributes["type"] != "hidden" ||
			attributes["name"] != "_csrf" || attributes["value"] != token {
			t.Fatalf("token escaped its value attribute: %+v", input)
		}
		if parser.Next() != html.ErrorToken || parser.Err() != io.EOF {
			t.Fatalf("extra markup after hidden input: %s", markup)
		}
	}
}
