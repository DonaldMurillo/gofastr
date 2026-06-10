# Tutorial: one blueprint → UI + API you own

This is the thesis tutorial. In about twenty minutes you will:

1. write a `gofastr.yml` blueprint and generate a working app — a
   server-rendered UI **and** a REST API (plus OpenAPI and MCP tools)
   from one file;
2. secure it — auth battery, per-user `owner_field` scoping, and an
   RBAC `access:` gate, all declared in the blueprint;
3. step past the generator and customize the app in plain Go — the
   generated code is a library you call, not a host you live inside;
4. point at the deploy recipe for the single-binary Docker image.

Every command below is copy-paste runnable. Each step ends with a
`curl` that proves the step worked.

> **Version note.** The blueprint `access:` key, the `gofastr validate`
> subcommand, and the auto-mounted session middleware used below ship in
> the next tagged release. Until then, install the CLI from the
> development branch
> (`go install github.com/DonaldMurillo/gofastr/cmd/gofastr@main`) and
> point the generated app at it too:
> `go get github.com/DonaldMurillo/gofastr@main` (run it after
> `gofastr generate`, before `go mod tidy`).

## 0. Prerequisites

- Go 1.26+ (the floor comes from an optional battery — see
  [deploy](deploy.md) for the nuance)
- the `gofastr` CLI:

```bash
# until the next tagged release — see the version note above
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@main
```

## 1. Blueprint → running app

Create an empty directory with a Go module and one file:

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

entities:
  - name: notes
    crud: true
    mcp: true
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

Validate, generate, run:

```bash
gofastr validate gofastr.yml
gofastr generate --from=gofastr.yml   # writes gen/ — main.go + entities/ + blueprint/
go mod tidy
go run ./gen
```

`gen/` is normal Go: `gen/entities/register.go` holds the
`app.Entity(...)` registrations, `gen/blueprint/screens.go` the screen
components, `gen/main.go` the wiring. Read them — they are short and
there is no hidden layer underneath.

Prove both surfaces from a second terminal:

```bash
# The API — auto-CRUD with validation, filtering, pagination:
curl -X POST http://localhost:8080/notes \
  -H 'Content-Type: application/json' \
  -d '{"title":"First note","body":"hello"}'
curl http://localhost:8080/notes

# The UI — server-rendered screen at /:
curl -s http://localhost:8080/ | grep "My Notes"

# The agent surface — MCP tools generated from the same declaration:
curl -s -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

One file produced all three. Right now, though, the API is anonymous —
anyone can read and write every note. Fix that next.

## 2. Secure it: auth + owner scoping + RBAC

Edit `gofastr.yml` — three changes, all declarative:

1. enable the auth battery (`app.auth`),
2. give notes an owner column and `owner_field` (per-user scoping),
3. gate deletion behind a permission with `access:`.

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
    owner_field: user_id
    access:
      delete: notes:admin
    fields:
      - name: user_id
        type: string
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

Regenerate, evolve the live database (auto-migrate creates missing
*tables* on boot; adding a *column* to an existing table is the
declarative diff's job — review first, then `--apply`), and restart:

```bash
gofastr generate --from=gofastr.yml
go mod tidy
gofastr migrate diff --from=gofastr.yml --db-url='file:notes.db'          # review: ALTER TABLE notes ADD COLUMN user_id TEXT
gofastr migrate diff --from=gofastr.yml --db-url='file:notes.db' --apply
go run ./gen
```

The generated app now mounts `/auth/register`, `/auth/login`,
`/auth/logout`, and session middleware that resolves the cookie to a
user on every request, so owner-scoped CRUD sees who is calling. (The
note created in step 1 predates auth, so its `user_id` is empty — it
belongs to nobody and no user will see it. Per-user scoping applies to
reads too, not just writes.)

Walk the security model with curl:

```bash
# Anonymous access fails closed:
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/notes   # 401

# Register and log in (the cookie jar holds the session):
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"s3cret-pass"}'
curl -s -c ana.jar -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"s3cret-pass"}'

# Create + list as Ana — user_id is stamped server-side, never trusted
# from the client:
curl -s -b ana.jar -X POST http://localhost:8080/notes \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ana private note"}'
curl -s -b ana.jar http://localhost:8080/notes

# A second user sees an empty list — rows are scoped per owner:
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","password":"s3cret-pass"}'
curl -s -c bob.jar -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","password":"s3cret-pass"}'
curl -s -b bob.jar http://localhost:8080/notes                          # total: 0

# DELETE is RBAC-gated and fails closed: until a policy grants
# notes:admin, even the owner gets 403.
NOTE_ID=$(curl -s -b ana.jar http://localhost:8080/notes | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["id"])')
curl -s -o /dev/null -w "%{http_code}\n" -b ana.jar \
  -X DELETE http://localhost:8080/notes/$NOTE_ID                        # 403
```

That 403 is correct, not broken: `access:` emits
`framework.AccessControl` into the generated registration, and the
framework denies any gated operation until **you** decide which roles
hold which permissions. Declaring policy is application logic — which
is the cue for the next step.

The same scoping applies to the MCP tools and the `_batch` /
`_events` endpoints, and the OpenAPI spec advertises the 401/403
contract — see [access-control](access-control.md) and
[entity-declarations](entity-declarations.md) → "Per-user scoping".

## 3. Own the Go: policy + a hand-written screen

`gen/` is deterministic output — regenerating overwrites it, so don't
edit it in place. Instead write your own `main` that calls the same
generated packages and layers on what the generator can't know. This
is the escape hatch working as designed: the generated code is plain
Go you compose, not a runtime you configure.

```bash
mkdir -p cmd/server
```

```go
// cmd/server/main.go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"

	"example.com/notes/gen/blueprint"
	"example.com/notes/gen/entities"
)

