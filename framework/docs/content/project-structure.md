# Project structure

GoFastr does not impose a layout. It scaffolds a **flat** project and expects
you to grow into structure as real boundaries appear — the way idiomatic Go
projects actually evolve, not the way a framework dictates on day one.

> The rule: **structure follows the app.** Don't pre-build `cmd/` + `internal/`
> for a hello-world. Extract a package when a real boundary shows up, not before.

## What `gofastr init` gives you

```
myapp/
├── main.go             # the entrypoint — one binary, so it lives at the root
├── app.go              # RegisterGenerated: screens + app wiring (package main)
├── screens.go          # generated screen components (package main)
├── entities/           # Go entity declarations — the source of truth
│   └── entities.go     # app.Entity(...) registrations (sample: posts)
├── migrations/         # versioned SQL (reversible)
├── static/             # assets served as-is
├── gofastr.yml         # the scaffold input — OPTIONAL, deletable once the code is yours
├── go.mod
└── AGENTS.md, CLAUDE.md, agents/   # agent onboarding
```

That's the whole thing. One `main.go`, your declarations, and the generated
code beside them — all owned Go you commit and edit. No `gen/` quarantine dir,
no `cmd/`, no `internal/`, no `controllers/services/repositories/` — there's
nothing to navigate yet.

## How it grows

GoFastr leans on the two layouts Go developers already reach for, in order:

**1. Declarations + scaffolded code.** Entities are the canonical
description — written in Go (`entities/entities.go`, the `gofastr init`
default) or declared in a [`gofastr.yml` blueprint](blueprints.md). When you
generate from a blueprint (`gofastr generate --from=gofastr.yml`), it scaffolds
owned Go straight into this root layout — a flat `package main` (`main.go`,
`app.go`, `screens.go`, …) plus the `entities/` package — that you read, edit,
and commit. The blueprint is optional and one-time, not a source of truth:
`generate` is one-shot (it refuses to overwrite an existing project unless you
pass `--force`), so once the code is yours you own and edit it directly, and
you can delete `gofastr.yml` entirely — the running app does not need it. Most
small apps never need more than this.

**`--out=<dir>` (or `output_dir:` in the blueprint's `app:` block) scaffolds into
a subpackage** instead of the module root — useful when the repo is a monorepo or
an example that also hosts its own Go test package and you want the app to live
under, say, `app/`. The [`examples/ecommerce`](https://github.com/DonaldMurillo/gofastr/tree/main/examples/ecommerce)
example app uses `output_dir: app` for exactly this reason; develop it with
`gofastr dev --dir app` (hot reload) or run it once with `go run ./app`. The
subpackage is still owned Go — `--out` only changes *where* the scaffold
lands, not whether you own it.

**2. Domain packages (`internal/<domain>/`), when real per-feature logic
appears.** When a feature grows hooks, custom endpoints, background work, or
service logic, give it a package named for the **domain**, not the technical
layer:

```
myapp/
├── main.go
├── entities/            # declarations stay here
├── app.go, screens.go   # generated screens + wiring stay here (package main)
├── internal/
│   ├── billing/         # billing hooks, webhooks, invoice logic
│   ├── projects/        # project-specific endpoints + rules
│   └── auth/            # custom auth policy, owner extraction
├── migrations/
└── static/
```

The opinion GoFastr holds here: **organize by domain (`billing/`,
`projects/`), not by layer (`controllers/`, `services/`, `repositories/`).**
Everything about a feature lives together, which is easier
to navigate and matches how the standard library and most large Go codebases
are organized. `internal/` keeps these packages private to your module.

**3. `cmd/<name>/` only when you have a second binary.** A single server stays
as a root `main.go` — that's idiomatic for one program. The moment you add a
worker, a CLI, or a migration runner, give each its own `cmd/` directory:

```
myapp/
├── cmd/
│   ├── server/main.go
│   └── worker/main.go
├── internal/…
└── …
```

## Reaching for `core/`

When the framework is in your way, drop to `core/` and write plain `net/http` —
`core/router`, `core/render`, `core/query`, and friends are usable on their own.
A domain package under `internal/` can mix framework entities and hand-written
`core` handlers freely; nothing forces you to stay inside the entity layer.

## Common mistakes

- **Pre-building `cmd/server/` + `internal/` for a tiny app.** It's ceremony
  with nothing in it. Start flat; the refactor to `cmd/`/`internal/` is
  mechanical when you actually need it.
- **Treating the scaffold as untouchable.** There's no `gen/` quarantine dir and
  no `DO NOT EDIT` header — the generated `entities/` package and root
  `app.go`/`screens.go`/… are owned `package main` Go. Edit them directly.
  `gofastr generate` is one-shot: it scaffolds once and refuses to overwrite an
  existing project (pass `--force` to regenerate the whole set).
- **Layer-based packages (`services/`, `repositories/`).** In Go this scatters
  one feature across many directories. Organize by domain instead.
- **Keeping `gofastr.yml` around as a source of truth.** It's an optional,
  one-time input to the scaffold — the app never reads it again. After
  scaffolding, the owned Go is canonical; you can delete the blueprint and
  the app still runs.
