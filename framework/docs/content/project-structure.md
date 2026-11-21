# Project structure

GoFastr does not force a directory layout. `gofastr init` scaffolds a flat project and expects structure to grow only as real boundaries appear—the same way idiomatic Go codebases evolve.

> **Start flat.** Don't create `cmd/`, `internal/`, and `services/` for a prototype. Extract packages when a real boundary shows up.

## What `gofastr init` creates

```text
myapp/
├── main.go                 # entrypoint, wiring, and Start()
├── screens.go              # sample home screen (package main)
├── screens_auth.go         # /login and /register screens (skipped with --no-entity)
├── entities/               # optional Go entity declarations (skipped with --no-entity)
│   └── entities.go
├── migrations/             # versioned SQL migrations
├── static/                 # static assets (serve via uihost.WithStaticDir)
├── gofastr.settings.yml    # CLI and contract settings
├── go.mod
└── AGENTS.md, CLAUDE.md, DESIGN.md, .agents/   # coding-agent onboarding
```

Everything generated is plain Go you commit and edit. There is no `gen/` folder and no `DO NOT EDIT` scaffold lock-in. Run `gofastr dev` from the root for hot reload while iterating (use `go run .` only for quick one-off runs).

If you want a completely barebones start without the sample entity, auth screens, or database wiring, run `gofastr init myapp --no-entity`—or skip `init` altogether and import `core/` packages in a single `main.go`.

## How an app grows

### 1. Flat `package main` (prototypes and small apps)

Most prototypes and small apps stay flat at the module root: `main.go`, `screens.go` (and `screens_auth.go` when auth is enabled), custom routes or handlers, and optional `entities/`.

If you scaffold from a [`gofastr.yml` app spec](app-spec.md) with `gofastr generate app gofastr.yml`, it writes into this same flat layout (`--out=<dir>` puts it in a subpackage if you're inside a monorepo). Generation is one-shot—it refuses to overwrite existing files unless you pass `--force`—and the running app never reads `gofastr.yml`, so you can delete the YAML once the code is yours.

### 2. Domain packages under `internal/<domain>/`

When a feature develops real business logic—billing webhooks, custom workflows, background jobs—move it into a package named after the **domain**, not the technical layer:

```text
myapp/
├── main.go
├── screens.go
├── entities/
├── internal/
│   ├── billing/       # billing handlers, hooks, webhooks, invoice rules
│   ├── projects/      # project endpoints, permissions, queries
│   └── auth/          # custom policies and session helpers
├── migrations/
└── static/
```

Organize by feature (`billing/`, `projects/`), not by layer (`controllers/`, `services/`, `repositories/`). Everything for one domain stays together, and `internal/` keeps it private to your module.

### 3. `cmd/<name>/` when you ship a second binary

One binary stays at the repo root (`main.go`). The moment you add a separate worker, CLI, or migration binary, introduce `cmd/`:

```text
myapp/
├── cmd/
│   ├── server/main.go
│   └── worker/main.go
└── internal/…
```

## Mixing `core/`, `framework/`, and plain stdlib

You never have to choose between barebones and framework-wide conventions. Inside the same app—or even the same `internal/<domain>/` package—you can mix `framework` screens, opt-in `app.Entity` registrations, `core/router` and `core/query` helpers, and plain `net/http` or `database/sql` handlers freely.

## Common mistakes

- **Pre-building `cmd/` + `internal/` on day one.** It adds navigation overhead with nothing inside. Start flat; moving files later is mechanical.
- **Organizing by technical layer (`controllers/`, `services/`, `repositories/`).** In Go this scatters one feature across five folders. Group by domain instead.
- **Treating scaffolded files as read-only.** Files from `gofastr init` and `gofastr generate app` are yours to edit directly; there is no hidden sync loop.
- **Keeping `gofastr.yml` around as a source of truth.** Once generated, the Go files are canonical and the running app never reads the YAML again.
