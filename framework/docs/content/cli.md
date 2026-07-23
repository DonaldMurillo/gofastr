# The gofastr CLI

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z
```

One binary. `gofastr <command> --help` prints every flag; this page maps
each command to the doc that covers it.

## Scaffold

- `gofastr init <name>` — a new project: framework UI, `DESIGN.md`, a
  sample entity (`--no-entity` to skip), git, and the agent onboarding
  files. `--module=<path>` sets the Go module; `--db=sqlite|postgres`
  picks the driver. A released CLI pins the generated `go.mod` to its
  matching GoFastr version. A local development build prints the exact
  `go get …@vX.Y.Z` step because it cannot infer a release safely.
- `gofastr new handler <name>` / `gofastr new route <path>` — scaffold
  one handler or route registration into an existing app.
- `gofastr agents init|sync|skill` — generate or refresh `AGENTS.md`
  and the per-battery detail files under `agents/`.
- `gofastr theme init` — scaffold a typed `theme/theme.go` you own.

## Blueprints

- `gofastr validate <yml>` — validate a blueprint without generating
  (exit 0 = valid; includes the unscoped-PII lint).
- `gofastr generate --from=<yml>` — one-shot scaffold of the whole app
  as owned Go ([tutorial](tutorial-blueprint-app.md)). `generate --add`
  / `generate entity <name>` scaffold *new* files into an existing app;
  owned files are never touched.
- `gofastr pack [app-dir]` — snapshot a generated app into a
  best-effort `gofastr.yml`. Lossy; not an inverse of `generate`.

## The daily loop

- `gofastr dev` — rebuild on save, browser livereload, and the dev MCP
  tools for your coding agent ([dev-livereload](dev-livereload.md)).
  `--dir` sets the watch root, `--pkg` the main package under it,
  `--addr`/`-p` the port.
- `gofastr build` — codegen + `go vet` + accessibility lint + `go
  build` (`--no-a11y` skips the lint, `--pkg` selects the main
  package).
- `gofastr test` — run the project's tests.
- `gofastr docs [topic]` — these docs, offline, versioned with the
  binary (`--list` every topic, `--grep <term>` to search).

## Ship

- `gofastr migrate up|down|status|generate|force` — versioned
  migrations: advisory-locked, checksum- and dirty-state-guarded
  ([migrations](migrations.md)).
- `gofastr generate cli` — a customer-facing terminal client for your
  API, with scoped API-token auth ([app-cli](app-cli.md)).
- `gofastr generate sdk` — Go + JS/TS clients your app can host behind
  a live docs page ([sdk](sdk.md)).
- `gofastr upgrade` — move to a newer release: lists every migration
  note between your `go.mod` version and the target and points at
  affected lines; `--apply` runs the steps ([upgrading](upgrading.md)).

## Audit

- `gofastr audit a11y --url <base>` — axe-core scan of a running app in
  both color schemes (`--email`/`--password` log in first).
- `gofastr audit lint` — scan for AI-typical mistakes (ignored `Exec`
  errors, missing CSRF, …).
- `gofastr audit deps` — list dependencies that perform init-time
  global registrations.

## Extras

- `gofastr embed index|watch|query|stats|clear` — the local semantic
  index ([embed](embed.md)).
- `gofastr harness` — the experimental agent harness (`harness mcp`
  runs it as a stdio MCP server; `harness creds` manages encrypted API
  keys).
- `gofastr version` — print version info.

## Common mistakes

- **Updating `go.mod` but not the CLI** (or the other way around). They
  version independently — after `go get …@vX.Y.Z`, also
  `go install …/cmd/gofastr@vX.Y.Z`. `gofastr upgrade --apply` keeps
  them in step ([upgrading](upgrading.md)).
- **`generate --force` on an app you've edited.** It regenerates the
  *entire* set and discards your changes. To add to an existing app use
  `generate --add` / `generate entity <name>` — owned files are never
  touched.
- **`dev --pkg ./cmd/server` from the wrong directory.** Keep `--dir`
  at the project root and point `--pkg` below it; otherwise the watcher
  misses `internal/` and relative paths (a sqlite `db_url`, static
  dirs) resolve against the command directory.
- **`migrate force` as a routine fix.** It only rewrites the tracking
  table. It's for dirty-state recovery or adopting a baseline — read
  [migrations](migrations.md) first.