// AboutScreen is plain Go — the same interface generated screens implement.
type AboutScreen struct{}

func (s *AboutScreen) ScreenTitle() string          { return "About" }
func (s *AboutScreen) ScreenDescription() string    { return "About this app" }
func (s *AboutScreen) ScreenType() uiapp.ScreenType { return uiapp.ScreenPage }
func (s *AboutScreen) ComponentID() string          { return "screen-about" }
func (s *AboutScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"data-component": s.ComponentID()},
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("About")),
		render.Tag("p", nil, render.Text("Hand-written in Go, served next to generated screens.")),
	)
}

func main() {
	db, err := sql.Open("sqlite3", getEnv("DATABASE_URL", "file:notes.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: blueprint.BlueprintAppName}),
		framework.WithDB(db),
	)
	entities.RegisterAll(fwApp)
	fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)

	site := uiapp.NewApp(blueprint.BlueprintAppName)
	blueprint.RegisterGenerated(fwApp, site, db)

	// RBAC: map roles to the permissions the blueprint's access: keys demand.
	policy := framework.NewRolePolicy()
	policy.Grant("admin", "notes:admin")
	fwApp.Use(framework.AccessMiddleware(policy, func(ctx context.Context) []string {
		if u := auth.GetCurrentUser(ctx); u != nil {
			return u.GetRoles()
		}
		return nil
	}))

	// A screen the blueprint doesn't know about.
	site.Register("/about", &AboutScreen{}, nil)

	fwApp.Mount(uihost.New(site))
	fwApp.OnReady(func(addr string) { fmt.Printf("Server running at http://%s\n", addr) })
	if err := fwApp.Start(getEnv("PORT", "localhost:8080")); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

Promote Ana to admin (there is no generated role-management UI yet —
v0.x honesty — so it's one SQL statement), then run **your** main:

```bash
sqlite3 notes.db "UPDATE auth_users SET roles='[\"admin\"]' WHERE email='ana@example.com';"
go run ./cmd/server
```

Verify the full model — log in again so the session reflects the role:

```bash
curl -s -c ana.jar -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"s3cret-pass"}'
NOTE_ID=$(curl -s -b ana.jar http://localhost:8080/notes | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["id"])')

# Bob (role: user) still can't delete:
curl -s -o /dev/null -w "%{http_code}\n" -b bob.jar \
  -X DELETE http://localhost:8080/notes/$NOTE_ID                        # 403

# Ana (role: admin) can:
curl -s -o /dev/null -w "%{http_code}\n" -b ana.jar \
  -X DELETE http://localhost:8080/notes/$NOTE_ID                        # 204

# And the hand-written screen renders next to the generated one:
curl -s http://localhost:8080/about | grep "Hand-written in Go"
```

From here on, `go run ./gen` is the pure-blueprint app and
`go run ./cmd/server` is yours. Re-run `gofastr generate` whenever the
blueprint changes; your `cmd/server` keeps compiling against the
regenerated packages because it only consumes their exported API.

## 4. Deploy

The app compiles to a single static binary — UI runtime, docs, and
migrations included. The short version:

```bash
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o app ./cmd/server
PORT=8080 ./app
```

For the production multi-stage Dockerfile (distroless, non-root), the
SQLite-vs-Postgres driver decision, secrets, and the
migrations-as-a-release-step pattern, follow [deploy](deploy.md). Two
things to do before shipping an auth-enabled app:

- set `AuthConfig.JWTSecret` from the environment and turn off
  `DevMode` (the generated wiring uses `DevMode: true`; edit your
  `cmd/server/main.go` to construct the auth manager yourself, or keep
  the generated wiring for development only) — see [auth](auth.md);
- decide whether `/openapi.json` stays auth-gated (the default) or is
  exposed via `framework.WithPublicOpenAPI()`.

## Where to go next

- [Blueprints](blueprints.md) — every root key (`screens`, `nav`,
  `seed`, `endpoints`, `middleware`, …) and the validation rules,
  including the unscoped-PII check that makes `gofastr validate` fail
  when per-user data is exposed without scoping.
- [Entity declarations](entity-declarations.md) — the full field-type
  vocabulary and the `owner_field` / `access` semantics.
- [Access control](access-control.md) — policies, roles, and gating
  custom routes.
- [Comparison](comparison.md) — how this pipeline differs from
  PocketBase, Encore, Wasp, and hand-rolling.
- [`examples/ecommerce`](https://github.com/DonaldMurillo/gofastr/tree/main/examples/ecommerce)
  — a five-entity blueprint (auth + owner-scoped orders) generated and
  surface-tested end-to-end.

## Common mistakes

- **Editing `gen/` and losing it.** `gen/` is regenerated wholesale.
  Customizations live in your own packages (`cmd/server`, or anywhere
  that imports `gen/...`).
- **Expecting `access:` to work without a policy.** The gate fails
  closed by design: declaring `access: {delete: notes:admin}` without
  mounting `framework.AccessMiddleware` means *nobody* can delete.
  That's a feature — the declaration states the requirement; the app
  decides who satisfies it.
- **Forgetting to re-login after changing roles.** Roles travel with
  the authenticated user; refresh the session after promoting one.
- **Shipping `DevMode: true`.** Development convenience only. Set a
  real `JWTSecret` in production ([deploy](deploy.md) → Secrets).
