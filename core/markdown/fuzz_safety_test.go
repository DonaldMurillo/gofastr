package markdown

import (
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

// mdTags and mdAttrs are everything the renderer emits. Any other tag
// or attribute in the tokenized output is markup the input smuggled
// through the escaper.
var mdTags = map[string]bool{
	"p": true, "a": true, "img": true, "code": true, "pre": true, "em": true,
	"strong": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "ul": true, "ol": true, "li": true, "blockquote": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true,
	"td": true, "hr": true, "br": true,
}

var mdAttrs = map[string]bool{
	"href": true, "src": true, "alt": true, "id": true, "class": true,
	"tabindex": true, "data-meta": true,
}

// checkRenderedSafe tokenizes out the way a browser does and fails on a
// tag or attribute the renderer never writes, or a URL attribute whose
// decoded value the urlsafe policy for that sink refuses.
func checkRenderedSafe(t *testing.T, input, out string) {
	t.Helper()
	z := html.NewTokenizer(strings.NewReader(out))
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return
		case html.CommentToken, html.DoctypeToken:
			t.Fatalf("input %q produced %v in output %q", input, tt, out)
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			tok := z.Token()
			if !mdTags[tok.Data] {
				t.Fatalf("input %q produced tag <%s> in output %q", input, tok.Data, out)
			}
			for _, a := range tok.Attr {
				if !mdAttrs[a.Key] {
					t.Fatalf("input %q produced attribute %s on <%s> in output %q", input, a.Key, tok.Data, out)
				}
				switch a.Key {
				case "href":
					if a.Val != "#" && !urlsafe.OK(a.Val, urlsafe.Anchor) {
						t.Fatalf("input %q produced href %q in output %q", input, a.Val, out)
					}
				case "src":
					if a.Val != "#" && !urlsafe.OK(a.Val, urlsafe.ImageSource) {
						t.Fatalf("input %q produced src %q in output %q", input, a.Val, out)
					}
				}
			}
		}
	}
}

// FuzzRenderHTMLSafe holds the renderer to its output contract, not only
// liveness: whatever the input, the HTML a browser tokenizes from the
// output carries no tag, attribute or URL scheme the renderer does not
// write itself.
func FuzzRenderHTMLSafe(f *testing.F) {
	for _, s := range []string{
		"<script>alert(1)</script>", "[x](javascript:alert(1))",
		"![x](data:text/html,<script>)", "[x](java\tscript:alert(1))",
		"[x](&#106;avascript:alert(1))", "[x](\"onmouseover=alert(1) x=\")",
		"```\"><img src=x onerror=alert(1)>\nx\n```", "# <img src=x>",
		"| a | b |\n|:-|-:|\n| <b> | c |", "[a](/\\evil.com)",
		"[![a](x)](javascript:1)", "[a](//evil.com)", "<!-- x -->",
		"![a](data:image/png;base64,AAAA\" onerror=\"x)", "`<a>`", "*<i>*",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		checkRenderedSafe(t, input, string(RenderHTML(input)))
	})
}
