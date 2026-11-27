package registry

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestInjectIntoSimpleDiv(t *testing.T) {
	got, err := injectMarker(`<div>hi</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `<div data-cui-comp="modal">`) {
		t.Errorf("got %s", got)
	}
}

func TestInjectMarkerEscapesName(t *testing.T) {
	got, err := injectMarker(`<div>hi</div>`, `modal">`)
	if err != nil {
		t.Fatal(err)
	}
	const want = `<div data-cui-comp="modal&#34;&gt;">hi</div>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInjectIntoDivWithAttrs(t *testing.T) {
	got, err := injectMarker(`<div class="x" id="y">hi</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-cui-comp="modal"`) {
		t.Errorf("got %s", got)
	}
	if !strings.Contains(string(got), `class="x"`) {
		t.Errorf("class lost: %s", got)
	}
}

func TestInjectIntoSelfClosingTag(t *testing.T) {
	got, err := injectMarker(`<img src="a.png" />`, "logo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-cui-comp="logo"`) {
		t.Errorf("got %s", got)
	}
	if !strings.Contains(string(got), `/>`) {
		t.Errorf("self-close marker lost: %s", got)
	}
}

func TestInjectIntoSemanticTag(t *testing.T) {
	got, err := injectMarker(`<section role="banner"><h1>Hi</h1></section>`, "page-header")
	if err != nil {
		t.Fatal(err)
	}
	want := `<section role="banner" data-cui-comp="page-header">`
	if !strings.Contains(string(got), want) {
		t.Errorf("got %s want substring %q", got, want)
	}
}

func TestInjectIntoFragmentLeadingWhitespace(t *testing.T) {
	got, err := injectMarker("\n\t<div>x</div>", "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-cui-comp="modal"`) {
		t.Errorf("got %s", got)
	}
}

func TestInjectSkipsLeadingComment(t *testing.T) {
	got, err := injectMarker(`<!-- intro --><div>x</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-cui-comp="modal"`) {
		t.Errorf("got %s", got)
	}
}

func TestInjectRejectsBareText(t *testing.T) {
	_, err := injectMarker(`plain text`, "modal")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "single rooted element") {
		t.Errorf("error message should hint at fix: %v", err)
	}
}

func TestInjectRejectsEmptyName(t *testing.T) {
	_, err := injectMarker(`<div></div>`, "")
	if err == nil {
		t.Fatal("expected error on empty name")
	}
}

func TestInjectIdempotentWhenAlreadyMarked(t *testing.T) {
	in := `<div data-cui-comp="modal" class="x">hi</div>`
	out, err := injectMarker(in, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("idempotent re-injection altered html:\nin:  %s\nout: %s", in, out)
	}
	count := strings.Count(string(out), `data-cui-comp=`)
	if count != 1 {
		t.Errorf("got %d data-cui-comp attrs, want 1", count)
	}
}

// TestInjectSelfClosingPreservesSpace asserts that a self-closing
// tag with a space before /> retains that space after marker
// injection, otherwise `<br />` becomes `<br data-cui-comp="…"/>`
// which is technically valid but visually inconsistent and
// regression-prone for downstream HTML normalizers.
func TestInjectSelfClosingPreservesSpace(t *testing.T) {
	out, err := injectMarker(`<br />`, "spacer")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, " />") {
		t.Errorf("self-closing space lost: got %q", s)
	}
	if !strings.Contains(s, `data-cui-comp="spacer"`) {
		t.Errorf("marker not injected: got %q", s)
	}
}

// TestInjectAttrWithEmbeddedGreaterThan asserts findOpenTagEnd
// doesn't terminate the opening tag early when a `>` lives inside
// a quoted attribute value (`<a title="a > b">`). If it did, the
// helper would inject the marker after the bogus `>` (inside the
// element body) and corrupt the markup.
func TestInjectAttrWithEmbeddedGreaterThan(t *testing.T) {
	in := `<a title="a > b">hi</a>`
	out, err := injectMarker(in, "tip")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `data-cui-comp="tip"`) {
		t.Errorf("marker not injected: %q", s)
	}
	if !strings.Contains(s, `title="a > b"`) {
		t.Errorf("attribute corrupted: %q", s)
	}
	// Marker must be inside the opening tag, BEFORE the > that closes
	// the element's open tag (not after the > inside the title attr).
	endOfOpen := strings.Index(s, `">hi</a>`)
	if endOfOpen < 0 {
		t.Fatalf("element body misaligned: %q", s)
	}
	markerIdx := strings.Index(s, `data-cui-comp`)
	if markerIdx >= endOfOpen {
		t.Errorf("marker spliced AFTER the real `>` — embedded `>` confused tag-end detector: %q", s)
	}
}

