package registry

import (
	"strings"
	"testing"
)

func TestInjectIntoSimpleDiv(t *testing.T) {
	got, err := injectMarker(`<div>hi</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `<div data-fui-comp="modal">`) {
		t.Errorf("got %s", got)
	}
}

func TestInjectIntoDivWithAttrs(t *testing.T) {
	got, err := injectMarker(`<div class="x" id="y">hi</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-fui-comp="modal"`) {
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
	if !strings.Contains(string(got), `data-fui-comp="logo"`) {
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
	want := `<section role="banner" data-fui-comp="page-header">`
	if !strings.Contains(string(got), want) {
		t.Errorf("got %s want substring %q", got, want)
	}
}

func TestInjectIntoFragmentLeadingWhitespace(t *testing.T) {
	got, err := injectMarker("\n\t<div>x</div>", "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-fui-comp="modal"`) {
		t.Errorf("got %s", got)
	}
}

func TestInjectSkipsLeadingComment(t *testing.T) {
	got, err := injectMarker(`<!-- intro --><div>x</div>`, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `data-fui-comp="modal"`) {
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
	if !strings.Contains(string(got), `data-fui-comp="modal"`) {
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
	in := `<div data-fui-comp="modal" class="x">hi</div>`
	out, err := injectMarker(in, "modal")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("idempotent re-injection altered html:\nin:  %s\nout: %s", in, out)
	}
	count := strings.Count(string(out), `data-fui-comp=`)
	if count != 1 {
		t.Errorf("got %d data-fui-comp attrs, want 1", count)
	}
}

// TestInjectSelfClosingPreservesSpace asserts that a self-closing
// tag with a space before /> retains that space after marker
// injection — otherwise `<br />` becomes `<br data-fui-comp="…"/>`
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
	if !strings.Contains(s, `data-fui-comp="spacer"`) {
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
	if !strings.Contains(s, `data-fui-comp="tip"`) {
		t.Errorf("marker not injected: %q", s)
	}
	// Marker must be inside the opening tag, BEFORE the > that closes
	// the element's open tag (not after the > inside the title attr).
	endOfOpen := strings.Index(s, `">hi</a>`)
	if endOfOpen < 0 {
		t.Fatalf("element body misaligned: %q", s)
	}
	markerIdx := strings.Index(s, `data-fui-comp`)
	if markerIdx >= endOfOpen {
		t.Errorf("marker spliced AFTER the real `>` — embedded `>` confused tag-end detector: %q", s)
	}
}

// TestInjectIgnoresAttrNameInsideQuotedValue guards against a false-positive
// in the idempotence check: if "data-fui-comp" appears inside a quoted
// attribute value, hasAttribute() must NOT treat it as already-present,
// otherwise injectMarker silently skips marker injection.
func TestInjectIgnoresAttrNameInsideQuotedValue(t *testing.T) {
	cases := []string{
		// Substring in class="..." value
		`<div class="x data-fui-comp x">hi</div>`,
		// Substring in title="..." value
		`<div title="data-fui-comp inside">hi</div>`,
		// Single-quoted attr value
		`<div data-foo='data-fui-comp'>hi</div>`,
	}
	for _, in := range cases {
		out, err := injectMarker(in, "modal")
		if err != nil {
			t.Errorf("input %q: unexpected error %v", in, err)
			continue
		}
		count := strings.Count(string(out), `data-fui-comp="modal"`)
		if count != 1 {
			t.Errorf("input %q: expected exactly 1 data-fui-comp=\"modal\" attr, got %d in output:\n%s", in, count, out)
		}
	}
}

// TestInjectIdempotentAcrossLineBreaks guards against the bug where
// the idempotence check only matched ` data-fui-comp` or `\tdata-fui-comp`,
// missing `\ndata-fui-comp` / `\rdata-fui-comp`. Multi-line opening
// tags (common in handwritten templates) would get a duplicate marker.
func TestInjectIdempotentAcrossLineBreaks(t *testing.T) {
	cases := []string{
		// Bare newline / CR directly before the attribute — no space
		// indent — so only an \n / \r boundary distinguishes the attr.
		"<div\ndata-fui-comp=\"modal\"\nclass=\"x\">hi</div>",
		"<div\rdata-fui-comp=\"modal\"\rclass=\"x\">hi</div>",
		// And the indented cases that already work — keep them as a
		// regression net.
		"<div\n  data-fui-comp=\"modal\"\n  class=\"x\">hi</div>",
		"<div\r\n  data-fui-comp=\"modal\"\r\n  class=\"x\">hi</div>",
	}
	for i, in := range cases {
		out, err := injectMarker(in, "modal")
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		count := strings.Count(string(out), `data-fui-comp=`)
		if count != 1 {
			t.Errorf("case %d: got %d data-fui-comp attrs, want 1 (multi-line opening tag should be idempotent)", i, count)
		}
	}
}

func TestInjectSkipsWhenWrappedByDifferentName(t *testing.T) {
	// Composition: outer Style wraps inner Style's already-wrapped
	// output. The outer marker would normally win, but with the
	// existing-marker guard we conservatively don't inject again.
	// (Authors should compose at the Style.Render level, not double-
	// wrap pre-rendered HTML.)
	in := `<div data-fui-comp="inner">hi</div>`
	out, _ := injectMarker(in, "outer")
	if strings.Count(string(out), `data-fui-comp=`) != 1 {
		t.Errorf("double-wrap should leave 1 marker; got %s", out)
	}
}
