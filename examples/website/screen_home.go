package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HomeScreen is the landing page: hero + feature grid + quickstart.
type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "Home" }
func (s *HomeScreen) ScreenDescription() string  { return "GoFastr — a Go full-stack framework for the AI era." }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *HomeScreen) Render() render.HTML {
	hero := render.Tag("section", map[string]string{"class": "hero", "aria-label": "Hero"},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("GoFastr")),
		render.Tag("p", map[string]string{"class": "subtitle"},
			render.Text("A Go full-stack framework for the AI era. Declare your entities once, "+
				"get a working app: typed CRUD, migrations, OpenAPI, MCP tools, and a server-driven UI runtime.")),
		render.Tag("div", map[string]string{"class": "cta-row"},
			html.Link(html.LinkConfig{Href: "/docs/", Text: "Read the docs", Class: "cta-button"}),
			html.Link(html.LinkConfig{
				Href: "https://github.com/DonaldMurillo/gofastr", Text: "GitHub",
				Class: "cta-button secondary",
			}),
		),
	)

	features := render.Tag("section", map[string]string{"aria-label": "Features"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("What you get")),
		render.Tag("div", map[string]string{"class": "feature-grid"},
			featureCard("Entity declarations",
				"Describe your domain in JSON or Go and the framework generates SQL tables, REST routes, OpenAPI ops, MCP tools, and typed Go models — all from one source."),
			featureCard("MCP-native CRUD",
				"Every entity registers a matching MCP tool surface (list / get / create / update / delete) so AI agents can drive your app through the same routes humans use."),
			featureCard("SSR + SSG out of the box",
				"Screens implement Load(ctx) once and run identically per-request and at build time. `gofastr generate` and `--build-static` ship side-by-side."),
			featureCard("Server-driven UI runtime",
				"Signals, components, islands, SSE streaming, and a 25 KB vanilla-JS runtime that hydrates progressively after first paint."),
			featureCard("Two-layer architecture",
				"core/ — twelve stdlib-only primitives. framework/ — opinionated entity system on top. Drop down to core when the framework is in your way."),
			featureCard("Batteries included, not embedded",
				"auth, cache, email, queue, search, storage — each behind a small interface with an in-memory implementation. Swap in Redis / S3 / Postgres FTS in production."),
		),
	)

	quickstart := render.Tag("section", map[string]string{"aria-label": "Quickstart"},
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Quickstart")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			"git clone https://github.com/DonaldMurillo/gofastr.git\n"+
				"cd gofastr\n"+
				"go test ./...                        # everything green on a fresh clone\n"+
				"go run ./cmd/gofastr -- help         # CLI overview\n"+
				"go run ./examples/blog               # auto-CRUD blog with Swagger UI\n"+
				"go run ./examples/website            # SSR site with the 10 UI primitives"))),
		render.Tag("p", nil, render.Text("Then visit http://localhost:8080.")),
	)

	return render.Tag("div", nil, hero, features, quickstart)
}

func featureCard(title, body string) render.HTML {
	return render.Tag("article", map[string]string{"class": "feature-card"},
		html.Heading(html.HeadingConfig{Level: 3}, render.Text(title)),
		render.Tag("p", nil, render.Text(body)),
	)
}