// TestInjectIgnoresAttrNameInsideQuotedValue guards against a false-positive
// in the idempotence check: if "data-cui-comp" appears inside a quoted
// attribute value, hasAttribute() must NOT treat it as already-present,
// otherwise injectMarker silently skips marker injection.
func TestInjectIgnoresAttrNameInsideQuotedValue(t *testing.T) {
	cases := []string{
		// Substring in class="..." value
		`<div class="x data-cui-comp x">hi</div>`,
		// Substring in title="..." value
		`<div title="data-cui-comp inside">hi</div>`,
		// Single-quoted attr value
		`<div data-foo='data-cui-comp'>hi</div>`,
	}
	for _, in := range cases {
		out, err := injectMarker(in, "modal")
		if err != nil {
			t.Errorf("input %q: unexpected error %v", in, err)
			continue
		}
		count := strings.Count(string(out), `data-cui-comp="modal"`)
		if count != 1 {
			t.Errorf("input %q: expected exactly 1 data-cui-comp=\"modal\" attr, got %d in output:\n%s", in, count, out)
		}
	}
}

// TestInjectIdempotentAcrossLineBreaks guards against the bug where
// the idempotence check only matched ` data-cui-comp` or `\tdata-cui-comp`,
// missing `\ndata-cui-comp` / `\rdata-cui-comp`. Multi-line opening
// tags (common in handwritten templates) would get a duplicate marker.
func TestInjectIdempotentAcrossLineBreaks(t *testing.T) {
	cases := []string{
		// Bare newline / CR directly before the attribute, no space
		// indent, so only an \n / \r boundary distinguishes the attr.
		"<div\ndata-cui-comp=\"modal\"\nclass=\"x\">hi</div>",
		"<div\rdata-cui-comp=\"modal\"\rclass=\"x\">hi</div>",
		// And the indented cases that already work, keep them as a
		// regression net.
		"<div\n  data-cui-comp=\"modal\"\n  class=\"x\">hi</div>",
		"<div\r\n  data-cui-comp=\"modal\"\r\n  class=\"x\">hi</div>",
	}
	for i, in := range cases {
		out, err := injectMarker(in, "modal")
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		count := strings.Count(string(out), `data-cui-comp=`)
		if count != 1 {
			t.Errorf("case %d: got %d data-cui-comp attrs, want 1 (multi-line opening tag should be idempotent)", i, count)
		}
	}
}

func TestInjectSkipsWhenWrappedByDifferentName(t *testing.T) {
	// Composition: outer Style wraps inner Style's already-wrapped
	// output. The outer marker would normally win, but with the
	// existing-marker guard we conservatively don't inject again.
	// (Authors should compose at the Style.Render level, not double-
	// wrap pre-rendered HTML.)
	in := `<div data-cui-comp="inner">hi</div>`
	out, _ := injectMarker(in, "outer")
	if strings.Count(string(out), `data-cui-comp=`) != 1 {
		t.Errorf("double-wrap should leave 1 marker; got %s", out)
	}
}

