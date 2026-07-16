# GoFastr Current Risk Register

Date: 2026-07-15 (revalidated against v0.26.0)

This page is a current, revalidated risk register. It is not an archive of
old review findings. Historical findings that are no longer true belong in
git history or `agent-notes.md`, not in this document.

## Current Shape

GoFastr is a Go full-stack framework split across five main surfaces:

- **`core/`** — reusable stdlib-first primitives: routing, handlers,
  middleware, query building, schema validation, streaming, MCP, OpenAPI,
  static files, uploads, config, dotenv, and migrations.
- **`framework/`** — the public application facade, entity registration,
  CRUD, OpenAPI, hooks, events, migrations, plugins, batteries, lifecycle,
  route groups, access control, static export, the plugin host, the agent
  harness, and server startup.
- **`core-ui/` and `framework/ui`** — server-rendered UI, screen/layout
  routing, islands, signals, widget presets, runtime JavaScript, style
  registry, and UI primitives.
- **`battery/`** — optional infrastructure packages such as auth, cache,
  email, embed, log, notify, print, queue, search, storage, and webhooks.
- **`cmd/gofastr`** — scaffolding (`init`, `generate`), the dev loop
  (`dev`, `build` with the enforced accessibility lint), audits
  (`audit a11y`, `audit deps`, `audit lint`), docs, agents guidance, and
  the version-migration helper (`upgrade` + `upgrades.yml`).

The current tree is large enough to require drift guards: about 2,000 Go
files and just under 200 packages at the time of this review. The important
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
  injection, and log redaction. Secure-by-default posture (tenant
  fail-closed, admin default-deny, production refusals for blank JWT
  secrets and in-memory 2FA stores) is pinned by tests.
- Multi-replica behavior is a first-class test dimension: per-consumer
  outbox delivery, shared rate-limit store, durable 2FA store, and
  serve/worker role separation all have witnesses.

## Active Risks

### P1 — Generated And Scaffolded Paths Can Drift

`cmd/gofastr` owns several generation-like paths: `init`, `new`,
`generate`, `generate --from`, `generate --config`, `theme init`,
`build`, `migrate`, `docs`, `agents`, and `upgrade`. The most useful
tests are the ones that build or run generated artifacts through public
command boundaries.

Current guardrails:

- `TestBlueprintCLIGeneratesEntireWorkingAppE2E` starts the generated
  blueprint binary and drives HTTP, MCP, static, and browser UI behavior.
- Generated client tests stand up a real framework server and call the
  generated client against it.
- `gofastr init` tests must compile fresh default, no-entity, and
  Postgres-targeted scaffolds with a local module replace.
- Agent inventory tests walk `battery/` and fail when a non-experimental
  battery is missing from generated `AGENTS.md`; snippet gates compile
  the struct-literal examples in agent guidance.
- `examples/ecommerce`'s flagship test regenerates its `app/` fixture
  with the current generator, so blueprint-output changes surface as
  diffs that must be committed.
- The embedded host skill is pinned to its canonical repo copy
  (`TestEmbeddedHostSkillMatchesRepo`).

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

Current guardrails: every embedded doc topic must appear in the site
catalog (`TestEveryEmbeddedDocIsInCatalog`), guide docs must end with a
"Common mistakes" section (`TestGuideDocsEndWithCommonMistakes`), and
the upgrade registry's `through` marker must match the latest CHANGELOG
release (`TestUpgradeRegistryThroughMatchesChangelog`).

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
- `BenchmarkT9_IslandRPC_Concurrency` uses fixed worker counts; the
  `workers=64` p99 is below the 10 ms target on the current witness.
- Postgres-specific claims must stay marked as Postgres-needed unless a
  Postgres benchmark run produced the evidence; `perf-results.md` records
  the evidence and its capture date.

Next check: do not state a numeric perf claim in `benchmarks.md` or
`perf-results.md` unless the named benchmark exercises the current
implementation.

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
- `examples/meridian` is the design-system completeness canary — a surface
  there that needs CSS the components don't provide is an upstream gap.
- Coverage floors (`scripts/coverage-floors.sh`) and the a11y build gate
  run in CI.

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
to weaker unit tests just to satisfy a restricted sandbox. Browser
(chromedp) suites are additionally load-sensitive: run them isolated, not
in the same `go test` invocation as other heavy suites.

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
