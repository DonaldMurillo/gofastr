# Tutorial: one app spec → UI + API you own

This tutorial walks through the optional `gofastr.yml` scaffolder from scratch:

1. Write a `gofastr.yml` spec and generate a running full-stack app (SSR UI, REST API, OpenAPI, and MCP tools).
2. Edit the generated Go directly to add per-user `OwnerField` scoping and an RBAC delete gate.
3. Add a hand-written Go screen alongside the generated ones.
4. Package it as a single binary for deployment.

**`gofastr generate app` is one-shot.** Once generated, the Go files are the source of truth — edit them like any normal Go project. Re-running without `--force` refuses to overwrite your changes.

## 0. Prerequisites

- Go 1.27.4+ (see [Deploy](deploy.md))
- The `gofastr` CLI:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
```

## 1. App spec → running app

Create a directory with a Go module and a `gofastr.yml`:

```bash
mkdir notes && cd notes
go mod init example.com/notes
```

```yaml
# gofastr.yml
app:
  name: Notes
  module: example.com/notes
  db:
    driver: sqlite
    url: file:notes.db
  auth:
    enabled: true

entities:
  - name: notes
    crud: true
    mcp: true
    public: true   # anyone may read/write; see the note after the smoke test
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: text

screens:
  - name: home
    route: /
    title: Notes
    body:
      - type: heading
        level: 1
        text: My Notes
      - kind: entity_list
        text: Latest notes
        entity: notes
        fields: [title]
        limit: 10
        empty_text: No notes yet.
```

`auth.enabled` wires register/login/logout routes and session middleware. `entity_list` renders a server-side table with search, sort, and pagination; add `display.facets` on the entity later for one-click filter chips (see [App specs](app-spec.md)).

Validate, generate, and start the dev server:

```bash
gofastr validate gofastr.yml
gofastr generate app gofastr.yml   # scaffolds owned Go: main.go + app.go + screens_register.go + screen_*.go + entities/
go mod tidy                           # the scaffold pulls new imports; dev builds need this first
gofastr dev                           # dev server with hot reload; the loop for everything below
```

When `go.mod` does not yet require GoFastr, `gofastr generate` pins the framework version matching your CLI so `go mod tidy` resolves compatible dependencies.

The generated files are plain `package main` Go (`main.go`, `app.go`, `screens_register.go`, `screen_home.go`) plus `entities/notes.go`, `entities/register.go`, and `entities/events.go`. Smoke-test the REST API (under `/api`, the default `api_prefix`) and the SSR UI:

```bash
curl -s -X POST http://localhost:8080/api/notes \
  -H 'Content-Type: application/json' \
  -d '{"title":"First note","body":"Hello GoFastr"}' | jq .

curl -s http://localhost:8080/api/notes | jq .
curl -s http://localhost:8080/ | grep "First note"
```

## 2. Own the generated Go: per-user scoping and RBAC

`public: true` in step 1 let the smoke test run without signing in. For a real app, lock down `notes` so every user only sees their own notes, and only editors/admins can delete them.

Open `entities/notes.go`, add a `user_id` field, remove `Public: true`, and configure `Scope.OwnerField` plus `Exposure.Access`:

```go
app.Entity("notes", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "body", Type: schema.Text},
		{Name: "user_id", Type: schema.String},
	},
	Scope: &framework.ScopeConfig{
		OwnerField: "user_id",
	},
	Exposure: &framework.ExposureConfig{
		MCP: true,
		Access: framework.AccessControl{
			Delete: "notes:delete",
		},
	},
})
```

Save the file — `gofastr dev` rebuilds and restarts automatically. Because `auth.enabled` wired `battery/auth`, GoFastr already has an owner extractor registered:
- Anonymous `GET /api/notes` now returns `401`.
- Creating a note while signed in stamps `user_id` automatically from the session.
- A second signed-in user sees an empty list and gets `404` when requesting the first user's note ID.
- Deleting a note returns `403` unless the caller holds `notes:delete`.

Verify with a cookie jar:

```bash
# Anonymous is rejected
curl -i http://localhost:8080/api/notes   # 401

# Sign up and create a note
curl -c cookies.txt -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct-horse-battery-staple"}'

curl -b cookies.txt -X POST http://localhost:8080/api/notes \
  -H 'Content-Type: application/json' \
  -d '{"title":"Private note"}'
```

## 3. Keep going in plain Go

Adding a hand-written page takes one file and one line in `screens_register.go`. Create `screen_about.go`:

```go
package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func aboutScreen(ctx context.Context) ui.Component {
	return ui.Container(ui.ContainerConfig{},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:       "About Notes",
			Description: "Plain Go screen rendered right beside generated ones.",
		}),
	)
}
```

Register it in `screens_register.go`:

```go
app.Screen("/about", func(c *framework.Context) ui.Component {
	return aboutScreen(c.Request.Context())
})
```

Visit `http://localhost:8080/about` — it shares the app's layout, navigation, theme, and client-side router automatically.

## 4. Next steps

- **Roles & permissions:** wire role grants and per-row rules in [Access control](access-control.md).
- **Additive scaffolding:** add another entity later without overwriting edits using `gofastr generate app more.yml --add` or `gofastr generate entity <name>`.
- **Deploy:** build a single static binary and container image with [Deploy](deploy.md).

## Common mistakes

- **Leaving `public: true` on after initial testing:** anyone on the network can read and overwrite every row. Remove `Public: true` and set `Scope.OwnerField` for user-owned data.
- **Re-running `gofastr generate app --force` after editing `entities/notes.go`:** `--force` overwrites every generated file. Edit the Go directly once scaffolded.
- **Forgetting `go mod tidy` right after `gofastr generate app`:** the scaffold adds framework imports that must be resolved before `gofastr dev` runs.
- **Hitting `/notes` instead of `/api/notes` for JSON:** `app.api_prefix` defaults to `api` so bare paths belong to HTML screens.
