# Tutorial: one blueprint → UI + API you own

This is the thesis tutorial. In about twenty minutes you will:

1. write a `gofastr.yml` blueprint and generate a working app — a
   server-rendered UI **and** a REST API (plus OpenAPI and MCP tools)
   from one file, in a single `gofastr generate`;
2. **own the generated Go** — add per-user `owner_field` scoping and an
   RBAC delete-gate by editing the emitted code directly, not by
   re-running the generator;
3. keep going in plain Go — a hand-written screen and role management;
4. point at the deploy recipe for the single-binary Docker image.

The one rule that shapes everything below: **`gofastr generate` is
one-shot.** You run it once to scaffold. From that moment the emitted
Go — not the blueprint — is the source of truth, and you evolve the app
by editing it, exactly the way you would any Go project. The generator
even refuses to overwrite an existing project without `--force`, so it
can't silently clobber your work.

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
  auth:
    enabled: true

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

`auth.enabled` wires the auth battery — register/login/logout routes and
session middleware — because you'll want a signed-in user in the next
step. The `entity_list` gives you a server-rendered table with search,
sort, and pagination out of the box. Once an entity has an enum, bool, or
relation column, add `filters: [<column>, …]` to the block and the
generated screen renders a `ui.FilterToolbar` of facet filters above the
table — see the [`entity_list` reference](blueprints.md) for the details.

Validate, generate, run:

```bash
gofastr validate gofastr.yml
gofastr generate --from=gofastr.yml   # scaffolds owned Go: main.go + app.go + screens.go + entities/
go mod tidy
go run .
```

The scaffold is normal, owned Go — a flat `package main` at the module root:
`entities/register.go` holds the `app.Entity(...)` registrations,
`screens.go` the screen components, `app.go` the `RegisterGenerated` wiring
(including the auth setup), `main.go` the entrypoint. Read them — they are
short, there is no hidden layer underneath, and they carry no `DO NOT EDIT`
header because they're yours to edit and commit. This is the whole point:
`gofastr generate` is one-shot, so from here the owned Go — not the
blueprint — is the source of truth. (Run it again and it refuses to
overwrite; `--force` regenerates the *entire* set and would discard the
edits you're about to make, so you won't use it past this first run.)

Prove all three surfaces from a second terminal — the REST API lives
under the `/api` prefix:

```bash
# The API — auto-CRUD with validation, filtering, pagination:
curl -X POST http://localhost:8080/api/notes \
  -H 'Content-Type: application/json' \
  -d '{"title":"First note","body":"hello"}'
curl http://localhost:8080/api/notes

# The UI — server-rendered screen at /:
curl -s http://localhost:8080/ | grep "My Notes"

# The agent surface — MCP tools generated from the same declaration:
curl -s -X POST http://localhost:8080/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

One file produced all three. Auth is wired — `/auth/register` and
`/auth/login` exist — but the `notes` API is still **anonymous**: the
session middleware is pass-through, and nothing yet scopes a note to its
creator, so anyone can read and write every row. Fix that next — by
owning the Go.

## 2. Own the Go: per-user scoping + an RBAC gate

The rest of the app grows in Go, not in the blueprint. To lock notes to
their owner and gate deletion behind a role, make two edits to the
generated code. (In a real project you might have declared `owner_field`
and `access` in the blueprint up front; doing it by hand here is the same
edit you'd make for *any* change after the one-shot — this is what
"owning the code" looks like.)

**Edit `entities/register.go`** — add the owner column, `OwnerField`, and
`Access` to the `notes` registration:

```go
	app.Entity("notes", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String}, // the owner column
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
		OwnerField: "user_id",                                    // per-user scoping
		Access:     framework.AccessControl{Delete: "notes:admin"}, // RBAC gate on DELETE
		CRUD:       boolPtr(true),
		MCP:        true,
	})
```

`OwnerField` makes CRUD stamp `user_id` from the session on writes and
filter every read to the caller's rows — the client never sets it.
`Access` declares that `DELETE` requires the `notes:admin` permission.

**Edit `app.go`** — declaring `Access` isn't enough; a permission has to
be *resolved* to a caller. First add these three imports to the existing
`import (...)` block (leave the generated ones in place — the exact set
varies with your blueprint):

```go
	"context"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
```

Then install a `RolePolicy` just after the `auth.SessionMiddleware(authMgr)`
line in `RegisterGenerated`:

```go
		fwApp.Use(auth.SessionMiddleware(authMgr))

		// notes declares access: {delete: notes:admin}. Resolve a signed-in
		// user's roles to permissions so the gated CRUD API can authorize.
		// admin holds the wildcard; add finer per-role Grants as you define
		// more roles. Without this, every gated write 403s.
		rbac := access.NewRolePolicy()
		rbac.Grant("admin", access.Wildcard)
		fwApp.Use(access.Middleware(rbac, func(ctx context.Context) []string {
			if u, ok := handler.GetUser(ctx); ok && u != nil {
				if rh, ok := u.(interface{ GetRoles() []string }); ok {
					return rh.GetRoles()
				}
			}
			return nil
		}))
