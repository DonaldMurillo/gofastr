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

The `entity_list` gives you a server-rendered table with search, sort, and
pagination out of the box. Once an entity has an enum, bool, or relation column,
add `filters: [<column>, …]` to the block and the generated screen renders a
`ui.FilterToolbar` of facet filters above the table — see the
[`entity_list` reference](blueprints.md) for the details.

Validate, generate, run:

```bash
gofastr validate gofastr.yml
gofastr generate --from=gofastr.yml   # scaffolds owned Go: main.go + app.go + screens.go + entities/
go mod tidy
go run .
```

The scaffold is normal, owned Go — a flat `package main` at the module root:
`entities/register.go` holds the `app.Entity(...)` registrations,
`screens.go` the screen components, `app.go` the `RegisterGenerated` wiring,
`main.go` the entrypoint. Read them — they are short, there is no hidden layer
underneath, and they carry no `DO NOT EDIT` header because they're yours to
edit and commit. `gofastr generate` is one-shot: it scaffolds once and refuses
to overwrite an existing project (pass `--force` to regenerate the whole set),
because from here the owned Go — not the blueprint — is the source of truth.

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

Regenerate and restart — auto-migrate converges the schema on boot,
creating missing tables *and* adding the new `user_id` column to the
existing `notes` table (additive only; it never drops or retypes):

```bash
gofastr generate --from=gofastr.yml
go mod tidy
go run .
```

Prefer to review schema changes before they run rather than lean on
boot auto-migrate? Generate a versioned migration from the owned entities
and apply it through the tracked, locked, checksummed runner:

```bash
gofastr migrate generate add_user_id   # writes migrations/0002_add_user_id.sql — review it
gofastr migrate up --db-url='file:notes.db'
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

The scaffold is a flat `package main` you own — `main.go`, `app.go`,
`screens.go`, `entities/`. There is no separate "generator" package to import;
you customize by editing these files directly and adding your own. This is the
escape hatch as designed: the generated code is plain Go you compose, not a
runtime you configure.

Add a hand-written screen in a new file at the root — same package, same
`Screen` interface the generated ones implement:

```go
// about.go — your own file, package main, alongside the generated ones.
package main

import (
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
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
```

Register it by editing the scaffolded `main.go` — it's yours. Just after the
generated `RegisterGenerated(fwApp, site, db)` call, add your own screen:

```go
	RegisterGenerated(fwApp, site, db)

	// A screen the blueprint doesn't know about.
	site.Register("/about", &AboutScreen{}, nil)
```

RBAC is already wired: because the `notes` entity declares `access:`,
`RegisterGenerated` (in `app.go`) installs a `RolePolicy` that grants the
`admin` role the wildcard. To add finer per-role grants, edit that block in
`app.go` — the generated comment marks the spot ("add finer per-role
`Grant`s … as you define more roles").

Promote Ana to admin (there is no generated role-management UI yet —
v0.x honesty — so it's one SQL statement), then re-run the app:

```bash
sqlite3 notes.db "UPDATE auth_users SET roles='[\"admin\"]' WHERE email='ana@example.com';"
go run .
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

From here on the owned Go at the root is the whole app: `go run .` runs the
generated screens plus your `about.go` and your `main.go` edits, all one
`package main`. The blueprint has done its job — it was a one-shot on-ramp, not
a source you keep regenerating from. To add a new entity or screen later, edit
the owned Go directly (or scaffold a fresh app in a scratch dir and copy what
you want across).

## 4. Deploy

The app compiles to a single static binary — UI runtime, docs, and
migrations included. The short version:

```bash
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o app .
PORT=8080 ./app
```

For the production multi-stage Dockerfile (distroless, non-root), the
SQLite-vs-Postgres driver decision, secrets, and the
migrations-as-a-release-step pattern, follow [deploy](deploy.md). Two
things to do before shipping an auth-enabled app:

- turn off auth dev mode: set `dev_mode: false` and `jwt_secret` under
  `app.auth` in the blueprint and re-scaffold with `--force` (or, since the
  code is yours, flip `DevMode`/`JWTSecret` in the generated `app.go`
  `AuthConfig` directly). The default is `dev_mode: true` because production
  cookies require HTTPS — `gofastr generate` warns until you opt out — see
  [auth](auth.md) and [blueprints](blueprints.md);
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

- **Treating the scaffold as untouchable.** The generated `entities/`
  package and the root `app.go`/`screens.go`/… are owned `package main` Go
  with no `DO NOT EDIT` header — edit them directly and add your own files
  beside them. `gofastr generate` is one-shot: it scaffolds once and refuses
  to overwrite an existing project (use `--force` to regenerate the whole set).
- **Expecting `access:` to work without a policy.** The gate fails
  closed by design: declaring `access: {delete: notes:admin}` without
  mounting `framework.AccessMiddleware` means *nobody* can delete.
  That's a feature — the declaration states the requirement; the app
  decides who satisfies it.
- **Forgetting to re-login after changing roles.** Roles travel with
  the authenticated user; refresh the session after promoting one.
- **Shipping auth dev mode.** Development convenience only. Set
  `dev_mode: false` plus a real `jwt_secret` under `app.auth` and
  regenerate before deploying ([deploy](deploy.md) → Secrets).
