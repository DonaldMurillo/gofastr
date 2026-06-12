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
├── entities/           # Go entity declarations — the source of truth
│   └── entities.go     # app.Entity(...) registrations (sample: posts)
├── blueprint/          # generated screens + app-wiring you own and edit
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
owned Go straight into this root layout — `main.go` at the root plus `entities/`
and `blueprint/` packages — that you read, edit, and commit. The blueprint is an
on-ramp, not a source of truth: once the code is yours you can keep editing it
directly (re-running `generate` is add-only and never clobbers your edits), and
you can delete `gofastr.yml` entirely — the running app does not need it. Most
small apps never need more than this.

**`--out=<dir>` (or `output_dir:` in the blueprint's `app:` block) scaffolds into
a subpackage** instead of the module root — useful when the repo is a monorepo or
an example that also hosts its own Go test package and you want the app to live
under, say, `app/`. The flagship [`examples/ecommerce`](https://github.com/DonaldMurillo/gofastr/tree/main/examples/ecommerce)
uses `output_dir: app` for exactly this reason; you build and run it with
`go run ./app`. The subpackage is still owned Go — `--out` only changes *where*
the scaffold lands, not whether you own it.

**2. Domain packages (`internal/<domain>/`), when real per-feature logic
appears.** When a feature grows hooks, custom endpoints, background work, or
service logic, give it a package named for the **domain**, not the technical
layer:

```
myapp/
├── main.go
├── entities/            # declarations stay here
├── blueprint/           # generated screens + wiring stay here
├── internal/
│   ├── billing/         # billing hooks, webhooks, invoice logic
│   ├── projects/        # project-specific endpoints + rules
│   └── auth/            # custom auth policy, owner extraction
├── migrations/
└── static/
```

This is the one structural opinion worth holding: **organize by domain
(`billing/`, `projects/`), not by layer (`controllers/`, `services/`,
`repositories/`).** Everything about a feature lives together, which is easier
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
  no `DO NOT EDIT` header — the generated `entities/` and `blueprint/` are owned
  Go. Edit them directly. Re-running `gofastr generate` is add-only: it writes
  new files but never overwrites one you've hand-edited (pass `--force` to
  overwrite).
- **Layer-based packages (`services/`, `repositories/`).** In Go this scatters
  one feature across many directories. Organize by domain instead.
- **Keeping `gofastr.yml` around as a source of truth.** It's a one-way on-ramp.
  After scaffolding, the owned Go is canonical; you can delete the blueprint and
  the app still runs.