func TestInjectAttribute(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		attr    string
		value   string
		want    string
		wantErr string
	}{
		{
			name:  "basic",
			html:  `<div class="x">body</div>`,
			attr:  "data-cui-scope",
			value: "board",
			want:  `<div class="x" data-cui-scope="board">body</div>`,
		},
		{
			name:  "no attributes yet",
			html:  `<main></main>`,
			attr:  "data-cui-comp",
			value: "issuecard",
			want:  `<main data-cui-comp="issuecard"></main>`,
		},
		{
			name:  "self-closing keeps spacing",
			html:  `<br />`,
			attr:  "data-cui-comp",
			value: "i",
			want:  `<br data-cui-comp="i" />`,
		},
		{
			name:  "self-closing tight",
			html:  `<br/>`,
			attr:  "data-cui-comp",
			value: "i",
			want:  `<br data-cui-comp="i"/>`,
		},
		{
			name:  "leading comment skipped",
			html:  `<!-- c --><div>x</div>`,
			attr:  "data-cui-scope",
			value: "b",
			want:  `<!-- c --><div data-cui-scope="b">x</div>`,
		},
		{
			name:  "value escaped",
			html:  `<div></div>`,
			attr:  "title",
			value: `a"b<c>&`,
			want:  `<div title="a&#34;b&lt;c&gt;&amp;"></div>`,
		},
		{
			name:  "idempotent",
			html:  `<div data-cui-scope="b" class="x"></div>`,
			attr:  "data-cui-scope",
			value: "other",
			want:  `<div data-cui-scope="b" class="x"></div>`,
		},
		{
			name:  "quoted mentions do not count as present",
			html:  `<div class="x data-cui-scope x"></div>`,
			attr:  "data-cui-scope",
			value: "b",
			want:  `<div class="x data-cui-scope x" data-cui-scope="b"></div>`,
		},
		{
			name:    "fragment",
			html:    `hello`,
			attr:    "data-cui-scope",
			value:   "b",
			wantErr: "must begin with an element open tag",
		},
		{
			name:    "closing tag",
			html:    `</div>`,
			attr:    "data-cui-scope",
			value:   "b",
			wantErr: "must begin with an element open tag",
		},
		{
			name:    "unterminated",
			html:    `<div class="x`,
			attr:    "data-cui-scope",
			value:   "b",
			wantErr: "unterminated open tag",
		},
		{
			name:  "a name after title= is title's value",
			html:  `<div title= data-cui-scope>x</div>`,
			attr:  "data-cui-scope",
			value: "b",
			want:  `<div title= data-cui-scope data-cui-scope="b">x</div>`,
		},
		{
			name:  "an empty comment before the root",
			html:  `<!--><div>x</div>`,
			attr:  "data-cui-scope",
			value: "b",
			want:  `<!--><div data-cui-scope="b">x</div>`,
		},
		{
			name:  "a dash-only comment before the root",
			html:  `<!---><div>x</div>`,
			attr:  "data-cui-scope",
			value: "b",
			want:  `<!---><div data-cui-scope="b">x</div>`,
		},
		{
			name:    "bad attribute name",
			html:    `<div></div>`,
			attr:    `a"b`,
			value:   "x",
			wantErr: "not a valid attribute name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InjectAttribute(render.HTML(tc.html), tc.attr, tc.value)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestAttribute(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		attr    string
		wantVal string
		wantOK  bool
		wantErr string
	}{
		{"double quoted", `<div data-cui-scope="board" class="x"></div>`, "data-cui-scope", "board", true, ""},
		{"single quoted", `<div data-cui-scope='board'></div>`, "data-cui-scope", "board", true, ""},
		{"unquoted", `<div data-cui-scope=board></div>`, "data-cui-scope", "board", true, ""},
		{"absent", `<div class="x"></div>`, "data-cui-scope", "", false, ""},
		{"mention in value only", `<div class="data-cui-scope" data-x="1"></div>`, "data-cui-scope", "", false, ""},
		{"prefix collision", `<div data-cui-scopey="1"></div>`, "data-cui-scope", "", false, ""},
		{"suffix collision", `<div x-data-cui-scope="1"></div>`, "data-cui-scope", "", false, ""},
		{"valueless", `<div data-cui-scope></div>`, "data-cui-scope", "", true, ""},
		{"spaces around the equals sign", `<div data-cui-scope = "board"></div>`, "data-cui-scope", "board", true, ""},
		{"form feeds around the equals sign", "<div data-cui-scope\f=\f\"board\"></div>", "data-cui-scope", "board", true, ""},
		{"upper-case name", `<div DATA-CUI-SCOPE="board"></div>`, "data-cui-scope", "board", true, ""},
		{"a long s does not fold to s", `<div data-cui-ſcope="board"></div>`, "data-cui-scope", "", false, ""},
		{"mixed-case name", `<div Data-Cui-Scope=board></div>`, "data-cui-scope", "board", true, ""},
		{"a name after title= is title's value", `<button title= aria-describedby=x></button>`, "aria-describedby", "", false, ""},
		{"after a slash", `<div/data-cui-scope="b"></div>`, "data-cui-scope", "b", true, ""},
		{"straight after a closing quote", `<div class="y"data-cui-scope="b"></div>`, "data-cui-scope", "b", true, ""},
		{"quotes inside an unquoted value", `<div data-cui-scope=a"b"></div>`, "data-cui-scope", `a"b"`, true, ""},
		{"valueless before another attribute", `<div data-cui-scope class="x"></div>`, "data-cui-scope", "", true, ""},
		{"after leading comment", `<!-- c --><div data-cui-scope="b"></div>`, "data-cui-scope", "b", true, ""},
		{"fragment", `hello`, "data-cui-scope", "", false, "must begin with an element open tag"},
		{"unterminated", `<div class="x`, "data-cui-scope", "", false, "unterminated open tag"},
		{"bad name", `<div></div>`, `a"b`, "", false, "not a valid attribute name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, ok, err := Attribute(render.HTML(tc.html), tc.attr)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if ok != tc.wantOK || v != tc.wantVal {
				t.Fatalf("got (%q, %t), want (%q, %t)", v, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestAddAttributeToken(t *testing.T) {
	cases := []struct {
		name, html, token, want, wantErr string
	}{
		{name: "no attribute yet", html: `<button type="button">Save</button>`, token: "tip",
			want: `<button type="button" aria-describedby="tip">Save</button>`},
		{name: "appends to an existing list", html: `<button aria-describedby="help">Save</button>`, token: "tip",
			want: `<button aria-describedby="help tip">Save</button>`},
		{name: "single quotes and extra spaces", html: `<input aria-describedby=' help  err ' />`, token: "tip",
			want: `<input aria-describedby=' help  err tip' />`},
		{name: "spaces around the equals sign", html: `<button aria-describedby = "help">Save</button>`, token: "tip",
			want: `<button aria-describedby = "help tip">Save</button>`},
		{name: "a line break before an unquoted value", html: "<button aria-describedby=\n help>Save</button>", token: "tip",
			want: `<button aria-describedby="help tip">Save</button>`},
		{name: "form feeds around the equals sign", html: "<button aria-describedby\f=\f\"help\">Save</button>", token: "tip",
			want: "<button aria-describedby\f=\f\"help tip\">Save</button>"},
		{name: "a form feed before the name", html: "<button\faria-describedby=\"help\">Save</button>", token: "tip",
			want: "<button\faria-describedby=\"help tip\">Save</button>"},
		{name: "a non-breaking space inside an id", html: `<button aria-describedby="help&#160;details">Save</button>`, token: "tip",
			want: `<button aria-describedby="help&#160;details tip">Save</button>`},
		{name: "an id an HTML text decoder would rewrite", html: `<button aria-describedby="help&copy=details">Save</button>`, token: "tip",
			want: `<button aria-describedby="help&copy=details tip">Save</button>`},
		{name: "a token equal to the undecoded bytes is not listed", html: `<button aria-describedby="t&#105;p">Save</button>`, token: "t&#105;p",
			want: `<button aria-describedby="t&#105;p t&amp;#105;p">Save</button>`},
		{name: "a semicolonless reference stays literal for the duplicate check", html: `<button aria-describedby="help&copy=x">Save</button>`, token: "help&copy=x",
			want: `<button aria-describedby="help&copy=x">Save</button>`},
		{name: "a semicolonless reference is not decoded as text", html: `<button aria-describedby="help&copy=x">Save</button>`, token: "help\u00a9=x",
			want: "<button aria-describedby=\"help&copy=x help\u00a9=x\">Save</button>"},
		{name: "already listed as character references", html: `<button aria-describedby="help t&#105;p">Save</button>`, token: "tip",
			want: `<button aria-describedby="help t&#105;p">Save</button>`},
		{name: "quotes inside an unquoted value stay inside it", html: `<button aria-describedby=a"onfocus=alert(1)//"x>Save</button>`, token: "tip",
			want: `<button aria-describedby="a&quot;onfocus=alert(1)//&quot;x tip">Save</button>`},
		{name: "an unquoted value ending in a quote", html: `<button aria-describedby=a"b">Save</button>`, token: "tip",
			want: `<button aria-describedby="a&quot;b&quot; tip">Save</button>`},
		{name: "a name after title= is title's value", html: `<button title= aria-describedby>Save</button>`, token: "tip",
			want: `<button title= aria-describedby aria-describedby="tip">Save</button>`},
		{name: "an attribute after a slash", html: `<button/aria-describedby="x">Save</button>`, token: "tip",
			want: `<button/aria-describedby="x tip">Save</button>`},
		{name: "an attribute straight after a closing quote", html: `<button class="y"aria-describedby="x">Save</button>`, token: "tip",
			want: `<button class="y"aria-describedby="x tip">Save</button>`},
		{name: "a slash ending an unquoted value is not a self-close", html: `<input class=x/>`, token: "tip",
			want: `<input class=x/ aria-describedby="tip">`},
		{name: "a dangling equals sign keeps its empty value", html: `<button title=>Save</button>`, token: "tip",
			want: `<button title="" aria-describedby="tip">Save</button>`},
		{name: "a double quote inside a single-quoted value", html: `<button aria-describedby='a"b'>Save</button>`, token: "a",
			want: `<button aria-describedby='a"b a'>Save</button>`},
		{name: "a non-breaking space does not split an id", html: `<button aria-describedby="help&#160;details">Save</button>`, token: "help",
			want: `<button aria-describedby="help&#160;details help">Save</button>`},
		{name: "a token that needs escaping, unquoted", html: `<button aria-describedby=help>Save</button>`, token: `a&b`,
			want: `<button aria-describedby="help a&amp;b">Save</button>`},
		{name: "a tag name holding a quote", html: `<button">">Save</button>`, token: "tip",
			want: `<button" aria-describedby="tip">">Save</button>`},
		{name: "a token with a form feed", html: "<button>x</button>", token: "a\fb", wantErr: "single token"},
		{name: "a token with a NUL, which a browser reads as U+FFFD", html: "<button>x</button>", token: "a\x00b", wantErr: "single token"},
		{name: "a long s does not fold to s", html: `<button aria-deſcribedby="help">Save</button>`, token: "tip",
			want: `<button aria-deſcribedby="help" aria-describedby="tip">Save</button>`},
		{name: "an upper-case name", html: `<button ARIA-DESCRIBEDBY="help">Save</button>`, token: "tip",
			want: `<button ARIA-DESCRIBEDBY="help tip">Save</button>`},
		{name: "a mixed-case name", html: `<button Aria-DescribedBy='help'>Save</button>`, token: "tip",
			want: `<button Aria-DescribedBy='help tip'>Save</button>`},
		{name: "a bare attribute before another", html: `<button aria-describedby class="x">Save</button>`, token: "tip",
			want: `<button aria-describedby="tip" class="x">Save</button>`},
		{name: "unquoted value", html: `<input aria-describedby=help class="x">`, token: "tip",
			want: `<input aria-describedby="help tip" class="x">`},
		{name: "already listed", html: `<button aria-describedby="tip help">Save</button>`, token: "tip",
			want: `<button aria-describedby="tip help">Save</button>`},
		{name: "empty value", html: `<button aria-describedby="">Save</button>`, token: "tip",
			want: `<button aria-describedby="tip">Save</button>`},
		{name: "no value", html: `<button aria-describedby>Save</button>`, token: "tip",
			want: `<button aria-describedby="tip">Save</button>`},
		{name: "a lookalike inside a quoted value is not the attribute", html: `<button title='aria-describedby="x"'>Save</button>`, token: "tip",
			want: `<button title='aria-describedby="x"' aria-describedby="tip">Save</button>`},
		{name: "only the root tag", html: `<span><b aria-describedby="x">a</b></span>`, token: "tip",
			want: `<span aria-describedby="tip"><b aria-describedby="x">a</b></span>`},
		{name: "escapes what it writes", html: `<button aria-describedby="a&amp;b">x</button>`, token: "tip",
			want: `<button aria-describedby="a&amp;b tip">x</button>`},
		{name: "a token with a space", html: `<button>x</button>`, token: "a b", wantErr: "single token"},
		{name: "text only", html: `Save`, token: "tip", wantErr: "element open tag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := AddAttributeToken(render.HTML(c.html), "aria-describedby", c.token)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
				}
				if string(got) != c.html {
					t.Fatalf("on error got %q, want the input back", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestAddAttributeTokenRefusesABadName(t *testing.T) {
	src := render.HTML(`<button>x</button>`)
	got, err := AddAttributeToken(src, `a"b`, "tip")
	if err == nil || !strings.Contains(err.Error(), "not a valid attribute name") || got != src {
		t.Fatalf("got (%s, %v), want the input back and a refusal of the name", got, err)
	}
}

// browserAttrs reads the first start tag of s, past leading whitespace
// and comments, as golang.org/x/net/html's tokenizer (a browser's rules)
// does: its attributes in order, the first of a repeated name winning.
func browserAttrs(s string) (map[string]string, []string, bool) {
	z := xhtml.NewTokenizer(strings.NewReader(s))
	for {
		switch z.Next() {
		case xhtml.CommentToken:
			continue
		case xhtml.TextToken:
			if strings.TrimLeft(string(z.Raw()), " \t\n\r\f") == "" {
				continue
			}
			return nil, nil, false
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			tok := z.Token()
			m := map[string]string{}
			var order []string
			for _, a := range tok.Attr {
				if _, seen := m[a.Key]; !seen {
					m[a.Key] = a.Val
					order = append(order, a.Key)
				}
			}
			return m, order, true
		default:
			return nil, nil, false
		}
	}
}

func htmlFields(v string) []string { return strings.FieldsFunc(v, isHTMLSpaceRune) }

// spliceSeeds are root tags whose attributes the splice once misread.
var spliceSeeds = []string{
	`<button>Save</button>`,
	`<button aria-describedby="help">Save</button>`,
	`<button aria-describedby=a"onfocus=alert(1)//"x>Save</button>`,
	`<button aria-describedby=a"b">Save</button>`,
	`<button title= aria-describedby>Save</button>`,
	`<button title= aria-describedby=x>Save</button>`,
	`<button/aria-describedby="x">Save</button>`,
	`<button class="y"aria-describedby="x">Save</button>`,
	`<input class=x/>`,
	`<button title=>Save</button>`,
	`<button title= >Save</button>`,
	`<br />`,
	`<br/>`,
	`<!--><div title="a>b" ARIA-DESCRIBEDBY='t&#105;p'>x</div>`,
	"<button aria-describedby\f=\f\"help\">Save</button>",
	`<button aria-describedby="help&copy=x">Save</button>`,
	`<button aria-describedby='a"b'>Save</button>`,
	`<button">">Save</button>`,
	`<a =x b>y</a>`,
	`<button aria-deſcribedby="help">Save</button>`,
	`<button ARIA-DESCRIBEDBY="help" aria-describedby="x">Save</button>`,
}

// checkSpliceMatchesABrowser: whatever the root tag, Attribute reads what
// a browser reads, and AddAttributeToken leaves every other attribute as
// a browser reads it and adds the token to the list once.
func checkSpliceMatchesABrowser(t *testing.T, src, token string) {
	t.Helper()
	const name = "aria-describedby"
	if !utf8.ValidString(src) || !utf8.ValidString(token) {
		return // a browser decodes bytes first; ids and markup here are UTF-8
	}
	before, order, ok := browserAttrs(src)
	if !ok {
		return
	}
	if v, has, err := Attribute(render.HTML(src), name); err == nil {
		want, wantHas := before[name]
		if has != wantHas || (has && attrDecode(v) != want) {
			t.Fatalf("Attribute(%q) = (%q, %v), a browser reads (%q, %v)", src, v, has, want, wantHas)
		}
	}
	out, err := AddAttributeToken(render.HTML(src), name, token)
	if err != nil {
		return
	}
	after, _, ok := browserAttrs(string(out))
	if !ok {
		t.Fatalf("AddAttributeToken(%q) = %q, which a browser does not read as a start tag", src, out)
	}
	for _, k := range order {
		if k != name && after[k] != before[k] {
			t.Fatalf("AddAttributeToken(%q) = %q changed %s from %q to %q", src, out, k, before[k], after[k])
		}
	}
	for k := range after {
		if _, had := before[k]; !had && k != name {
			t.Fatalf("AddAttributeToken(%q) = %q added attribute %q", src, out, k)
		}
	}
	want := htmlFields(before[name])
	if !slices.Contains(want, token) {
		want = append(want, token)
	}
	if got := htmlFields(after[name]); !slices.Equal(got, want) {
		t.Fatalf("AddAttributeToken(%q, %q) = %q: a browser reads %s as %q, want %q", src, token, out, name, got, want)
	}
}

func TestAttributeSpliceMatchesABrowser(t *testing.T) {
	for _, src := range spliceSeeds {
		for _, token := range []string{"tip", `a&b"<`} {
			checkSpliceMatchesABrowser(t, src, token)
		}
	}
}

func FuzzAttributeSpliceMatchesABrowser(f *testing.F) {
	for _, seed := range spliceSeeds {
		f.Add(seed, "tip")
		f.Add(seed, `a&b"<`)
	}
	f.Add("<A>", "\x00")
	f.Fuzz(checkSpliceMatchesABrowser)
}
