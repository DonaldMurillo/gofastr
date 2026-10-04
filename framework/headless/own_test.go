package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestOwnMarksEveryTopLevelElement(t *testing.T) {
	cases := []struct{ in, want string }{
		{`<b>x</b>`, `<b data-cui-internal="">x</b>`},
		{`<a href="/x">1</a><a>2</a>`, `<a href="/x" data-cui-internal="">1</a><a data-cui-internal="">2</a>`},
		{`<span><i>inner</i></span>`, `<span data-cui-internal=""><i>inner</i></span>`},
		{`<svg viewBox="0 0 1 1"/>`, `<svg viewBox="0 0 1 1" data-cui-internal=""/>`},
		{`<input type="text"><b>x</b>`, `<input type="text" data-cui-internal=""><b data-cui-internal="">x</b>`},
		{`<b title="a > b">x</b>`, `<b title="a > b" data-cui-internal="">x</b>`},
		{`<b data-cui-internal="">x</b>`, `<b data-cui-internal="">x</b>`},
		{`<!-- <b> --><i>x</i>`, `<!-- <b> --><i data-cui-internal="">x</i>`},
		{`<style>.a>b{}</style><i>x</i>`, `<style data-cui-internal="">.a>b{}</style><i data-cui-internal="">x</i>`},
		{`text only`, `text only`},
		{``, ``},
	}
	for _, c := range cases {
		if got := string(Own(render.HTML(c.in))); got != c.want {
			t.Errorf("Own(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOwnedSlotNeedsEveryRootMarked(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{``, false},
		{`caller text`, false},
		{`<b>x</b>`, false},
		{`<b data-cui-internal="">x</b>`, true},
		{`<b data-cui-internal="">x</b> <i data-cui-internal="">y</i>`, true},
		{`<b data-cui-internal="">x</b><i>y</i>`, false},
		{`<b data-cui-internal="">x</b>tail`, false},
		// A mark on a nested element is not a mark on the root.
		{`<b><i data-cui-internal="">x</i></b>`, false},
		// The attribute's name, not text inside another attribute.
		{`<b title=" data-cui-internal">x</b>`, false},
	}
	for _, c := range cases {
		if got := ownedSlot(render.HTML(c.in)); got != c.want {
			t.Errorf("ownedSlot(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	for _, in := range []string{`<b>x</b>`, `<b>1</b><i>2</i>`, `<svg/>`} {
		if !ownedSlot(Own(render.HTML(in))) {
			t.Errorf("ownedSlot(Own(%q)) = false", in)
		}
	}
}
