package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ExamplesScreen lists the example apps shipped in the repo.
type ExamplesScreen struct{}

func (s *ExamplesScreen) ScreenTitle() string        { return "Examples" }
func (s *ExamplesScreen) ScreenDescription() string  { return "Reference applications shipped with GoFastr." }
func (s *ExamplesScreen) ScreenType() app.ScreenType { return app.ScreenPage }

type exampleEntry struct {
	Name string
	Path string
	Body string
}

var exampleEntries = []exampleEntry{
	{
		Name: "blog",
		Path: "examples/blog",
		Body: "Auto-CRUD with the entity framework. Three JSON declarations under entities/ produce SQL tables, REST endpoints, OpenAPI/Swagger UI, and an MCP tool surface. Backed by SQLite by default.",
	},
	{
		Name: "core-ui-demo",
		Path: "examples/core-ui-demo",
		Body: "Server-driven UI playground. Demonstrates signals, components, islands, SSE streaming, the Go→JS action compiler, and the new SSR Loader hook + SSG build pipeline.",
	},
	{
		Name: "spa",
		Path: "examples/spa",
		Body: "Single-page application skeleton.",
	},
	{
		Name: "static-site",
		Path: "examples/static-site",
		Body: "Static-site generation example.",
	},
	{
		Name: "website",
		Path: "examples/website",
		Body: "This site. Renders the project's Markdown docs through the framework, ships statically via gofastr's --build-static.",
	},
}

func (s *ExamplesScreen) Render() render.HTML {
	cards := make([]render.HTML, 0, len(exampleEntries))
	for _, ex := range exampleEntries {
		cards = append(cards, render.Tag("a",
			map[string]string{"href": "https://github.com/DonaldMurillo/gofastr/tree/main/" + ex.Path, "rel": "external"},
			render.Tag("strong", nil, render.Text(ex.Name)),
			render.Tag("span", nil, render.Text(ex.Body)),
		))
	}
	return render.Tag("div", nil,
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Examples")),
		render.Tag("p", nil, render.Text(
			"Self-contained reference apps under examples/ in the repo. Each is a normal "+
				"Go module that imports the framework — clone the repo and run them locally.")),
		render.Tag("div", map[string]string{"class": "doc-list"}, cards...),
	)
}
