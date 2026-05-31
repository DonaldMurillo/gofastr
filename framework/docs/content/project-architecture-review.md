# GoFastr Current Risk Register

Date: 2026-05-31

This page is a current, revalidated risk register. It is not an archive of
old review findings. Historical findings that are no longer true belong in
git history or `agent-notes.md`, not in this document.

## Current Shape

GoFastr is a Go full-stack framework split across four main surfaces:

- **`core/`** — reusable stdlib-first primitives: routing, handlers,
  middleware, query building, schema validation, streaming, MCP, OpenAPI,
  static files, uploads, config, dotenv, and migrations.
- **`framework/`** — the public application facade, entity registration,
  CRUD, OpenAPI, hooks, events, migrations, plugins, batteries, lifecycle,
  route groups, access control, and server startup.
- **`core-ui/` and `framework/ui`** — server-rendered UI, screen/layout
  routing, islands, signals, widget presets, runtime JavaScript, style
  registry, and UI primitives.
- **`battery/`** — optional infrastructure packages such as auth, cache,
  email, embed, log, notify, print, queue, search, storage, and webhooks.

The current tree is large enough to require drift guards: about 1,500 Go
files and more than 170 packages at the time of this review. The important
architectural control is not smallness; it is keeping each generated,
documented, and benchmarked surface tied to an executable witness.

## What Is Healthy

- The framework root is a facade over subpackages with documented layering
  rules in `framework/ARCHITECTURE.md`.
- Entity CRUD, hooks, owner/tenant scoping, soft delete, batch operations,
  events, OpenAPI, and generated clients have focused regression tests.
- `core-ui/ARCHITECTURE.md` defines the SSR, SPA navigation, island RPC,
  and SSE runtime contract, with drift tests around previously broken rules.
- Generation paths for blueprints and clients run generated code in temp
  modules instead of only checking snippets.
- Security-sensitive batteries have adversarial tests for auth role
  assignment, webhook signing/SSRF, uploads, static file serving, stream
  injection, and log redaction.

## Active Risks

### P1 — Generated And Scaffolded Paths Can Drift

`cmd/gofastr` owns several generation-like paths: `init`, `new`,
`generate`, `generate --from`, `generate --config`, `theme init`,
`build`, `migrate`, `docs`, and `agents`. The most useful tests are the
ones that build or run generated artifacts through public command
boundaries.

Current guardrails:

- `TestBlueprintCLIGeneratesEntireWorkingAppE2E` starts the generated
  blueprint binary and drives HTTP, MCP, static, and browser UI behavior.
- Generated client tests stand up a real framework server and call the
  generated client against it.
- `gofastr init` tests must compile fresh default, no-entity, and
  Postgres-targeted scaffolds with a local module replace.
- Agent inventory tests walk `battery/` and fail when a non-experimental
  battery is missing from generated `AGENTS.md`.

Next check: whenever a new CLI scaffold writes Go code, add an E2E-style
test that compiles that generated code in a temp module.

### P1 — Documentation Drift Can Reintroduce Fixed Bugs

Old architecture-review findings had started describing fixed behavior as
current risk. That is actively harmful: agents and humans will optimize for
bugs that no longer exist and miss the bugs that do.

Current rule:

- This file only contains current, revalidated findings.
- Historical lessons go in `framework/docs/content/agent-notes.md`.
- Per-feature truth belongs in the feature doc under
  `framework/docs/content/`.
- Forward-looking work belongs in `ROADMAP.md`.

Next check: when a review finding is fixed, remove it from this file in the
same change that lands the fix or move only the durable lesson to
`agent-notes.md`.

### P2 — Performance Witnesses Must Measure The Current Path

The benchmark suite is valuable because it names the claim each benchmark
defends. It becomes misleading when the implementation moves but the
benchmark keeps measuring the old path.

Current known status:

- `BenchmarkSSE_BackpressureDropRate` must exercise `core/stream.SSEBroker`
  and its configurable subscriber buffer, not the legacy raw channel path.
  Current semantics are bounded, non-blocking delivery with oldest-drop and
  latest-event retention for slow subscribers.
- `BenchmarkT9_UIHostPageRender` should be interpreted against the current
  SSR page body size; old byte-size baselines are not comparable.
- `BenchmarkT9_IslandRPC_Concurrency` now uses fixed worker counts; the
  `workers=64` p99 is below the 10 ms target on the current witness.
- Postgres-specific claims must stay marked as Postgres-needed unless a
  Postgres benchmark run produced the evidence. `SchemaDiff`,
  idempotent `AutoMigrate`, and streaming-vs-buffered have 2026-05-31
  Postgres evidence recorded in `perf-results.md`.

Next check: do not mark a ROADMAP perf item verified unless the benchmark
named in the item exercises the current implementation.

### P2 — Broad Surface Area Raises Maintenance Cost

GoFastr intentionally ships a broad framework: backend, UI, batteries,
codegen, docs, and agent tooling. The risk is not any single package; it is
cross-surface drift.

Current guardrails:

- `agents.md` snippets live beside battery packages and are registered by
  package init.
- Docs are embedded into the binary and searchable with `gofastr docs`.
- Framework and UI architecture docs name the layering/runtime contracts.
- Generated app E2Es exercise the actual generated binaries where practical.

Next check: when adding a new battery or public surface, ship the package
docs, agent inventory entry, generated docs/CLI witness, and focused tests
in the same change.

## Test Environment Notes

Some tests use `httptest`, local TCP listeners, or browser automation. In
restricted sandboxes these can fail with errors like:

```text
listen tcp 127.0.0.1:0: bind: operation not permitted
```

Treat those as environment failures only after rerunning the affected
package where localhost listeners are allowed. Do not convert those tests
to weaker unit tests just to satisfy a restricted sandbox.

Recommended focused checks:

```bash
GOCACHE=/private/tmp/gofastr-gocache go test -short ./cmd/gofastr
GOCACHE=/private/tmp/gofastr-gocache go test -short ./core/stream ./framework
GOCACHE=/private/tmp/gofastr-gocache go test ./framework -run=^$ \
  -bench='BenchmarkSSE_BackpressureDropRate|BenchmarkT9_UIHostPageRender|BenchmarkT9_IslandRPC_Concurrency' \
  -benchmem -benchtime=100ms -count=1
```

## Maintenance Rule

A finding stays here only while it is actionable and current. If the code
changes, re-check the evidence. If the finding is fixed, remove it. If the
lesson is still useful, summarize it in `agent-notes.md` with a "Next time"
instruction.
