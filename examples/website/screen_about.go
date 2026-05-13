package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// AboutScreen — composed from framework/ui semantic components, so
// every visible element on this page is also a real test case for
// PageHeader / Section / Callout.
type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string        { return "About" }
func (s *AboutScreen) ScreenDescription() string  { return "Project status, scope, and trade-offs." }
func (s *AboutScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *AboutScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "doc-body"},
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow: "About",
			Title:   "GoFastr",
			Subtitle: "An experimental framework that treats AI agents as first-class authors of web applications. Describe your domain once; get schema, REST, OpenAPI, MCP tools, and admin UI without giving up database/sql, net/http, or compile-time safety.",
		}),

		ui.Section(ui.SectionConfig{
			Heading:     "Status",
			Description: "Pre-alpha research. APIs change between commits. Suitable for learning and experimentation, not production work.",
		},
			render.Tag("ul", nil,
				render.Tag("li", nil, render.Text("core/ primitives are usable and tested in isolation")),
				render.Tag("li", nil, render.Text("framework/ entity layer is solid for SQLite + Postgres CRUD apps")),
				render.Tag("li", nil, render.Text("core-ui/ is the active research frontier — APIs change")),
				render.Tag("li", nil, render.Text("CLI binary blank-imports only sqlite3; build your own for other drivers")),
			),
		),

		ui.Callout(ui.CalloutConfig{Title: "No license chosen yet", Variant: ui.StatusWarning},
			render.Text("The code is read-only until a license is added — please don't fork-and-publish until then.")),

		ui.Section(ui.SectionConfig{
			Heading:     "Why",
			Description: "Most Go web frameworks assume a human will hand-write every route, query, validator, migration, and form. AI agents already generate this code — but no framework treats their output as the canonical source. GoFastr inverts that: one declaration, many surfaces.",
		}),

		ui.Section(ui.SectionConfig{
			Heading: "Layered design",
		}, render.Tag("ul", nil,
			render.Tag("li", nil,
				html.Strong(html.TextConfig{}, render.Text("core/ ")),
				render.Text("— stdlib-only primitives. Render, router, markdown, validator.")),
			render.Tag("li", nil,
				html.Strong(html.TextConfig{}, render.Text("core-ui/ ")),
				render.Text("— elements, accordion, tabs, progress, skeleton, breadcrumbs, pagination, widget, theme. Everything maps 1:1 to an HTML primitive or ARIA pattern.")),
			render.Tag("li", nil,
				html.Strong(html.TextConfig{}, render.Text("framework/ ")),
				render.Text("— entity system + ui semantic components. Composes core-ui to express product intent (PageHeader, FormField, DataTable…).")),
		)),
	)
}
