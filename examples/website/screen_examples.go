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
		Name: "website",
		Path: "examples/website",
		Body: "This site. The canonical SSR + islands + 10 UI primitives showcase. Every framework/ui component and every widget preset is dogfooded under /components/<slug>. Ships statically via --build-static.",
	},
	{
		Name: "api-tour",
		Path: "examples/api-tour",
		Body: "Tour of the CRUD API surface — include= (eager loading), cursor= (keyset pagination), POST /_batch (atomic batches), GET /_events (SSE), multipart uploads, and BelongsTo FK constraints.",
	},
	{
		Name: "blog",
		Path: "examples/blog",
		Body: "Auto-CRUD with JSON-declared entities. Three JSON declarations under entities/ produce SQL tables, REST endpoints, OpenAPI/Swagger UI, and an MCP tool surface. Backed by SQLite by default.",
	},
	{
		Name: "embed-demo",
		Path: "examples/embed-demo",
		Body: "Runnable example of battery/embed — in-process semantic index with the dependency-free stub embedder. Demonstrates registering Plugin on a framework.App to auto-mount the /embed/* routes.",
	},
	{
		Name: "spa",
		Path: "examples/spa",
		Body: "Single-page application skeleton. Vue frontend hitting the Go entity API; demonstrates the 'use GoFastr just for the API' pattern with static SPA fallback.",
	},
	{
		Name: "static-site",
		Path: "examples/static-site",
		Body: "Static HTML-only pages served via core/static.Mount. Demonstrates the simplest possible deployment: no DB, no UI runtime, just a file server.",
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
