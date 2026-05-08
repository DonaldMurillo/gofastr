# GoFastr — Task Tracker

> Master list of all tasks. Each task has its own file in `plan/tasks/`.
> Status: 🔲 not started | 🔄 in progress | ✅ done | ⏸ blocked
>
> Last reconciled: 2026-05-08. This tracker now reflects the current source tree,
> not just the original proposal checklist. Individual task files may still
> contain unchecked acceptance bullets until their detailed criteria are
> re-reconciled.

---

## Phase 1: Core Primitives

These can be parallelized within each tier. Tier 2 depends on Tier 1.

### Tier 1 — Standalone (no internal deps)

| # | Task | File | Status | Depends On |
|---|------|------|--------|------------|
| 001 | Project scaffolding (go mod, folder structure, CI) | [001-project-scaffolding.md](tasks/001-project-scaffolding.md) | ✅ | — |
| 002 | Schema primitive (field types, validation, JSON Schema gen) | [002-schema.md](tasks/002-schema.md) | ✅ | — |
| 003 | Router primitive (register, match, extract params) | [003-router.md](tasks/003-router.md) | ✅ | — |
| 004 | Handler primitive (typed in/out, error handling) | [004-handler.md](tasks/004-handler.md) | ✅ | — |
| 005 | Middleware primitive (pipeline, compose, context) | [005-middleware.md](tasks/005-middleware.md) | ✅ | — |
| 006 | Query primitive (SQL builder, parameterized, composable) | [006-query.md](tasks/006-query.md) | ✅ | — |
| 007 | Stream primitive (SSE, chunked responses) | [007-stream.md](tasks/007-stream.md) | ✅ | — |

### Tier 2 — Depends on Tier 1

| # | Task | File | Status | Depends On |
|---|------|------|--------|------------|
| 008 | Migrate primitive (versioned migrations, up/down) | [008-migrate.md](tasks/008-migrate.md) | ✅ | 002, 006 |
| 009 | Static files primitive (embed, cache, fingerprint) | [009-static.md](tasks/009-static.md) | ✅ | 003 |
| 010 | Upload primitive (multipart, validate, storage interface) | [010-upload.md](tasks/010-upload.md) | ✅ | 002, 003 |
| 011 | MCP primitive (tool registration, protocol, transport) | [011-mcp.md](tasks/011-mcp.md) | ✅ | 004, 005 |
| 012 | OpenAPI primitive (spec generation from schemas + routes) | [012-openapi.md](tasks/012-openapi.md) | ✅ | 002, 003, 004 |
| 013 | Render primitive (type-safe template engine, Templ-inspired) | [013-render.md](tasks/013-render.md) | ✅ | 002, 003 |

---

## Phase 2: Pluggable Batteries

| # | Task | File | Status | Depends On |
|---|------|------|--------|------------|
| 014 | Auth battery (interface + sessions + OAuth2 + password hashing) | [014-auth-battery.md](tasks/014-auth-battery.md) | 🔄 | 003, 004, 005 |
| 015 | Storage battery (interface + local filesystem) | [015-storage-battery.md](tasks/015-storage-battery.md) | ✅ | 010 |
| 016 | Cache battery (interface + in-memory) | [016-cache-battery.md](tasks/016-cache-battery.md) | ✅ | 004 |
| 017 | Email battery (interface + SMTP) | [017-email-battery.md](tasks/017-email-battery.md) | ✅ | 004 |
| 018 | Queue battery (interface + goroutine pool) | [018-queue-battery.md](tasks/018-queue-battery.md) | 🔄 | 004 |

---

## Phase 3: Framework Layer

