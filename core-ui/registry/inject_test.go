package registry

import (
	"strings"
	"testing"

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

func TestInjectRespectsAttrQuotes(t *testing.T) {
	// '>' inside an attribute must not be treated as tag end.
	got, err := injectMarker(`<div title="a > b">hi</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-cui-comp="modal"`) {
		t.Errorf("got %s", got)
	}
	if !strings.Contains(string(got), `title="a > b"`) {
		t.Errorf("attribute corrupted: %s", got)
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
		{"valueless", `<div data-cui-scope></div>`, "data-cui-scope", "", true, ""},
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
