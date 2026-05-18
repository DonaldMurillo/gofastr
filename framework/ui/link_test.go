package ui

import (
	"strings"
	"testing"
)

func TestLinkRequiresHref(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Link without Href should panic")
		}
	}()
	Link(LinkConfig{Text: "Go"})
}

func TestLinkRequiresText(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Link without Text should panic")
		}
	}()
	Link(LinkConfig{Href: "/"})
}

func TestLinkInlineEmitsBaseClassOnly(t *testing.T) {
	h := string(Link(LinkConfig{Href: "/x", Text: "Edit"}))
	if !strings.Contains(h, "ui-link") {
		t.Errorf("Link should emit .ui-link:\n%s", h)
	}
	if strings.Contains(h, "ui-link--action") || strings.Contains(h, "ui-link--muted") {
		t.Errorf("inline variant should not emit modifier class:\n%s", h)
	}
}

func TestLinkActionEmitsActionModifier(t *testing.T) {
	h := string(Link(LinkConfig{Href: "/x", Text: "Edit", Variant: LinkAction}))
	if !strings.Contains(h, "ui-link--action") {
		t.Errorf("Variant: LinkAction should emit .ui-link--action:\n%s", h)
	}
}

func TestLinkMutedEmitsMutedModifier(t *testing.T) {
	h := string(Link(LinkConfig{Href: "/x", Text: "see all", Variant: LinkMuted}))
	if !strings.Contains(h, "ui-link--muted") {
		t.Errorf("Variant: LinkMuted should emit .ui-link--muted:\n%s", h)
	}
}

func TestLinkRejectsUnknownVariant(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Link with unknown Variant should panic")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "bogus") {
			t.Errorf("panic should name the bogus variant: %q", msg)
		}
	}()
	Link(LinkConfig{Href: "/x", Text: "y", Variant: LinkVariant("bogus")})
}

func TestLinkEmitsAnchorAndHref(t *testing.T) {
	h := string(Link(LinkConfig{Href: "/customers/42", Text: "Edit"}))
	if !strings.Contains(h, `<a `) {
		t.Errorf("Link should render an <a> tag:\n%s", h)
	}
	if !strings.Contains(h, `href="/customers/42"`) {
		t.Errorf("Link should emit Href:\n%s", h)
	}
	if !strings.Contains(h, ">Edit<") {
		t.Errorf("Link should emit Text content:\n%s", h)
	}
}

func TestLinkActionEmitsCompMarker(t *testing.T) {
	// linkStyle.WrapHTML should attach data-fui-comp so the runtime
	// auto-loads the link CSS sheet on first appearance.
	h := string(Link(LinkConfig{Href: "/x", Text: "y", Variant: LinkAction}))
	if !strings.Contains(h, `data-fui-comp="ui-link"`) {
		t.Errorf("Link should emit data-fui-comp=ui-link:\n%s", h)
	}
}
