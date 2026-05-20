package scrollspy

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestScrollspyRequiresObserve(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic without ObserveSelector")
		}
	}()
	Wrap(Config{}, render.HTML("x"))
}

func TestScrollspyObserveAttr(t *testing.T) {
	got := string(Wrap(Config{ObserveSelector: "main"}, render.HTML("x")))
	if !strings.Contains(got, `data-fui-scrollspy="main"`) {
		t.Errorf("expected data-fui-scrollspy attr, got: %s", got)
	}
}

func TestScrollspyTargetAttr(t *testing.T) {
	got := string(Wrap(Config{
		ObserveSelector: "main",
		TargetSelector:  "section[id]",
	}, render.HTML("x")))
	if !strings.Contains(got, `data-fui-scrollspy-target="section[id]"`) {
		t.Errorf("expected target attr, got: %s", got)
	}
}

func TestScrollspyMarker(t *testing.T) {
	got := string(Wrap(Config{ObserveSelector: "main"}, render.HTML("x")))
	if !strings.Contains(got, `data-fui-comp="scrollspy"`) {
		t.Errorf("expected data-fui-comp=\"scrollspy\" marker, got: %s", got)
	}
}

func TestScrollspyKeepsChild(t *testing.T) {
	child := render.HTML(`<nav><a href="#one">One</a></nav>`)
	got := string(Wrap(Config{ObserveSelector: "main"}, child))
	if !strings.Contains(got, `<a href="#one">One</a>`) {
		t.Errorf("expected child preserved, got: %s", got)
	}
}

func TestScrollspyActiveCSS(t *testing.T) {
	css := Style.Entry().CSSFor(style.Theme{})
	for _, want := range []string{".scrollspy", "is-active", "aria-current"} {
		if !strings.Contains(css, want) {
			t.Errorf("registered Style missing %q", want)
		}
	}
}
