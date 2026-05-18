package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/tree"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type TreeScreen struct{}

func (s *TreeScreen) ScreenTitle() string {
	return "Tree View"
}
func (s *TreeScreen) ScreenDescription() string {
	return "Recursive treeitems with optional RPC lazy-load on expand."
}
func (s *TreeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TreeScreen) Render() render.HTML {
	demo := tree.Render(tree.Config{
		ID:           "files-tree",
		Label:        "Project files",
		SignalPrefix: "files-tree",
		Nodes: []tree.Node{
			{
				ID: "src", Label: "src", Expanded: true,
				Children: []tree.Node{
					{ID: "src-main", Label: "main.go", Href: "#main"},
					{ID: "src-util", Label: "util.go", Href: "#util"},
				},
			},
			{
				ID: "vendor", Label: "vendor",
				LazyPath: "/islands/new-components/tree-load",
			},
			{
				ID: "docs", Label: "docs",
				Children: []tree.Node{
					{ID: "docs-readme", Label: "README.md", Href: "#readme"},
				},
			},
		},
	})
	src := `tree.Render(tree.Config{
    ID:           "files",
    Label:        "Project files",
    SignalPrefix: "files-tree",
    Nodes: []tree.Node{
        {ID: "src", Label: "src", Expanded: true, Children: []tree.Node{
            {ID: "src-main", Label: "main.go", Href: "/files/src/main.go"},
        }},
        {ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
    },
})`
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Tree View")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"WAI-ARIA tree with roving tabindex. The \"vendor\" node lazy-loads its children via RPC the first time you expand it. Arrow keys + Home/End + type-ahead navigate; Enter / Space toggle.")),
		demoFrame(demo, src),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Keyboard")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("↓ / ↑ — next / previous visible treeitem")),
			render.Tag("li", nil, render.Text("→ — expand (or focus first child if already expanded)")),
			render.Tag("li", nil, render.Text("← — collapse (or focus parent if already collapsed)")),
			render.Tag("li", nil, render.Text("Home / End — first / last visible treeitem")),
			render.Tag("li", nil, render.Text("Enter / Space — toggle expand")),
			render.Tag("li", nil, render.Text("a–z — type-ahead jump to next label starting with the typed prefix")),
		),
	)
}
