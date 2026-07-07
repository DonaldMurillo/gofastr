package tree

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

type stubTheme = style.Theme

func TestRenderFlatTree(t *testing.T) {
	out := string(Render(Config{
		ID:    "files",
		Label: "File system",
		Nodes: []Node{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C", Selected: true},
		},
	}))
	wants := []string{
		`role="tree"`,
		`aria-label="File system"`,
		`id="files"`,
		`id="a"`,
		`id="b"`,
		`id="c"`,
		`role="treeitem"`,
		`aria-level="1"`,
		`aria-posinset="1"`,
		`aria-setsize="3"`,
		`aria-selected="true"`,
		`tabindex="0"`,
		`tabindex="-1"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("Tree missing %q\nout: %s", w, out)
		}
	}
}

func TestRenderNestedTree(t *testing.T) {
	out := string(Render(Config{
		ID:    "fs",
		Label: "FS",
		Nodes: []Node{
			{
				ID: "src", Label: "src", Expanded: true,
				Children: []Node{
					{ID: "src-main", Label: "main.go"},
					{ID: "src-util", Label: "util.go"},
				},
			},
		},
	}))
	for _, w := range []string{
		`aria-expanded="true"`,
		`role="group"`,
		`aria-level="2"`,
		`>main.go<`,
		`>util.go<`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("nested tree missing %q\nout: %s", w, out)
		}
	}
}

func TestRenderLazy(t *testing.T) {
	out := string(Render(Config{
		ID:           "fs",
		Label:        "FS",
		SignalPrefix: "tree-fs",
		Nodes: []Node{
			{ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
		},
	}))
	for _, w := range []string{
		`aria-expanded="false"`,
		`data-fui-rpc="/tree/vendor"`,
		`data-fui-rpc-method="POST"`,
		`data-fui-rpc-signal="tree-fs-vendor"`,
		`data-fui-tree-toggle=""`,
		`data-fui-signal="tree-fs-vendor"`,
		`data-fui-signal-mode="html"`,
		`hidden`, // group starts hidden
	} {
		if !strings.Contains(out, w) {
			t.Errorf("lazy tree missing %q\nout: %s", w, out)
		}
	}
}

func TestRenderLeafLink(t *testing.T) {
	out := string(Render(Config{
		ID:    "nav",
		Label: "Nav",
		Nodes: []Node{
			{ID: "home", Label: "Home", Href: "/"},
		},
	}))
	if !strings.Contains(out, `href="/"`) || !strings.Contains(out, ">Home</a>") {
		t.Errorf("expected anchored leaf, got: %s", out)
	}
	if !strings.Contains(out, `class="tree__label"`) {
		t.Errorf("expected tree__label class on anchor, got: %s", out)
	}
}

func TestRenderPanics(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no ID", Config{Label: "x", Nodes: []Node{{ID: "x", Label: "X"}}}},
		{"no Label", Config{ID: "x", Nodes: []Node{{ID: "x", Label: "X"}}}},
		{"no Nodes", Config{ID: "x", Label: "x"}},
		{"lazy needs SignalPrefix", Config{
			ID: "x", Label: "x",
			Nodes: []Node{{ID: "a", Label: "A", LazyPath: "/x"}},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			Render(c.cfg)
		})
	}
}

func TestStyleScopedToComponent(t *testing.T) {
	css := styleFn(stubTheme{})
	for _, w := range []string{`[data-fui-comp="tree"]`, `tree__row`, `tree__toggle`, `aria-expanded="true"`, `aria-selected="true"`} {
		if !strings.Contains(css, w) {
			t.Errorf("styleFn missing %q", w)
		}
	}
}

func TestRenderEmitsDataFuiComp(t *testing.T) {
	out := string(Render(Config{
		ID: "x", Label: "x",
		Nodes: []Node{{ID: "a", Label: "A"}},
	}))
	if !strings.Contains(out, `data-fui-comp="tree"`) {
		t.Errorf("Render must emit data-fui-comp for auto-load, got: %s", out)
	}
}

func TestFocusOutlineRequiresFocus(t *testing.T) {
	css := styleFn(style.Theme{})
	// The roving-tabindex item always carries tabindex="0"; an outline
	// keyed on the bare attribute renders a permanent focus ring on the
	// first item. The outline must require a focus pseudo-class.
	if strings.Contains(css, `[tabindex="0"] > .tree__row`) {
		t.Errorf("outline selector keys on bare [tabindex=\"0\"] — permanent focus ring:\n%s", css)
	}
	if !strings.Contains(css, ":focus-visible") {
		t.Errorf("tree CSS should scope the outline to :focus-visible:\n%s", css)
	}
}
