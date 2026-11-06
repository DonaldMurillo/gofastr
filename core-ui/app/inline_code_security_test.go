package app

import (
	"strings"
	"testing"
)

func TestInlineCodeDecodesExactlyOnce(t *testing.T) {
	for _, test := range []struct{ html, want string }{
		{`<p>Use <code>&lt;div&gt;</code> here</p>`, "Use `<div>` here"},
		{`<code>&lt;widget data-x=&quot;a&quot;&gt;</code>`, "`<widget data-x=\"a\">`"},
		{`<code>&amp;lt;aside&amp;gt;</code>`, "`&lt;aside&gt;`"},
		{`<code>&lt;script&gt;` + "`" + `&lt;/script&gt;</code><strong>safe</strong>`, "``<script>`</script>``**safe**"},
		{`<table><tr><td><code>&lt;a|b&gt;</code></td></tr></table>`, "`<a\\|b>`"},
	} {
		if got := htmlToMarkdown(test.html); !strings.Contains(got, test.want) {
			t.Errorf("html=%s got=%s want=%s", test.html, got, test.want)
		}
	}
}
