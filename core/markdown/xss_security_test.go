package markdown

import (
	"regexp"
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
	_, after, ok := strings.Cut(html, `id="`)
	if ok {
		after := after
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
// `&quot;`-escaped src value. That's not an injection, the HTML parser
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
	// "script:", a browser ignores it and resolves the URL to
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

// --- table syntax (post-xss-pass surface) ----------------------------------
//
// Property: table cell text flows through the SAME inline pipeline
// (code-span extraction, link/image scheme guards, HTML escaping) as
// paragraph text. The table renderer is newer than every test above, and
// a cell is attacker-controlled wherever markdown is user-submitted.
// Surfaces: <th> cells, <td> cells, image-with-quote payloads in cells.
func TestMarkdown_TableCellsEscapeInline(t *testing.T) {
	docs := []string{
		"| h |\n| --- |\n| <script>alert(1)</script> |",
		"| h |\n| --- |\n| [x](javascript:alert(1)) |",
		"| h |\n| --- |\n| ![o](x\" onerror=\"alert(1)) |",
		"| <img src=x onerror=alert(1)> |\n| --- |\n| c |",
	}
	for _, d := range docs {
		html := string(RenderHTML(d))
		if strings.Contains(html, "<script") || strings.Contains(html, "<img src=x") {
			t.Errorf("SECURITY: [markdown] table cell produced unescaped markup from %q: %s. "+
				"Attack: a cell bypasses the inline escape pipeline paragraphs are subject to.", d, html)
		}
		if strings.Contains(html, `href="javascript:`) {
			t.Errorf("SECURITY: [markdown] javascript: link survived in a table cell: %s", html)
		}
		// A quote smuggled into a URL must arrive escaped, never as a bare
		// `"` that would terminate the src attribute.
		if i := strings.Index(html, "<img"); i >= 0 {
			tag := html[i:]
			if end := strings.Index(tag, ">"); end >= 0 {
				tag = tag[:end]
				if strings.Contains(tag, `" onerror=`) {
					t.Errorf("SECURITY: [markdown] unescaped quote broke out of a cell image attribute: %s", tag)
				}
			}
		}
	}
	// Happy path: ordinary cell text still renders.
	if html := string(RenderHTML("| a | b |\n| --- | --- |\n| 1 | 2 |")); !strings.Contains(html, "<td>1</td>") {
		t.Errorf("[markdown] ordinary table broken: %s", html)
	}
}

// TestMarkdown_TableAlignClassFixedSet pins that the only attacker-influenced
// thing a delimiter row can put into the output is a class name from the
// fixed enum md-align-{left,right,center}: any other byte shape (letters,
// quotes, spaces-as-content) disqualifies the row from being a table at
// all, and the renderer maps the surviving colon shapes onto the enum.
func TestMarkdown_TableAlignClassFixedSet(t *testing.T) {
	classRe := regexp.MustCompile(`class="[^"]*"`)
	docs := map[string]string{
		"left":   "| h |\n| :--- |\n| c |",
		"right":  "| h |\n| ---: |\n| c |",
		"center": "| h |\n| :--: |\n| c |",
		"none":   "| h |\n| --- |\n| c |",
	}
	for want, d := range docs {
		html := string(RenderHTML(d))
		switch want {
		case "none":
			if strings.Contains(html, "md-align-") {
				t.Errorf("[markdown] plain delimiter row gained a class: %s", html)
			}
		default:
			if !strings.Contains(html, `class="md-align-`+want+`"`) {
				t.Errorf("[markdown] delimiter row for %s did not map onto the enum: %s", want, html)
			}
		}
		// Every class attribute the table renderer emits must come from the
		// fixed enum — no other bytes from the delimiter row survive.
		for _, m := range classRe.FindAllString(html, -1) {
			switch m {
			case `class="md-align-left"`, `class="md-align-right"`, `class="md-align-center"`:
			default:
				t.Errorf("SECURITY: [markdown] table emitted non-enum class %q from %q", m, d)
			}
		}
	}
	// Hostile delimiter rows: not tables at all, so no class attribute and
	// no <table> in which to carry the payload.
	for _, d := range []string{
		"| h |\n| :javascript:x: |\n| c |",
		"| h |\n| :\"onmouseover=\"alert(1): |\n| c |",
	} {
		html := string(RenderHTML(d))
		if strings.Contains(html, "<table") || strings.Contains(html, "class=\"") {
			t.Errorf("SECURITY: [markdown] hostile delimiter row became a table/class: %s. "+
				"Attack: the alignment cell writes into an attribute position.", html)
		}
	}
}

// TestMarkdown_LinkURLBoundaryShapes pins that parseLink's URL capture —
// which ends at the FIRST ')' and trims surrounding whitespace — always
// feeds the captured prefix through the scheme guard. Truncation and
// padding are how a payload tries to arrive "split" across the boundary;
// none of these shapes may yield an executable href.
func TestMarkdown_LinkURLBoundaryShapes(t *testing.T) {
	docs := []string{
		"[t](javascript:alert(1))x)",          // payload plus decoy ')' tail
		"[t]( javascript:alert(1) )",          // whitespace-padded scheme
		"[t](JaVaScRiPt:alert(1))",            // case-folded scheme
		"[t](data:image/svg+xml;base64,AAAA)", // svg data URI in a LINK
		"[t](vbscript:msgbox)",                // vbscript
	}
	for _, d := range docs {
		html := string(RenderHTML(d))
		lower := strings.ToLower(html)
		for _, scheme := range []string{`href="javascript:`, `href="vbscript:`, `href="data:image/svg`, `href="data:text/html`} {
			if strings.Contains(lower, scheme) {
				t.Errorf("SECURITY: [markdown] boundary-truncated URL smuggled %q past the scheme guard: %s", scheme, html)
			}
		}
	}
	// A genuinely safe URL must keep its href: the guard is a filter, not
	// a blanket replacement.
	if html := string(RenderHTML("[t](https://example.com/a(b))")); !strings.Contains(html, `href="https://example.com/a(b"`) {
		t.Errorf("[markdown] safe URL with interior paren mangled: %s", html)
	}
}

// TestMarkdown_FrontmatterDelimShapes pins that frontmatter content never
// reaches the rendered HTML as markup, regardless of delimiter shape:
// space-padded `--- ` delimiters still open/close the block, an UNCLOSED
// block renders as ordinary (escaped) body text rather than silently
// swallowing or executing the rest, and a `<script>` in a frontmatter
// value stays out of the HTML entirely.
func TestMarkdown_FrontmatterDelimShapes(t *testing.T) {
	// Padded delimiters: still frontmatter; script value never in HTML.
	doc := Render("--- \ntitle: \"<script>alert(1)</script>\"\n--- \nbody text\n")
	html := string(doc.HTML)
	if strings.Contains(html, "<script") {
		t.Errorf("SECURITY: [markdown] padded-delimiter frontmatter leaked markup into the body: %s", html)
	}
	if !strings.Contains(html, "body text") {
		t.Errorf("[markdown] padded delimiters swallowed the body: %q", html)
	}

	// Unclosed block: the leading `---` renders as a rule and the key: value
	// line as ESCAPED text — never raw markup.
	doc = Render("---\ntitle: \"<img src=x onerror=alert(1)>\"\nno closing delimiter")
	html = string(doc.HTML)
	if strings.Contains(html, "<img src=x") {
		t.Errorf("SECURITY: [markdown] unclosed frontmatter executed as body markup: %s", html)
	}
	// Frontmatter keys/values never appear raw even when they look like markdown.
	doc = Render("---\nx: [link](javascript:alert(1))\n---\nok")
	html = string(doc.HTML)
	if strings.Contains(html, `href="javascript:`) || strings.Contains(html, "<script") {
		t.Errorf("SECURITY: [markdown] frontmatter value rendered as live markup: %s", html)
	}
}

// Property: a frontmatter key defined twice must not silently resolve
// to the LAST value. splitFrontmatter writes fm[key] = val with no
// duplicate detection, so in
//
//	---
//	title: Safe Title
//	title: <img src=x onerror=alert(1)>
//	---
//
// Document.Title silently becomes the second line. The sibling YAML
// parser (core/yaml) rejects duplicate mapping keys precisely because
// silent last-wins lets a stale or hostile line override the value a
// reviewer believes is in force — and frontmatter is the same
// config-adjacent surface (Title flows into <title>, SEO meta, and
// templated page chrome). With no error channel on Render, the minimum
// fail-closed rule is: ambiguity must not resolve to the value a
// top-to-bottom reader did not see.
//
// Surfaces: the Frontmatter map itself and the Title fallback
// (markdown.go:28 reads fm["title"]), which is the value hosts
// interpolate into page chrome.
func TestFrontmatterDupKeysNotLastWins(t *testing.T) {
	doc := Render("---\ntitle: Safe Title\ntitle: EVIL<title>\n---\nbody text\n")
	if got := doc.Frontmatter["title"]; got != "Safe Title" {
		t.Errorf("SECURITY: [markdown] duplicate frontmatter key resolved to %q — silent last-wins lets a stale/hostile line override the value a reviewer read first.", got)
	}
	if strings.Contains(string(doc.HTML), "EVIL") {
		t.Errorf("SECURITY: [markdown] losing duplicate value leaked into the body: %s", doc.HTML)
	}
	// Title fallback reads the same map, so it inherits the defect.
	doc = Render("---\ntitle: First\ntitle: Second\n---\n\nNo heading here.\n")
	if doc.Title != "First" {
		t.Errorf("SECURITY: [markdown] Document.Title resolved duplicate frontmatter title to %q, want the first definition", doc.Title)
	}
	// False-positive guard: distinct keys keep parsing, last-wins has no
	// excuse to be reachable via them.
	doc = Render("---\ntitle: Only\nauthor: Don\n---\n\nNo heading here.\n")
	if doc.Title != "Only" {
		t.Errorf("single title key mis-parsed: %q", doc.Title)
	}
}