```

Restart. Auto-migrate converges the schema on boot — it adds the new
`user_id` column to the existing `notes` table (additive only; it never
drops or retypes):

```bash
go run .
```

Prefer to review schema changes before they run rather than lean on boot
auto-migrate? Generate a versioned migration from the owned entities and
apply it through the tracked, locked, checksummed runner instead — see
[migrations](migrations.md).

Walk the security model with curl. The note from step 1 predates the
owner column, so it belongs to nobody and no user will see it — per-user
scoping applies to reads too, not just writes:

```bash
# Anonymous access now fails closed:
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/notes   # 401

# Register and log in (the cookie jar holds the session):
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"s3cret-pass"}'
curl -s -c ana.jar -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"s3cret-pass"}'

# Create + list as Ana — user_id is stamped server-side, never trusted
# from the client:
curl -s -b ana.jar -X POST http://localhost:8080/api/notes \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ana private note"}'
curl -s -b ana.jar http://localhost:8080/api/notes

# A second user sees an empty list — rows are scoped per owner:
curl -s -X POST http://localhost:8080/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","password":"s3cret-pass"}'
curl -s -c bob.jar -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"bob@example.com","password":"s3cret-pass"}'
curl -s -b bob.jar http://localhost:8080/api/notes                          # total: 0

# DELETE is RBAC-gated and fails closed: until a policy grants
# notes:admin, even the owner gets 403.
NOTE_ID=$(curl -s -b ana.jar http://localhost:8080/api/notes | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["id"])')
curl -s -o /dev/null -w "%{http_code}\n" -b ana.jar \
  -X DELETE http://localhost:8080/api/notes/$NOTE_ID                        # 403
```

That 403 is correct, not broken: the `RolePolicy` you installed grants
`notes:admin` to nobody yet. Deciding which roles hold which permissions
is application logic — the cue for the next step.

The same scoping applies to the MCP tools and the `_batch` / `_events`
endpoints, and the OpenAPI spec advertises the 401/403 contract — see
[access-control](access-control.md) and
[entity-declarations](entity-declarations.md) → "Per-user scoping".

## 3. Keep owning the Go: roles + a hand-written screen

Everything from here is ordinary Go you add and edit. Grant a role, then
drop in a screen the blueprint never knew about.

You already wired the `RolePolicy` in step 2, so promoting a user to
`admin` gives them the wildcard — and the note deletion gate opens. There
is no generated role-management UI yet (v0.x honesty), so set the role
with one SQL statement:

```bash
sqlite3 notes.db "UPDATE auth_users SET roles='[\"admin\"]' WHERE email='ana@example.com';"
```

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

Register it by editing the scaffolded `main.go` — it's yours. Just after
the generated `RegisterGenerated(fwApp, site, db)` call, add your own
screen:

```go
	RegisterGenerated(fwApp, site, db)

	// A screen the blueprint doesn't know about.
	site.Register("/about", &AboutScreen{}, nil)
```

Restart, then verify the full model — log in again so the session
reflects Ana's new role:

```bash
go run .
```

```bash
curl -s -c ana.jar -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ana@example.com","password":"s3cret-pass"}'
NOTE_ID=$(curl -s -b ana.jar http://localhost:8080/api/notes | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["id"])')

# Bob (role: user) still can't delete:
curl -s -o /dev/null -w "%{http_code}\n" -b bob.jar \
  -X DELETE http://localhost:8080/api/notes/$NOTE_ID                        # 403

# Ana (role: admin) can:
curl -s -o /dev/null -w "%{http_code}\n" -b ana.jar \
  -X DELETE http://localhost:8080/api/notes/$NOTE_ID                        # 204

# And the hand-written screen renders next to the generated one:
curl -s http://localhost:8080/about | grep "Hand-written in Go"
```

The owned Go at the root is now the whole app: `go run .` runs the
generated screens plus your `about.go` and your `register.go` / `app.go` /
`main.go` edits, all one `package main`. The blueprint did its job — it
was a one-shot on-ramp. To add another entity or screen later, edit the
owned Go directly (or scaffold a fresh app in a scratch dir and copy what
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
things to do before shipping an auth-enabled app — both are edits to the
Go you own:

- turn off auth dev mode: in the generated `app.go`, set
  `DevMode: false` and a real `JWTSecret` in the `auth.AuthConfig`
  (source the secret from the environment / a secret manager — the
  generated code already reads `os.Getenv("JWT_SECRET")`). The default is
  dev mode because production cookies require HTTPS — see [auth](auth.md);
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

- **Re-running `gofastr generate` to make a change.** It's one-shot: on
  an existing project it refuses to overwrite (and `--force` regenerates
  everything, discarding your edits). After the first scaffold, change
  the app by editing the owned Go — `entities/register.go`, `app.go`,
  `screens.go`, and your own files beside them.
- **Treating the scaffold as untouchable.** The generated `entities/`
  package and the root `app.go` / `screens.go` / `main.go` are owned
  `package main` Go with no `DO NOT EDIT` header — edit them directly.
- **Declaring `access:` (or `Access`) without resolving roles.** The gate
  fails closed by design: an `AccessControl` requirement means *nobody*
  passes until an `access.RolePolicy` grants the permission and an
  `access.Middleware` resolves the caller's roles (step 2). The
  declaration states the requirement; your code decides who satisfies it.
- **Forgetting to re-login after changing roles.** Roles travel with the
  authenticated user; refresh the session after promoting one.
- **Shipping auth dev mode.** Development convenience only. Set
  `DevMode: false` plus a real `JWTSecret` in the generated `app.go`
  before deploying ([deploy](deploy.md) → Secrets).
