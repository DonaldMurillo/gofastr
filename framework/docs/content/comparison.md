# Comparison — where GoFastr sits

GoFastr is a full-stack Go framework: server-rendered screens, a REST
API, MCP tools, and migrations, written as plain Go against
`framework/` and `framework/ui/`, which sit on stdlib-first primitives
in `core/` and `core-ui/`. An optional `gofastr.yml` blueprint
scaffolds a whole app — UI and API with scoped permissions — as Go you
read, commit, and own. Several tools cover parts of that job; no Go
tool we know of covers all of it. This page maps the neighbours
honestly — including the ways they are better — so you can pick the
right tool, which is sometimes not this one.

A note on "MCP tools from your schema": that was a differentiator in
early 2025 and is table stakes now — Supabase ships an MCP server,
PocketBase has community MCP servers, FastAPI has FastAPI-MCP. GoFastr
generates MCP tools from entities too (on top of `core/mcp`, which any
GoFastr app can use directly), but that's supporting evidence, not the
main point of comparison here. What's different is the codegen
pipeline that generates the whole stack and hands you the code to own.

## vs PocketBase

**What PocketBase is:** a runtime BaaS — one prebuilt binary with
embedded SQLite, an admin dashboard, auth, files, and realtime. You
configure collections at runtime through the dashboard; you extend it
by writing hooks (Go or JS) that run *inside* PocketBase's lifecycle.

**The difference in kind:** PocketBase is a host you configure.
GoFastr is Go source you own — written by hand against the framework,
or scaffolded by the optional blueprint as a flat `package main`
(`main.go`, `app.go`, `screens.go`, `entities/`) — that you commit,
diff in code review, debug with a debugger, and refactor like any
other package. There's no runtime
schema store and no canonical file you have to keep living inside: you
generate once, then edit the Go directly, and you can delete the
blueprint once the code is yours. If you want to drop below the
framework, you write `net/http` — there's no plugin API to route
around.

**Choose PocketBase when** you want a backend in five minutes, the
admin dashboard is the product, and you're happy working inside its
extension points. It's been in production longer, across more
projects, and has a larger community. **Choose GoFastr when** the
output needs to be a Go codebase your team (or your agents) can read
and change directly — and you want the server-rendered UI generated
from the same schema as the API.

## vs Encore.go

**What Encore is:** a Go backend framework where infrastructure
(Pub/Sub, cron, secrets, databases) is declared in code and provisioned
by Encore's tooling, with tracing and a dashboard. It's commercially
backed and more mature.

**The difference in kind:** Encore's features depend on its platform —
its build system, its cloud or self-hosted runtime, its dashboard.
GoFastr has no platform: `go build` emits a single static binary that
runs anywhere a binary runs, and it sends no telemetry. The trade-off
is real in both directions: Encore sets up infrastructure and
distributed tracing for you; GoFastr doesn't couple you to a platform
and keeps a plain `go.mod`.

**Also a scope difference:** Encore is backend-first; GoFastr generates
the server-rendered UI (screens, nav, islands) from the same blueprint
as the API. **Choose Encore when** you want managed infrastructure and
a mature toolchain. **Choose GoFastr when** you want nothing between
your binary and your VPS.

## vs Wasp

**What Wasp is:** the closest thing to the same approach — a
`main.wasp` declaration compiles to a full-stack app (auth, CRUD, jobs,
email), and "the framework for the AI era" is literally their tagline.
Wasp is more mature, has a team, a community, and production users.

**The difference in kind:** the generator model, then the runtime
model. Wasp (like Encore and Amplify) is a *canonical-declaration*
tool — `main.wasp` *is* the program; you live in it and regenerate
from it, and the emitted code belongs to the tool, not to you to
hand-edit. GoFastr scaffolds once and gets out of the way: the
blueprint emits idiomatic Go you then own and edit directly; `generate`
is one-shot and refuses to overwrite an existing project (pass
`--force` to regenerate), and you can delete the blueprint entirely
once the code is yours. The trade-off is real — a canonical
declaration promises "no drift" because there's one source; GoFastr's
approach trades that for a plain Go codebase with no second language to
grow into. There's also a runtime difference: Wasp targets JS/TS —
React SPA + Node + Prisma — while GoFastr is the Go counterpart:
SSR-first HTML with island hydration instead of a client-side React
app, one static binary instead of a Node deployment, `database/sql`
instead of an ORM. If you're a TypeScript shop, Wasp is the obvious
pick. If you want the same idea in Go — Go's deployment story,
compile-time guarantees, and code you own — that's what GoFastr is
for.

## vs hand-rolled Gin/Echo + sqlc

**What hand-rolling is:** the Go default, and often the right call —
maximal control, zero framework risk, every line auditable.

**What you write yourself:** the glue. Per-entity CRUD handlers,
request validation, filtering/sorting/pagination math, batch
endpoints, cursor paging, OpenAPI annotations kept in sync by hand, an
MCP server if agents need tools, migrations, per-user scoping enforced
in every query, RBAC checks in every handler, and any admin/UI layer.
One entity declaration in `framework/entity` generates that glue — and
because the output is plain Go on disk, dropping the framework costs
the same either way: delete the declaration, keep the code, write
`net/http`.

**Choose hand-rolling when** the domain is small, the endpoints are
few, or the requirements are unusual enough that generated CRUD is
noise. **Choose GoFastr when** the app is CRUD-heavy and the glue
would dominate the diff.

## What GoFastr does *not* have (read before adopting)

- **It's v0.x.** Breaking changes happen between releases; pin a
  version. The `v1.0.0` tag will mark the stability promise.
- **No external production adopters yet.** The pipeline is proven by
  the repo's own tooling, docs site, and the
  [`examples/meridian`](https://github.com/DonaldMurillo/gofastr/tree/main/examples/meridian)
  flagship — not by someone else's production traffic. This is a
  young framework and says so.
- **Solo maintainer.** Bus factor of one. Issues get read; SLAs don't
  exist.
- **SQLite and Postgres only.** No MySQL, no MongoDB.
- **No hosted anything.** No cloud, no dashboard-as-a-service, no
  telemetry. That's deliberate, but if you want Encore's or Supabase's
  managed experience, this isn't it.
- **Small community.** The docs are extensive and embedded in the
  binary (`gofastr docs`), but you won't find a thousand Stack
  Overflow answers.

If those trade-offs are disqualifying for your context, the tools
above are good — use them. If they're acceptable for a v0.x bet on Go
you own, start with the
[blueprint tutorial](tutorial-blueprint-app.md).

## Common mistakes

- **Evaluating GoFastr as a BaaS.** There's no runtime collection
  editor; the blueprint is a compile-time scaffold input, not a live
  schema store. And it isn't something you keep regenerating from
  either — once it scaffolds the Go, that Go is what you edit and
  redeploy directly (you can delete the blueprint).
- **Assuming MCP support is the differentiator.** It isn't anymore;
  most schema-bearing platforms expose MCP. Compare the codegen
  pipeline and the fact that you own the output instead.
- **Comparing maturity 1:1.** Every alternative on this page is more
  mature. The honest pitch is the difference above plus the
  trade-offs above, not feature parity.
