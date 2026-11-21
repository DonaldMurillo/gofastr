# Local entities (`framework/localentity`)

`framework/localentity` lets you declare browser-local records (stored in IndexedDB via [`core-ui/localdb`](localdb.md)) and render server-built `framework/ui` forms and live lists for them in Go — zero server database tables, zero API routes, and zero hand-written JavaScript.

Use it for per-visitor state that belongs only in the visitor's browser (checklists, wishlists, drafts, game teams). Changes survive reloads and sync automatically across open tabs.

## Declare a local entity

<!-- gofastr:compile
-->
```go
package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/localentity"
)

func ptr(f float64) *float64 { return &f }

var (
	Box = localdb.New("team-builder")

	Members = localentity.Define(Box, "members", []schema.Field{
		{Name: "nickname", Type: schema.String, Required: true, Max: ptr(20)},
		{Name: "species", Type: schema.Enum, Required: true, Values: []string{"Pikachu", "Eevee"}},
		{Name: "level", Type: schema.Int, Required: true, Min: ptr(1), Max: ptr(100)},
	},
		localentity.Indexed("level"),
		localentity.MaxRecords(6),
	)
)

func main() { _ = Members }
```

Supported field types: `String`, `Text`, `Int`, `Float`, `Bool`, `Enum`, `Date`, and `Timestamp` (plus automatic `id` UUIDv7, `created_at`, and `updated_at`). `Indexed(field)` requires the field to be `Required`. `Pattern` regexes must stay inside the shared Go/JS subset (no `\s`, bare `.`, inline flags, or named groups).

## Forms and lists

- **Form:** wrap a normal `ui.Form` (with its required `Action`) in `Members.Form("team-form", ...)`, matching each input's `Name` to its `schema.Field` name. Validation, locale-aware number parsing, type conversion, and edit/reset behavior run automatically in the `localentity-form` runtime module (and work inside `framework/embed` frames too).
- **List:** configure `team := Members.List(localentity.ListConfig{OrderBy: "level", Desc: true})`, build row and empty templates with `team.Row(...)` and `team.Empty(...)`, and emit the list carrier with `team.Render(row, empty)`:

```go
form := Members.Form("team-form", ui.Form(ui.FormConfig{Action: "#"},
	ui.Input(ui.InputConfig{Name: "nickname", Label: "Nickname", Required: true}),
	ui.Button(ui.ButtonConfig{Label: "Save", Type: "submit"}),
))

team := Members.List(localentity.ListConfig{OrderBy: "level", Desc: true})
list := team.Render(
	team.Row(func(r localentity.Row) render.HTML {
		return html.Div(html.DivConfig{},
			r.Text("nickname"),
			r.Edit("team-form", ui.Button(ui.ButtonConfig{Label: "Edit"})),
			r.Delete(ui.Button(ui.ButtonConfig{Label: "Remove"})),
		)
	}),
	team.Empty(html.Paragraph(html.TextConfig{}, render.Text("No members yet."))),
)
```

Use `Members.Count()` to render a live record count.
- **Safety:** values populate via `textContent` only (never `innerHTML`), and undeclared or prototype keys (`id`, `created_at`, `__proto__`) are ignored on save. Customize messages with `localentity.WithMessages(...)`.

## Common mistakes

- **Putting an `id` attribute inside `team.Row`:** rows clone the template per record, creating duplicate IDs.
- **Mismatched field `Name` on a `ui` form input:** controls not matching a declared `schema.Field` name are ignored on save.
- **Ordering a list by a field not declared `Indexed`:** panics at render; IndexedDB only orders via indexes.
- **Using local entities for shared or critical data:** records live only in one browser and are wiped if site data is cleared; use a server entity when data must sync across devices or users.
