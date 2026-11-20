# Comparison: where GoFastr sits

GoFastr is a full-stack Go framework for prototypes and small-to-medium apps: plain Go you own, from barebones `core/` primitives up to server-rendered UI, auth, SQLite/Postgres, batteries, and MCP in one binary. Several great tools cover parts of that job. This page compares them honestly—including where they beat GoFastr—so you can pick the right one.

## What actually makes GoFastr different

- **Barebones to full-stack in one repo.** You can start with plain `core/router` and `core/render` handlers, or turn on the full stack (`framework/ui`, `battery/auth`, migrations, entities, queues, search), and drop back to `net/http` or `database/sql` anywhere in the same app.
- **Full-stack UI in Go with no JS build pipeline.** Go renders HTML on the server and a small runtime hydrates interactive islands in place—no React, Vue, or separate frontend repo.
- **Built for humans and coding agents together.** Plain Go on disk is easy to read and debug by hand; `gofastr dev` MCP and `gofastr verify` contract checks keep coding agents honest while building; `/mcp` and `llm.md` expose your app to user agents in production.

MCP support by itself is table stakes today (Supabase, FastAPI-MCP, and community PocketBase plugins all offer it). The difference is having the UI, backend, auth, verification, and agent surfaces share one plain-Go codebase with no platform lock-in.

## vs PocketBase

**PocketBase** is a prebuilt runtime BaaS: one binary with embedded SQLite, an admin dashboard, auth, files, and realtime. You configure collections at runtime and extend it with Go or JS hooks inside PocketBase's lifecycle.

**The trade-off:** PocketBase is a host you configure; GoFastr is Go source you own. In GoFastr, your routes, screens, and schema live in ordinary Go files you diff in PRs, refactor with your IDE, and step through in a debugger. If a route doesn't fit the framework, you write `net/http` directly.

- **Choose PocketBase** when you want an instant backend + admin UI and are happy working inside its extension hooks. It is far more mature and battle-tested.
- **Choose GoFastr** when you want a real Go codebase (UI included) that you and your agents edit directly with no runtime schema store.

## vs Encore.go

**Encore** is a mature, commercially backed Go backend framework where databases, Pub/Sub, cron, and secrets are declared in code and provisioned by Encore's toolchain and cloud/self-hosted runtime.

**The trade-off:** Encore manages infrastructure and distributed tracing for you, at the cost of coupling your build and runtime to Encore's platform. GoFastr has no platform—`go build` emits a plain static binary with a normal `go.mod` and zero telemetry—and also covers the server-rendered UI layer.

- **Choose Encore** when you want managed cloud infrastructure, built-in distributed tracing, and a mature backend platform.
- **Choose GoFastr** when you want full-stack Go (including UI) with nothing between your binary and a VPS.

## vs Wasp

**Wasp** is a full-stack JS/TS framework where a `main.wasp` file compiles to a React + Node + Prisma app. It is more mature, with a dedicated team and production community.

**The trade-off:** Wasp uses a *canonical declaration*—`main.wasp` stays the source of truth and owns the generated code. GoFastr's optional `gofastr.yml` app spec is strictly *scaffold-and-own*: it generates idiomatic Go once, refuses to clobber your edits, and can be deleted immediately. On top of that, GoFastr compiles to one static Go binary with SSR + islands instead of a Node + React stack.

- **Choose Wasp** if your team lives in TypeScript and React.
- **Choose GoFastr** if you want Go's single-binary deployment, compile-time checks, and plain source files you own.

## vs hand-rolled Go (stdlib / Gin / Echo + sqlc)

**Hand-rolling** is the Go default—and often the right choice. Maximum control, zero framework surface, every line explicit.

**The trade-off:** On prototypes and small-to-medium apps, hand-rolling means rewriting the same glue every time: auth and sessions, CSRF/CSP headers, form validation, server-rendered UI components, pagination/filtering, migrations, OpenAPI, MCP tools, and background jobs. GoFastr gives you those pieces ready to use, while letting you drop down to `core/` or `net/http` for anything custom.

- **Choose hand-rolling** for tiny microservices, unusual protocols, or teams that want zero framework abstractions.
- **Choose GoFastr** when you want to go from idea to a full-stack app fast without giving up plain Go escape hatches.

## Honest caveats (read before adopting)

- **Pre-1.0:** Breaking changes still happen between minor releases. Pin a version and use `gofastr upgrade` when moving forward ([stability](stability.md)).
- **Early adoption:** GoFastr powers [Barcode & QR Code Maker](https://barcode.donaldmurillo.com/), its own docs site, and `examples/meridian`, but it does not have a large ecosystem of external production teams yet.
- **Solo maintainer:** Bus factor of one; no commercial SLAs.
- **SQLite and Postgres only:** No MySQL, no MongoDB.
- **No hosted cloud:** Deliberate—you deploy the binary yourself.

## Common mistakes

- **Evaluating GoFastr as a BaaS.** There is no runtime collection editor; everything lives in Go files you compile and deploy.
- **Treating `gofastr.yml` as a permanent DSL.** The app spec is an optional one-shot scaffolder. You can build a GoFastr app without ever touching YAML, or scaffold once and delete the file.
- **Assuming you have to use entities for everything.** `core/` routes, custom `framework` handlers, and hand-written screens are first-class; entities are just an opt-in shortcut when CRUD saves time.
