package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Pad is the page's block rhythm under a site header. The retired
// shell padded main itself; without it a marketing page's first block
// sat on the header rule and its last block on the footer rule.
func TestContainerPadModifierClass(t *testing.T) {
	cases := map[ContainerPad]string{
		ContainerPadPage: "fui-container--pad-page",
		ContainerPadEnd:  "fui-container--pad-end",
	}
	for p, cls := range cases {
		h := string(Container(ContainerConfig{Pad: p, Class: "x"}))
		if !strings.Contains(h, `class="fui-container `+cls+` x"`) {
			t.Errorf("Pad=%q should emit .%s before the caller's class:\n%s", p, cls, h)
		}
	}
	if h := string(Container(ContainerConfig{})); strings.Contains(h, "--pad-") {
		t.Errorf("no Pad should emit no pad modifier:\n%s", h)
	}
	css := containerCSS(style.DefaultTheme())
	for _, want := range []string{
		".fui-container--pad-page { padding-block: var(--ui-container-pad-start, clamp(40px, 6vw, 64px)) var(--ui-container-pad-end, clamp(48px, 7vw, 80px)); }",
		".fui-container--pad-end { padding-block-end: var(--ui-container-pad-end, clamp(48px, 7vw, 80px)); }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("sheet should carry %q", want)
		}
	}
	defer func() { _ = recover() }()
	Container(ContainerConfig{Pad: "loose"})
	t.Error("an unknown Pad should panic")
}

func TestContainerDefaultsToDiv(t *testing.T) {
	h := string(Container(ContainerConfig{}, render.Text("body")))
	if !strings.Contains(h, "<div ") {
		t.Errorf("default As should render <div>:\n%s", h)
	}
	if !strings.Contains(h, "body") {
		t.Errorf("children should render:\n%s", h)
	}
}

func TestContainerAsTagOverride(t *testing.T) {
	h := string(Container(ContainerConfig{As: "main"}, render.Text("x")))
	if !strings.Contains(h, "<main ") {
		t.Errorf("As: main should render <main>:\n%s", h)
	}
}

func TestContainerWidthVariantClass(t *testing.T) {
	cases := map[ContainerWidth]string{
		ContainerNarrow: "fui-container--narrow",
		ContainerWide:   "fui-container--wide",
		ContainerFull:   "fui-container--full",
	}
	for w, cls := range cases {
		h := string(Container(ContainerConfig{Width: w}))
		if !strings.Contains(h, cls) {
			t.Errorf("Width=%q should emit .%s:\n%s", w, cls, h)
		}
	}
}

// A width modifier the markup carries with no rule behind it renders at
// the default cap: ContainerWide lost its rule once and every wide page
// sat at 1080px.
func TestEveryContainerWidthHasARule(t *testing.T) {
	css := containerCSS(style.DefaultTheme())
	want := map[ContainerWidth]string{
		ContainerNarrow: "max-inline-size: var(--size-narrow-width",
		ContainerWide:   "max-inline-size: var(--size-wide-width",
		ContainerPage:   "max-inline-size: calc(var(--size-page-width",
		ContainerFull:   "max-inline-size: none",
	}
	for w, decl := range want {
		cls := ".fui-container--" + string(w)
		if !strings.Contains(string(Container(ContainerConfig{Width: w})), cls[1:]) {
			t.Errorf("Width=%q should emit %s", w, cls)
		}
		i := strings.Index(css, cls+" {")
		if i < 0 {
			t.Errorf("Width=%q: no %s rule in the container CSS", w, cls)
			continue
		}
		if rule := css[i : i+strings.Index(css[i:], "}")]; !strings.Contains(rule, decl) {
			t.Errorf("Width=%q: %s rule %q should hold %q", w, cls, rule, decl)
		}
	}
}

func TestContainerDefaultWidthEmitsNoModifier(t *testing.T) {
	h := string(Container(ContainerConfig{}))
	if strings.Contains(h, "fui-container--") {
		t.Errorf("default Width should not emit a modifier class:\n%s", h)
	}
}

func TestContainerRejectsUnknownWidth(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Container with unknown Width should panic")
		}
	}()
	Container(ContainerConfig{Width: ContainerWidth("huge")})
}

func TestContainerExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := Container(ContainerConfig{ID: "real", ExtraAttrs: map[string]string{
		"data-test": "hook", "id": "evil", "Class": "evil",
	}}, render.Text("x"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `id="real"`) {
		t.Errorf("framework id lost:\n%s", root)
	}
	if !strings.Contains(root, "fui-container") {
		t.Errorf("framework class lost:\n%s", root)
	}
	if strings.Contains(root, "evil") {
		t.Errorf("owned attr overridden by ExtraAttrs:\n%s", root)
	}
}
