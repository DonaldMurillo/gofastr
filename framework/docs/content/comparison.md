# Comparison — where GoFastr sits

GoFastr's claim, stated narrowly: **one blueprint becomes a
server-rendered UI and a REST API with secure scopes — generated as
plain Go you read, commit, and own.** Several excellent tools cover
parts of that sentence; as far as we can tell, none in the Go ecosystem
covers all of it. This page maps the neighbours honestly — including
the ways they are better — so you can pick the right tool, which is
sometimes not this one.

A note on "MCP tools from your schema": that was a differentiator in
early 2025 and is table stakes now — Supabase ships an MCP server,
PocketBase has community MCP servers, FastAPI has FastAPI-MCP. GoFastr
generates MCP tools too, but that's supporting evidence, not the
wedge. The wedge is the owned-codegen full-stack pipeline.

## vs PocketBase

**What PocketBase is:** a superb runtime BaaS — one prebuilt binary
with embedded SQLite, an admin dashboard, auth, files, and realtime.
You configure collections at runtime through the dashboard; you extend
it by writing hooks (Go or JS) that run *inside* PocketBase's
lifecycle.

**The difference in kind:** PocketBase is a host you configure;
GoFastr is a scaffold-and-own generator plus library. A `gofastr.yml`
blueprint scaffolds owned Go source (a flat `package main` — `main.go`,
`app.go`, `screens.go` — plus `entities/`) that you commit, diff in code
review, debug with a debugger, and refactor like any other package. There is no runtime schema
store — and no canonical declaration you live inside: the blueprint is a
one-way on-ramp you can delete once the code is yours, and the escape
hatch is `net/http`, not a plugin API.

**Choose PocketBase when** you want a backend in five minutes, the
admin dashboard is the product, and you're happy living inside its
extension points. It is more mature, far more battle-tested, and has a
large community. **Choose GoFastr when** the output needs to be a Go
codebase your team (or your agents) own and evolve — and you want the
server-rendered UI generated from the same declaration as the API.

## vs Encore.go

**What Encore is:** a polished Go backend framework where
infrastructure (Pub/Sub, cron, secrets, databases) is declared in code
and provisioned by Encore's tooling, with excellent tracing and a
dashboard. It's commercially backed and significantly more mature.

**The difference in kind:** Encore's full value flows through its
platform — its build system, its cloud/self-hosted runtime, its
dashboard. GoFastr has no platform: `go build` emits a single static
binary that runs anywhere a binary runs, and nothing phones home. The
trade is real in both directions — Encore gives you provisioned infra
and distributed tracing out of the box; GoFastr gives you zero
coupling and a plain `go.mod`.

**Also a scope difference:** Encore is backend-first; GoFastr
generates the server-rendered UI (screens, nav, islands) from the same
blueprint as the API. **Choose Encore when** you want managed-feeling
infra and a mature toolchain. **Choose GoFastr when** you want nothing
between your binary and your VPS.

## vs Wasp

**What Wasp is:** the closest thing to the same thesis — a `main.wasp`
declaration compiles to a full-stack app (auth, CRUD, jobs, email),
and "the framework for the AI era" is literally their tagline. Wasp is
more mature, has a team, a community, and production users.

**The difference in kind:** the generator philosophy, then the runtime
model. Wasp (like Encore and Amplify) is a *canonical-declaration* tool —
`main.wasp` *is* the program; you live in it and regenerate from it, and
the emitted code is the tool's, not yours to hand-edit. GoFastr is
*scaffold-and-own*: the blueprint is a one-way on-ramp that emits idiomatic
Go you then own and edit; `generate` is one-shot and refuses to clobber an
existing project (pass `--force` to regenerate), and you can delete the
blueprint entirely once the code is yours. The trade is real — a canonical declaration promises "no
drift" because there's one source; scaffold-and-own trades that for a
plain Go codebase with no second language to grow into. On top of that
sits the runtime difference: Wasp targets JS/TS — React SPA + Node +
Prisma — while GoFastr is the Go counterpart: SSR-first HTML with island
hydration instead of a client-side React app, one static binary instead of
a Node deployment, `database/sql` instead of an ORM. If you're a
TypeScript shop, Wasp is the obvious pick. If you want this thesis with
Go's deployment story, compile-time guarantees, and owned output, that's
the gap GoFastr exists to fill.

## vs hand-rolled Gin/Echo + sqlc

**What hand-rolling is:** the Go default, and often the right call —
maximal control, zero framework risk, every line auditable.

**What you write yourself:** the glue. Per-entity CRUD handlers,
request validation, filtering/sorting/pagination math, batch
endpoints, cursor paging, OpenAPI annotations kept in sync by hand, an
MCP server if agents need tools, migrations, per-user scoping enforced
in every query, RBAC checks in every handler, and any admin/UI layer.
That glue is exactly what one entity declaration generates here — and
because the output is plain Go on disk, "framework in the way" has the
same exit as hand-rolling: delete the declaration, keep the code,
write `net/http`.

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
  thesis framework and says so.
- **Solo maintainer.** Bus factor of one. Issues get read; SLAs don't
  exist.
- **SQLite and Postgres only.** No MySQL, no MongoDB.
- **No hosted anything.** No cloud, no dashboard-as-a-service, no
  telemetry. That's a feature here, but if you want Encore's or
  Supabase's managed experience, this isn't it.
- **Small community.** The docs are extensive and embedded in the
  binary (`gofastr docs`), but you won't find a thousand Stack
  Overflow answers.

If those trade-offs read as disqualifying for your context, the tools
above are genuinely good — use them. If they read as acceptable for a
v0.x bet on owned, generated Go, start with the
[blueprint tutorial](tutorial-blueprint-app.md).

## Common mistakes

- **Evaluating GoFastr as a BaaS.** There's no runtime collection
  editor; the blueprint is a compile-time scaffold input, not a live
  schema store. And it isn't a source you keep round-tripping either —
  once it scaffolds the owned Go, that Go is canonical; you edit it
  directly and redeploy (you can delete the blueprint).
- **Assuming MCP support is the differentiator.** It isn't anymore;
  most schema-bearing platforms expose MCP. Compare the codegen
  pipeline and ownership story instead.
- **Comparing maturity 1:1.** Every alternative on this page is more
  mature. The honest pitch is the wedge plus the trade-offs above, not
  feature parity.
