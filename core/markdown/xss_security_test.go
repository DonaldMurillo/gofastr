package markdown

import (
	"strings"
	"testing"
)

// TestMarkdown_XSSInLinkTarget verifies that malicious JavaScript URLs in
// markdown links are not rendered as clickable. Attack: [click](javascript:alert(1)).
func TestMarkdown_XSSInLinkTarget(t *testing.T) {
	doc := Render(`[click me](javascript:alert(1))`)
	html := string(doc.HTML)
	if strings.Contains(html, `href="javascript:alert(1)"`) {
		t.Errorf("SECURITY: [markdown] javascript: URL rendered in link href: %s. Attack: XSS via markdown link.", html)
	}
}

// TestMarkdown_XSSInImageData verifies that JavaScript URLs in image
// sources are not rendered. Attack: ![img](javascript:alert(1)).
func TestMarkdown_XSSInImageData(t *testing.T) {
	doc := Render(`![img](javascript:alert(1))`)
	html := string(doc.HTML)
	if strings.Contains(html, `src="javascript:alert(1)"`) {
		t.Errorf("SECURITY: [markdown] javascript: URL rendered in img src: %s. Attack: XSS via markdown image.", html)
	}
}

// TestMarkdown_HTMLTagEscaped verifies that raw HTML tags in markdown are
// escaped. Attack: injecting <script> tags via markdown content.
func TestMarkdown_HTMLTagEscaped(t *testing.T) {
	input := `Hello <script>alert('xss')</script> world`
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, "<script>") {
		t.Errorf("SECURITY: [markdown] raw <script> tag not escaped: %s. Attack: XSS via HTML injection in markdown.", html)
	}
}

// TestMarkdown_CodeBlockEscaped verifies that code blocks escape HTML.
// Attack: injecting HTML inside fenced code blocks.
func TestMarkdown_CodeBlockEscaped(t *testing.T) {
	input := "```\n<script>alert('xss')</script>\n```"
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, "<script>alert") && !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("SECURITY: [markdown] HTML not escaped in code block: %s. Attack: XSS via code block content.", html)
	}
}

// TestMarkdown_InlineCodeEscaped verifies that inline code escapes HTML.
// Attack: `<script>` in backticks.
func TestMarkdown_InlineCodeEscaped(t *testing.T) {
	input := "Here is `<script>alert(1)</script>` inline code"
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, "<script>alert") && !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("SECURITY: [markdown] HTML not escaped in inline code: %s. Attack: XSS via inline code.", html)
	}
}

// TestMarkdown_HeadingIDSanitized verifies that auto-generated heading IDs
// don't contain dangerous characters. Attack: XSS via heading ID attribute.
func TestMarkdown_HeadingIDSanitized(t *testing.T) {
	input := `# Hello "World" <script>`
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, `<script>`) {
		t.Errorf("SECURITY: [markdown] heading ID contains unescaped HTML: %s. Attack: XSS via heading ID attribute.", html)
	}
	idStart := strings.Index(html, `id="`)
	if idStart >= 0 {
		after := html[idStart+len(`id="`):]
		idEnd := strings.Index(after, `"`)
		if idEnd > 0 {
			id := after[:idEnd]
			if strings.ContainsAny(id, `"<>&'`) {
				t.Errorf("SECURITY: [markdown] heading ID %q contains dangerous characters. Attack: attribute injection.", id)
			}
		}
	}
}

// TestMarkdown_ImageEventHandlers verifies that an attempt to smuggle
// an event handler through an image src is neutralised by attribute
// escaping. Attack: ![a](" onerror="alert(1)).
//
// The literal substring `onerror=` may still appear inside a properly
// `&quot;`-escaped src value — that's not an injection, the HTML parser
// will read it as a single string. What matters is that no UNESCAPED
// `"` survives the escape pass, since an unescaped quote is what would
// terminate the attribute and let the next token become a new
// attribute. So this test asserts there is no bare `"` inside the
// rendered `<img …>` tag past the opening `src="`.
func TestMarkdown_ImageEventHandlers(t *testing.T) {
	input := `![a](" onerror="alert(1))`
	doc := Render(input)
	html := string(doc.HTML)

	imgStart := strings.Index(html, "<img ")
	if imgStart < 0 {
		t.Fatalf("expected <img> in output: %s", html)
	}
	rest := html[imgStart:]
	imgEnd := strings.Index(rest, ">")
	if imgEnd < 0 {
		t.Fatalf("malformed <img> in output: %s", html)
	}
	tag := rest[:imgEnd+1]
	// A correctly-escaped tag has exactly two double-quote pairs
	// (src="..." and alt="..."). More than 4 raw `"` characters means
	// a value escaped out and started a new attribute.
	if n := strings.Count(tag, `"`); n != 4 {
		t.Errorf("SECURITY: [markdown] image tag has %d unescaped quotes (want 4): %s. Attack: src breakout via unescaped quote.", n, tag)
	}
}

