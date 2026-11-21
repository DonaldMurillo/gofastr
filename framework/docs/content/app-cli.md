# Ship your API as a CLI

`gofastr generate cli` turns your app's HTTP API into a standalone, stdlib-only Go CLI (`cmd/<binary>/`) that customers install and authenticate against with scoped API tokens.

```bash
gofastr generate cli
go build ./cmd/myapp
```

It reads entities directly from `entities/*.go` (no YAML required) and imports your generated `entities/client`. Distribute binaries to customers with `go install` or cross-compiled archives + install scripts via `gofastr release` (see [Release](release.md)).

## Customer experience

```bash
myapp login --url https://app.example.com --with-token
myapp posts list --status published --sort -created_at -o table
myapp posts get 42
myapp posts create --title "Hello" --views 3
myapp posts patch 42 --published=false
myapp posts batch-create --json @rows.json
myapp posts watch                          # live SSE feed, JSON lines
```

Built-in conventions:
- **Subcommands per entity:** `list`, `get`, `create`, `update`, `patch`, `delete`, `batch-create`, `batch-update`, `batch-delete`, `watch`, plus custom endpoints from `gofastr.settings.yml` in kebab case.
- **Presence-faithful flags:** `--published=false` sends `false` explicitly; `--json` accepts inline JSON, `@file`, or `-` (stdin); `Image`/`File` fields accept file paths (or `-`) as multipart uploads.
- **Output modes:** `-o json` (default, exact numeric precision) or `-o table` (aligned, stripped of terminal control/bidi characters).
- **Connection precedence:** `--url` → `<BINARY>_URL` / `<BINARY>_TOKEN` env vars → `login` config (`0600` in `os.UserConfigDir()`). Stored tokens are only sent to the exact origin logged into; there is intentionally no `--token` CLI flag (avoiding `ps` leaks).
- **Exit codes:** `0` success, `1` API/transport error, `2` usage, `4` auth failure (`401`/`403`).

## Auth: scoped API tokens

Wire `auth.TokenMiddleware`, `auth.NewTokensPlugin`, and `auth.RequireAPIScopes("/api")` on your server so users can mint scoped `gfsk_...` bearer tokens (`POST /auth/tokens`) and log in via `echo "$TOKEN" | myapp login --url ... --with-token`. Bearer requests bypass CSRF by design (see [Auth](auth.md#service-accounts--scoped-api-tokens)).

## Filtering what gets generated

```bash
gofastr generate cli --only=posts,comments
gofastr generate cli --exclude=audit_logs
gofastr generate cli --verbs='posts=list,get;comments=*'
gofastr generate cli --binary=myapp --api-prefix=api
```

### From an OpenAPI 3 document (no entities)

```bash
gofastr generate cli --from-openapi openapi.json --binary=barc
```

Generates a self-contained CLI (with its own `internal/client`) directly from OpenAPI 3.x `operationId`s, scalar and string-array parameters, JSON/binary bodies, and bearer/header `apiKey` security schemes.

## Extending and regenerating

- `*.gen.go` files are regenerated on every run and carry `//gofastr:generated` markers.
- **`custom.go` is yours:** created once and never overwritten. Override or add commands in `customCommands()`, customize the HTTP client in `configureClient(c *client.Client)`, or call custom routes via `Do(...)`.

### Keeping it current

Add a `cli` generator entry to [`gofastr.settings.yml`](settings.md) so `gofastr generate` and `gofastr build` keep the CLI synchronized automatically:

```yaml
codegen:
  generators:
    - name: cli
      output: cmd/myapp
      config:
        binary: myapp
        api_prefix: api
        only: [posts, comments]
        verbs:
          posts: [list, get]
          comments: ["*"]
```

For an OpenAPI-driven CLI, set `from_openapi: openapi.json` (a local repo file, never a URL during builds).

## Common mistakes

- **Mismatched `--api-prefix`:** baked into the CLI at generation time; if it differs from `AppConfig.APIPrefix`, every request 404s.
- **Forgetting `auth.TokenMiddleware` or `auth.RequireAPIScopes("/api")`:** without `TokenMiddleware`, bearer tokens get `401` (exit 4); without `RequireAPIScopes`, token scopes are not enforced.
- **Editing `*.gen.go` directly:** regenerated on the next run. Put overrides in `custom.go`.
- **Writing `--published false` instead of `--published=false`:** Go's `flag` package treats space-separated `false` as a positional argument and exits 2.
