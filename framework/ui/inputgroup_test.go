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