| # | Task | File | Status | Depends On |
|---|------|------|--------|------------|
| 019 | Entity system core (declaration, codegen, struct generation) | [019-entity-system.md](tasks/019-entity-system.md) | 🔄 | 002-013 |
| 020 | Auto-CRUD (list/get/create/update/delete routes + handlers) | [020-auto-crud.md](tasks/020-auto-crud.md) | ✅ | 019 |
| 021 | Entity relationships (decl, auto-queries, smart loading) | [021-relationships.md](tasks/021-relationships.md) | 🔄 | 019, 006 |
| 022 | Entity hooks (before/after lifecycle, sync, cancel support) | [022-hooks.md](tasks/022-hooks.md) | ✅ | 019 |
| 023 | Entity validators (field-level custom validation) | [023-validators.md](tasks/023-validators.md) | ✅ | 019 |
| 024 | Entity access control (per-op rules, middleware integration) | [024-access-control.md](tasks/024-access-control.md) | 🔄 | 019, 014 |
| 025 | Entity events (pub/sub, auto-emit lifecycle, custom events) | [025-events.md](tasks/025-events.md) | 🔄 | 019, 018 |
| 026 | Entity pagination (cursor-based default, offset fallback) | [026-pagination.md](tasks/026-pagination.md) | ✅ | 019, 006 |
| 027 | Soft delete (entity toggle, auto-filter queries) | [027-soft-delete.md](tasks/027-soft-delete.md) | ✅ | 019, 006 |
| 028 | Multi-tenancy (TenantID, auto-scope middleware) | [028-multitenancy.md](tasks/028-multitenancy.md) | ✅ | 019, 005 |
| 029 | Entity file/image fields (auto-wire upload + storage) | [029-file-fields.md](tasks/029-file-fields.md) | 🔄 | 019, 010, 015 |
| 030 | Entity rendering (list/detail/form HTML from schema) | [030-entity-rendering.md](tasks/030-entity-rendering.md) | 🔲 | 019, 013 |
| 031 | Entity → MCP auto-tools (CRUD as MCP tools) | [031-entity-mcp.md](tasks/031-entity-mcp.md) | 🔄 | 019, 011 |
| 032 | Entity → OpenAPI (auto-spec from entity definitions) | [032-entity-openapi.md](tasks/032-entity-openapi.md) | ✅ | 019, 012 |
| 033 | Custom endpoints + validators per entity | [033-custom-endpoints.md](tasks/033-custom-endpoints.md) | 🔄 | 019 |
| 034 | DSL query parser (string → type-safe structs, codegen) | [034-dsl-parser.md](tasks/034-dsl-parser.md) | 🔲 | 006, 019 |

---

## Phase 4: CLI & DX

| # | Task | File | Status | Depends On |
|---|------|------|--------|------------|
| 035 | CLI framework (cobra-based, `--json` support) | [035-cli.md](tasks/035-cli.md) | 🔄 | — |
| 036 | `gofastr init` — scaffold new project | [036-cli-init.md](tasks/036-cli-init.md) | 🔄 | 035 |
| 037 | `gofastr generate` — entity → Go codegen | [037-cli-generate.md](tasks/037-cli-generate.md) | 🔄 | 035, 019 |
| 038 | `gofastr build` — codegen + go build | [038-cli-build.md](tasks/038-cli-build.md) | 🔄 | 035, 037 |
| 039 | `gofastr dev` — hot-reload file watcher | [039-cli-dev.md](tasks/039-cli-dev.md) | 🔄 | 038 |
| 040 | `gofastr migrate` — run migrations | [040-cli-migrate.md](tasks/040-cli-migrate.md) | 🔄 | 035, 008 |
| 041 | `gofastr test` — test harness integration | [041-cli-test.md](tasks/041-cli-test.md) | 🔄 | 035, 042 |

---

## Phase 5: Testing & Integration

| # | Task | File | Status | Depends On |
|---|------|------|--------|------------|
| 042 | Testing harness (in-memory app, request helpers, assertions) | [042-testing-harness.md](tasks/042-testing-harness.md) | ✅ | 003, 004 |
| 043 | Plugin system (registry, optional interfaces, lifecycle) | [043-plugin-system.md](tasks/043-plugin-system.md) | ✅ | 003, 004, 005 |
| 044 | Integration tests (full entity lifecycle end-to-end) | [044-integration-tests.md](tasks/044-integration-tests.md) | 🔄 | 019-034 |
| 045 | Example app (blog with posts, comments, tags, auth) | [045-example-app.md](tasks/045-example-app.md) | 🔄 | 044 |

---

## Dependency Graph (simplified)

```
Tier 1 (parallel):  001  002  003  004  005  006  007
                         \    |    |    /         |
Tier 2 (parallel):        008  009  010  011  012  013
                           |         |    |    |
Batteries (parallel):  014  015  016  017  018
                        |              |
Framework (sequential-ish): 019 → 020-034
                            |
CLI: 035 → 036-041
Testing: 042 → 044 → 045
Plugins: 043
```
