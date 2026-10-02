package headless

import (
	"strings"
	"testing"
)

func renderTreeView(p TreeProps) string { return string(Tree(p, nil)) }

func TestTreeRendersTheRovingTabindexAndSetMath(t *testing.T) {
	h := renderTreeView(TreeProps{ID: "files", Label: "File system", Nodes: []TreeNode{
		{ID: "a", Label: "A"},
		{ID: "b", Label: "B"},
		{ID: "c", Label: "C", Selected: true},
	}})
	for _, want := range []string{
		`data-hui-tree=""`,
		`aria-label="File system"`,
		`role="treeitem"`,
		`aria-level="1"`,
		`aria-posinset="2"`,
		`aria-setsize="3"`,
		`aria-selected="true"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("tree missing %q:\n%s", want, h)
		}
	}
	// Exactly one row carries tabindex=0 — the roving tabindex.
	if n := strings.Count(h, `tabindex="0"`); n != 1 {
		t.Errorf("tree rendered %d rows with tabindex=0, want exactly 1:\n%s", n, h)
	}
	if n := strings.Count(h, `tabindex="-1"`); n != 2 {
		t.Errorf("tree rendered %d rows with tabindex=-1, want 2:\n%s", n, h)
	}
}

func TestTreeRendersNestedBranches(t *testing.T) {
	h := renderTreeView(TreeProps{ID: "fs", Label: "FS", Nodes: []TreeNode{
		{ID: "src", Label: "src", Expanded: true, Children: []TreeNode{
			{ID: "src-main", Label: "main.go"},
			{ID: "src-util", Label: "util.go"},
		}},
		{ID: "docs", Label: "docs", Children: []TreeNode{
			{ID: "docs-readme", Label: "README.md"},
		}},
	}})
	for _, want := range []string{
		`aria-expanded="true"`,
		`aria-expanded="false"`,
		`role="group"`,
		`aria-level="2"`,
		`>main.go<`,
		`>util.go<`,
		`data-hui-tree-toggle=""`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("nested tree missing %q:\n%s", want, h)
		}
	}
	// A collapsed branch's group is hidden; an expanded one's is not.
	// (Count the boolean attribute exactly: aria-hidden on the
	// toggles is a different attribute.)
	if n := strings.Count(h, ` hidden=""`); n != 1 {
		t.Errorf("exactly the collapsed branch's group should be hidden, found %d hidden attributes:\n%s", n, h)
	}
}

func TestTreeLazyBranchKeepsThePatternContract(t *testing.T) {
	h := renderTreeView(TreeProps{ID: "lz", Label: "Lazy", LazySignalPrefix: "tree-lz", Nodes: []TreeNode{
		{ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
	}})
	for _, want := range []string{
		`aria-expanded="false"`,
		`data-fui-rpc="/tree/vendor"`,
		`data-fui-rpc-method="POST"`,
		`data-fui-rpc-signal="tree-lz-vendor"`,
		`data-hui-tree-toggle=""`,
		`data-fui-signal="tree-lz-vendor"`,
		`data-fui-signal-mode="html"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("lazy tree missing %q:\n%s", want, h)
		}
	}
	// The group starts hidden; children win when both are set.
	h = renderTreeView(TreeProps{ID: "lz2", Label: "Lazy", LazySignalPrefix: "x", Nodes: []TreeNode{
		{ID: "n", Label: "n", LazyPath: "/x", Children: []TreeNode{{ID: "c", Label: "c"}}},
	}})
	if strings.Contains(h, "data-fui-rpc") || strings.Contains(h, "data-fui-signal=") {
		t.Errorf("a node with Children should ignore LazyPath:\n%s", h)
	}
	// The children are real markup (collapsed by default, like any
	// static branch) rather than a lazy swap target.
	if !strings.Contains(h, `>c<`) {
		t.Errorf("a node with Children should render them as markup:\n%s", h)
	}
}

