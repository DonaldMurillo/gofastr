package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestInputGroupRequiresInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("InputGroup without Input should panic")
		}
	}()
	InputGroup(InputGroupConfig{})
}

func TestInputGroupRendersInputOnly(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "price", ID: "price"})
	h := string(InputGroup(InputGroupConfig{Input: in}))
	if !strings.Contains(h, `name="price"`) {
		t.Errorf("expected input with name=price:\n%s", h)
	}
	if strings.Contains(h, "ui-input-group__prepend") {
		t.Errorf("no Prepend should not render prepend:\n%s", h)
	}
	if strings.Contains(h, "ui-input-group__append") {
		t.Errorf("no Append should not render append:\n%s", h)
	}
}

func TestInputGroupWithPrepend(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "price", ID: "price"})
	h := string(InputGroup(InputGroupConfig{
		Prepend: render.Text("$"),
		Input:   in,
	}))
	if !strings.Contains(h, "ui-input-group__prepend") {
		t.Errorf("expected prepend span:\n%s", h)
	}
	if !strings.Contains(h, ">$<") {
		t.Errorf("expected $ in prepend:\n%s", h)
	}
}

func TestInputGroupWithAppend(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "weight", ID: "weight"})
	h := string(InputGroup(InputGroupConfig{
		Input:  in,
		Append: render.Text("kg"),
	}))
	if !strings.Contains(h, "ui-input-group__append") {
		t.Errorf("expected append span:\n%s", h)
	}
	if !strings.Contains(h, ">kg<") {
		t.Errorf("expected kg in append:\n%s", h)
	}
}

func TestInputGroupWithPrependAndAppend(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "url", ID: "url"})
	h := string(InputGroup(InputGroupConfig{
		Prepend: render.Text("https://"),
		Input:   in,
		Append:  render.Text(".com"),
	}))
	if !strings.Contains(h, "ui-input-group__prepend") {
		t.Errorf("expected prepend:\n%s", h)
	}
	if !strings.Contains(h, "ui-input-group__append") {
		t.Errorf("expected append:\n%s", h)
	}
	if !strings.Contains(h, ">https://<") {
		t.Errorf("expected prepend content:\n%s", h)
	}
	if !strings.Contains(h, ">.com<") {
		t.Errorf("expected append content:\n%s", h)
	}
}

func TestInputGroupPrependHasAriaHidden(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "price", ID: "price"})
	h := string(InputGroup(InputGroupConfig{
		Prepend: render.Text("$"),
		Input:   in,
	}))
	if !strings.Contains(h, `aria-hidden="true"`) {
		t.Errorf("prepend span should have aria-hidden=true, got:\n%s", h)
	}
}

func TestInputGroupAppendHasAriaHidden(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "weight", ID: "weight"})
	h := string(InputGroup(InputGroupConfig{
		Input:  in,
		Append: render.Text("kg"),
	}))
	if !strings.Contains(h, `aria-hidden="true"`) {
		t.Errorf("append span should have aria-hidden=true, got:\n%s", h)
	}
}

func TestInputGroupCustomClass(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "x", ID: "x"})
	h := string(InputGroup(InputGroupConfig{
		Input: in,
		Class: "my-extra",
	}))
	if !strings.Contains(h, "my-extra") {
		t.Errorf("expected custom class:\n%s", h)
	}
}

// TestInputGroupComposesPrependInputAppend asserts the rendered DOM
// for a prepend + input + append composition: a single ui-input-group
// wrapper whose children appear in source order, each addon carries
// the right class, and the wrapper carries the data-fui-comp marker
// the runtime expects so the CSS for the visual join is applied.
func TestInputGroupComposesPrependInputAppend(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "price", ID: "price"})
	h := string(InputGroup(InputGroupConfig{
		Prepend: render.Text("$"),
		Input:   in,
		Append:  render.Text("USD"),
	}))

	// The runtime style hook + CSS selectors all key off this
	// data-fui-comp marker — its absence would silently strip the
	// visual join (regression seen on prior shipped components).
	if !strings.Contains(h, `data-fui-comp="ui-input-group"`) {
		t.Errorf("missing data-fui-comp=\"ui-input-group\" marker:\n%s", h)
	}

	prependIdx := strings.Index(h, "ui-input-group__prepend")
	inputIdx := strings.Index(h, `name="price"`)
	appendIdx := strings.Index(h, "ui-input-group__append")

	if prependIdx < 0 || inputIdx < 0 || appendIdx < 0 {
		t.Fatalf("expected prepend, input, and append in output:\n%s", h)
	}
	if !(prependIdx < inputIdx && inputIdx < appendIdx) {
		t.Errorf("expected source order prepend → input → append (got %d / %d / %d):\n%s",
			prependIdx, inputIdx, appendIdx, h)
	}
	if !strings.Contains(h, "ui-input-group") {
		t.Errorf("missing wrapper class ui-input-group:\n%s", h)
	}
}
