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