func TestTreeDropsDangerousHrefs(t *testing.T) {
	for _, href := range []string{
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.example.com/x",
		"java\tscript:alert(1)",
	} {
		h := renderTreeView(TreeProps{ID: "t", Label: "T", Nodes: []TreeNode{
			{ID: "n", Label: "Leaf", Href: href},
		}})
		if strings.Contains(h, "<a ") {
			t.Errorf("dangerous href %q rendered an anchor:\n%s", href, h)
		}
		if strings.Contains(h, "javascript:") || strings.Contains(h, "vbscript:") ||
			strings.Contains(h, "data:text/html") {
			t.Errorf("dangerous scheme survived for %q:\n%s", href, h)
		}
	}
}

// TestTreeRendersSafeHrefAnchors is the other half of the href
// policy: the safe list must keep rendering real anchors, so a
// future tightening of CleanAnchor cannot silently degrade every
// leaf to a plain label while the dangerous test stays green.
func TestTreeRendersSafeHrefAnchors(t *testing.T) {
	for _, tc := range []struct{ href, want string }{
		{"/docs/page", `<a href="/docs/page"`},
		{"https://example.com/docs", `<a href="https://example.com/docs"`},
		{"#section", `<a href="#section"`},
		{"mailto:support@example.com", `<a href="mailto:support@example.com"`},
	} {
		h := renderTreeView(TreeProps{ID: "t", Label: "T", Nodes: []TreeNode{
			{ID: "n", Label: "Leaf", Href: tc.href},
		}})
		if !strings.Contains(h, tc.want) {
			t.Errorf("safe href %q should render an anchor:\n%s", tc.href, h)
		}
	}
}

func TestTreeScrubsCarriedLabels(t *testing.T) {
	h := renderTreeView(TreeProps{ID: "t", Label: "T", Nodes: []TreeNode{
		{ID: "n", Label: "Ev\r\nil<script>"},
	}})
	if strings.ContainsAny(h, "\r\n") {
		t.Errorf("control bytes reached the DOM:\n%q", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("markup reached the DOM raw:\n%s", h)
	}
}

func TestTreeRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"no ID", func() { Tree(TreeProps{Label: "L", Nodes: []TreeNode{{ID: "a", Label: "A"}}}, nil) }},
		{"blank label", func() { Tree(TreeProps{ID: "t", Label: " ", Nodes: []TreeNode{{ID: "a", Label: "A"}}}, nil) }},
		{"no nodes", func() { Tree(TreeProps{ID: "t", Label: "L"}, nil) }},
		{"lazy without prefix", func() {
			Tree(TreeProps{ID: "t", Label: "L", Nodes: []TreeNode{{ID: "v", Label: "v", LazyPath: "/x"}}}, nil)
		}},
		{"blank node id", func() {
			Tree(TreeProps{ID: "t", Label: "L", Nodes: []TreeNode{{ID: " ", Label: "A"}}}, nil)
		}},
		{"blank node label", func() {
			Tree(TreeProps{ID: "t", Label: "L", Nodes: []TreeNode{{ID: "a", Label: " "}}}, nil)
		}},
		{"duplicate node ids", func() {
			Tree(TreeProps{ID: "t", Label: "L", Nodes: []TreeNode{
				{ID: "a", Label: "A"},
				{ID: "a", Label: "B"},
			}}, nil)
		}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s should refuse at render", c.name)
				}
			}()
			c.call()
		}()
	}
}

func TestTreePreloadsItsModule(t *testing.T) {
	// The marker the preload scan sees is the tree root's hook; a
	// render without one must not fetch the module.
	if !treeRendersMarker(renderTreeView(TreeProps{ID: "t", Label: "L", Nodes: []TreeNode{{ID: "a", Label: "A"}}})) {
		t.Fatal("a rendered Tree carries no data-hui-tree marker — the module never loads")
	}
}

func treeRendersMarker(html string) bool {
	return strings.Contains(html, "data-hui-tree")
}
