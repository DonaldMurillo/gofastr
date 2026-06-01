package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Collapsible ───

func TestCollapsibleBasic(t *testing.T) {
	h := Collapsible(CollapsibleConfig{Summary: "Details"}, render.Text("body content"))
	s := string(h)

	mustContain(t, h, "<details")
	mustContain(t, h, "<summary")
	mustContain(t, h, "Details")
	mustContain(t, h, "body content")
	mustContain(t, h, `class="fui-collapsible"`)
	mustContain(t, h, `data-fui-comp="fui-collapsible"`)
	mustContain(t, h, `fui-collapsible__summary`)
	mustContain(t, h, `fui-collapsible__content`)
	mustContain(t, h, "</details>")

	// Verify the <details> tag has no "open" attribute when Open is false
	if strings.Contains(s, " open") {
		t.Fatalf("did not expect 'open' attribute when cfg.Open is false\ngot: %s", s)
	}
}

func TestCollapsibleOpen(t *testing.T) {
	h := Collapsible(CollapsibleConfig{Summary: "Expanded", Open: true}, render.Text("visible"))
	s := string(h)

	// Find the <details> open tag and verify it contains the "open" attribute
	idx := strings.Index(s, "<details ")
	if idx < 0 {
		t.Fatalf("expected <details element\ngot: %s", s)
	}
	end := strings.Index(s[idx:], ">")
	fragment := s[idx : idx+end]
	if !strings.Contains(fragment, " open") {
		t.Fatalf("expected 'open' attribute on <details> tag\ngot fragment: %s", fragment)
	}
}

func TestCollapsibleDisclosure(t *testing.T) {
	h := Collapsible(CollapsibleConfig{Summary: "Section"}, render.Text("content"))

	mustContain(t, h, "data-fui-disclosure")
}

func TestCollapsibleMissingSummary(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when Summary is empty")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "Summary") {
			t.Fatalf("panic should mention Summary, got: %s", msg)
		}
	}()
	Collapsible(CollapsibleConfig{Summary: ""}, render.Text("body"))
}

func TestCollapsibleCustomClassAndID(t *testing.T) {
	h := Collapsible(
		CollapsibleConfig{Summary: "Custom", Class: "extra-class", ID: "my-collapsible"},
		render.Text("content"),
	)
	s := string(h)

	if !strings.Contains(s, "extra-class") {
		t.Fatalf("expected custom class in output\ngot: %s", s)
	}
	if !strings.Contains(s, `id="my-collapsible"`) {
		t.Fatalf("expected custom id in output\ngot: %s", s)
	}
}