// TestMarkdown_DataURIImage verifies that data: URIs in images are handled.
// Attack: data:text/html,... for XSS via images.
func TestMarkdown_DataURIImage(t *testing.T) {
	input := `![xss](data:text/html,<script>alert(1)</script>)`
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, `src="data:text/html,<script>`) {
		t.Errorf("SECURITY: [markdown] data:text/html URI in image src: %s. Attack: XSS via data URI.", html)
	}
}

// TestMarkdown_FrontmatterNotInOutput verifies that frontmatter is stripped
// from rendered output. Attack: leaking configuration via frontmatter.
func TestMarkdown_FrontmatterNotInOutput(t *testing.T) {
	input := "---\nsecret: super-secret-key\npassword: admin123\n---\n# Hello"
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, "super-secret-key") || strings.Contains(html, "admin123") {
		t.Errorf("SECURITY: [markdown] frontmatter leaked into HTML output: %s. Attack: config disclosure.", html)
	}
}

// TestMarkdown_LinkDataURIVerify verifies that data: URIs in links are
// handled safely. Attack: [click](data:text/html,<script>...).
func TestMarkdown_LinkDataURIVerify(t *testing.T) {
	input := `[click](data:text/html,<h1>Hello</h1>)`
	doc := Render(input)
	html := string(doc.HTML)
	if strings.Contains(html, `href="data:text/html`) {
		t.Errorf("SECURITY: [markdown] data:text/html URI in link href: %s. Attack: XSS via data URI link.", html)
	}
}

// TestMarkdown_SchemeInteriorControlChar verifies that control bytes
// embedded INSIDE a scheme name (which the HTML5 URL parser strips
// before scheme resolution) cannot smuggle a javascript: URL past the
// dangerous-scheme allow-list. Attack: [x](java<TAB>script:alert(1)).
func TestMarkdown_SchemeInteriorControlChar(t *testing.T) {
	// Each attack embeds a different control byte between "java" and
	// "script:" — a browser ignores it and resolves the URL to
	// javascript:, so the renderer must neutralise all of them.
	for _, ctrl := range []string{"\t", "\n", "\r", "\x00"} {
		link := "[x](java" + ctrl + "script:alert(1))"
		img := "![x](java" + ctrl + "script:alert(1))"
		for _, src := range []string{link, img} {
			html := string(Render(src).HTML)
			// The control byte must not survive into the href/src value
			// such that the residual reads as "javascript:" once stripped.
			deScripted := strings.ReplaceAll(html, ctrl, "")
			if strings.Contains(strings.ToLower(deScripted), "java") &&
				strings.Contains(strings.ToLower(deScripted), "script:alert") &&
				!strings.Contains(deScripted, `="#"`) {
				t.Errorf("SECURITY: [markdown] interior control byte %q smuggled javascript: scheme: %s", ctrl, html)
			}
		}
	}
}

// TestMarkdown_FenceInfoAttrEscaped verifies the fenced-code-block info
// string cannot break out of the class attribute into element context.
// Attack: ```"><img src=x onerror=alert(1)> as the fence info string.
func TestMarkdown_FenceInfoAttrEscaped(t *testing.T) {
	// Happy path: a normal language identifier renders a clean class.
	clean := string(Render("```go\nx := 1\n```").HTML)
	if !strings.Contains(clean, `class="language-go"`) {
		t.Errorf("expected clean language class, got: %s", clean)
	}

	for _, info := range []string{
		`"><img src=x onerror=alert(1)>`,
		`go"><script>alert(1)</script>`,
		`x" onmouseover="alert(1)`,
	} {
		html := string(Render("```" + info + "\nbody\n```").HTML)
		if strings.Contains(html, "<img") || strings.Contains(html, "<script") {
			t.Errorf("SECURITY: [markdown] fence info string broke out of class attribute: %s. Attack: XSS via info string.", html)
		}
	}
}
