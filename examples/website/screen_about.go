package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// AboutScreen — static copy. Could be re-implemented as a markdown screen
// later for symmetry with /docs.
type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About" }
func (s *AboutScreen) ScreenDescription() string  { return "Project status, scope, and trade-offs." }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	return render.Tag("main", map[string]string{"class": "doc-body"},
		elements.Heading(elements.HeadingConfig{Level: 1}, render.Text("About GoFastr")),
		render.Tag("p", nil, render.Text(
			"GoFastr is an experimental framework that treats AI agents as first-class authors "+
				"of web applications. You describe your domain in JSON or Go, and the framework "+
				"generates everything around it — schema, REST endpoints, OpenAPI, MCP tools, and "+
				"admin-grade UI primitives — without giving up database/sql, net/http, or "+
				"compile-time safety.")),
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Status")),
		render.Tag("p", nil, render.Text(
			"Pre-alpha research. APIs change between commits. Suitable for learning and "+
				"experimentation, not production work.")),
		render.Tag("ul", nil,
			render.Tag("li", nil, render.Text("core/ primitives are usable and tested in isolation")),
			render.Tag("li", nil, render.Text("framework/ entity layer is solid for SQLite + Postgres CRUD apps")),
			render.Tag("li", nil, render.Text("core-ui/ is the active research frontier — APIs change")),
			render.Tag("li", nil, render.Text("CLI binary blank-imports only sqlite3; build your own for other drivers")),
			render.Tag("li", nil, render.Text("No license has been chosen yet — the code is read-only until one is added")),
		),
		elements.Heading(elements.HeadingConfig{Level: 2}, render.Text("Why")),
		render.Tag("p", nil, render.Text(
			"Most Go web frameworks assume a human will hand-write every route, query, validator, "+
				"migration, and form. AI agents already generate this code — but no framework treats "+
				"their output as the canonical source. GoFastr inverts that: one declaration, many surfaces.")),
	)
}
