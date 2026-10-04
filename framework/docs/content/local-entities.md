# Local entities

`framework/localentity` declares records that live in the visitor's
browser and renders the forms and lists that edit them. You write Go
and design-system components; there is no server table, no handler,
and no JavaScript in your app. A visitor's records survive reloads and
closed tabs, and every open tab updates when any of them saves.

Use it for per-visitor data that has no business on your server: a
game team, a wishlist, a draft, a checklist. It is built on
[localdb](localdb.md), which you can use directly when you need
something a form and a list do not cover.

The runnable example is `examples/team-builder`, a Pokémon team
builder: `go run ./examples/team-builder` and open
`http://localhost:8093`.

## Declare an entity

Fields are `core/schema` fields, the same type an entity on the server
uses.

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
		localentity.Indexed("level"), // lists can order by level
		localentity.MaxRecords(6),    // a seventh is refused
	)
)

func main() { _ = Members }
```

`Define` declares the backing IndexedDB store for you. Every record
also gets three built-in fields:

| Field | Value |
| --- | --- |
| `id` | A UUIDv7 string minted in the browser. |
| `created_at` | RFC 3339 time of the first save. |
| `updated_at` | RFC 3339 time of the latest save. |

Field names are lowercase snake_case. The field types a browser record
can hold are `String`, `Text`, `Int`, `Float`, `Bool`, `Enum`, `Date`
and `Timestamp`; `Relation`, `JSON`, `Decimal`, `UUID`, `Image` and
`File` are refused. `Required`, `Min`, `Max` (numeric bounds, or string
length), `Pattern`, `Values` and `Default` are honored. Any mistake is
a panic at startup.

## The form

Wrap a normal `ui.Form` with `Entity.Form`. Each control's `name` is a
field name.

```go
Members.Form("team-form", ui.Form(ui.FormConfig{
	Action: "/", Method: "POST", HideSubmit: true, Ctx: ctx,
},
	ui.TextField(ui.TextFieldConfig{Name: "nickname", Label: "Nickname", ID: "member-nickname",
		Required: true, MaxLength: 20}),
	ui.Select(ui.SelectConfig{Name: "species", Label: "Species", ID: "member-species",
		Required: true, Options: speciesOptions()}),
	ui.NumberField(ui.NumberFieldConfig{Name: "level", Label: "Level", ID: "member-level",
		Required: true, Min: ptr(1), Max: ptr(100)}),
	ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Justify: ui.JustifyEnd},
		ui.Button(ui.ButtonConfig{Label: "Clear", Type: "reset", Variant: ui.ButtonGhost}),
		ui.Button(ui.ButtonConfig{Label: "Save", Type: "submit", Variant: ui.ButtonPrimary}),
	),
))
```

On submit the browser's own validation runs first (the `required`,
`maxlength`, `min` and `max` the fields render). Then the behaviour
reads only the declared fields, converts each to its type, checks it
against the declaration (the `localentity-form` behaviour does this; lists are the `localentity` behaviour, so a page with only a list never loads the form code), and either places the problems beside their
fields (the same error placement a server form uses) or saves the
record and resets the form. Controls the declaration does not name,
such as the CSRF input, are ignored.

`ui.Form` still needs an `Action`. Without JavaScript the browser
submits there, so point it at the page itself or at a route that says
the page needs script.

## The list

A list is a configured view over the records. Put it inside a layout
component: the rows become that component's children, so a `ui.Grid`
lays them out as grid cells.

```go
team := Members.List(localentity.ListConfig{OrderBy: "level", Desc: true})

ui.Grid(ui.GridConfig{Min: "14rem"}, team.Render(
	team.Row(func(r localentity.Row) render.HTML {
		return ui.Card(ui.CardConfig{
			HeadingContent: r.Text("nickname"),
			HeadingLevel:   2,
			Footer: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
				r.Edit("team-form", ui.Button(ui.ButtonConfig{Label: "Edit", Type: "button"})),
				r.Delete(ui.Button(ui.ButtonConfig{Label: "Release", Type: "button", Variant: ui.ButtonDanger})),
			),
		}, ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: "Species", Value: r.Text("species")},
			{Label: "Level", Value: r.Text("level")},
		}}))
	}),
	team.Empty(ui.EmptyState(ui.EmptyStateConfig{Title: "No Pokémon yet"})),
))
```

| Piece | Does |
| --- | --- |
| `ListConfig.OrderBy` | `created_at` (default), `updated_at`, or a field declared `Indexed`. IndexedDB orders the rows through the index. |
| `ListConfig.Desc`, `Limit` | Reverse the order; cap the rows shown. |
| `Row(fn)` | The row template, built once on the server. One root element; no `id` attributes, since every record clones it. |
| `Empty(content)` | What shows when there are no records. |
| `r.Text(field)` | A span filled with the record's value as text. Built-ins work too. |
| `r.Delete(control)` | A click deletes this record. |
| `r.Edit(formID, control)` | A click loads this record into the form; the next save updates it. The form's reset button ends the edit. |
| `Entity.Count()` | A span showing how many records there are. |

`ui.CardConfig.HeadingContent` exists for this: it puts a filled-in
value inside the card's own heading element, so the heading keeps the
card's heading style.

## What the visitor sees before the records load

The server cannot see the visitor's records, so the first paint shows
the list empty and the count blank. The behaviour fills both as soon as
it loads, normally before the visitor notices. That is the cost of
keeping the data off the server.

## Safety

Records are untrusted input when they come back out: anyone can edit
IndexedDB from devtools, and any script on your origin can write it.

- Values are written into rows with `textContent` only. A stored
  `<img onerror=…>` shows as text. Objects and arrays show as nothing.
- Only declared fields are read from a form and written to a record.
  A control named `id`, `created_at` or `__proto__` added to the page
  never reaches a record.
- Field names are checked at startup, so none can name a prototype key.

## Messages

The words shown for a refused value or a full list come from Go, so
they can be translated:

```go
localentity.WithMessages(localentity.Messages{
	Full: "Your team is full: release a Pokémon first ({n} at most).",
})
```

`{n}` is replaced by the bound. Unset fields keep `DefaultMessages`.

## Limits

- One browser, one copy. Records do not follow the visitor to another
  device or browser, and clearing site data deletes them.
  `__gofastr.localdb.persist()` asks the browser to keep them under
  storage pressure.
- No server-side validation, hooks, or access rules: there is no
  server write to hang them on.
- Rows show text values. A row that needs a link or an image built
  from a record is a page script reading [localdb](localdb.md)
  directly.

## Common mistakes

- **Putting an `id` on an element inside `Row`.** Every record clones
  the template, so the page ends up with duplicate ids. Use classes
  from the design system and the `r.*` helpers instead.
- **Naming a control differently from its field.** A control whose
  `name` is not a declared field is ignored on save, so the value
  silently never lands. Keep `Name` in the `ui` field config equal to
  the `schema.Field` name.
- **Ordering by a field that is not `Indexed`.** `List` panics at
  render: IndexedDB orders through an index, and the page never sorts.
- **Expecting the server to see the records.** Handlers, hooks, access
  rules and `gofastr verify` coverage do not apply: nothing is written
  on the server. Use a server entity when other users or devices need
  the data.
- **Building links or images from record values in a row.** Rows fill
  text only. Read the records in a page script through
  [localdb](localdb.md) and clean any URL before using it.
