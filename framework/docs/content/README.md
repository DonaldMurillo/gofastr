# GoFastr documentation

Every guide here ships inside the `gofastr` binary. Browse offline with `gofastr docs`, search with `gofastr docs --grep <term>`, or read them online at [donaldmurillo.github.io/gofastr](https://donaldmurillo.github.io/gofastr/).

If you are new to GoFastr, start with the [Overview](overview.md), then jump to the capability map for what you are building ([Backend](backend-capability-map.md) or [UI](ui-capability-map.md)).

## Start here

- [Overview](overview.md): how the layers fit together, from barebones `core/` to a full-stack app.
- [UI getting started](ui-getting-started.md): scaffold an app, run `gofastr dev`, customize the theme, and build screens.
- [Project structure](project-structure.md): why apps start flat and when to introduce `internal/` or `cmd/`.
- [The gofastr CLI](cli.md): install, scaffold, dev server, migrations, verification, and releases.
- [Backend capability map](backend-capability-map.md): pick a backend job and jump straight to the package and doc.
- [UI capability map](ui-capability-map.md): pick a UI shape and see state ownership, delivery tier, and examples.
- [Comparison](comparison.md): honest trade-offs vs PocketBase, Encore, Wasp, and hand-rolled Go.
- [API stability](stability.md) and [Upgrading](upgrading.md): pre-v1 breaking-change policy and `gofastr upgrade`.

## UI & interactivity

- [Reactivity model](reactivity.md): choosing between client signals, island RPC, polling, and SSE push.
- [UI components](ui-new-components.md): catalog of built-in `framework/ui` components.
- [UI composition recipes](ui-composition-recipes.md): real desktop and mobile layouts built from kit primitives.
- [UI wiring](ui-wiring.md): mounting the UI host onto a plain `framework.App` by hand.
- [Theming](theming.md): light/dark palettes, semantic tokens, and owning your own `theme/theme.go`.
- [Layouts](layouts.md): shared shells, nested routes, and partial navigation swaps.
- [Forms](form-module.md): server-validated forms, field controls, conditional sections, and wizards.
- [Widgets](widgets.md): modals, drawers, popovers, and toasts via `core-ui/widget`.
- [Interactive patterns](interactive-patterns.md): `data-cui-*` behaviors and hand-written server islands.
- [Signal store](signal-store.md): typed client state shared across components with SSR seeding.
- [Optimistic UI](optimistic-ui.md): optimistic mutations, rollback, and conflict reconciliation.
- [Live dashboards](live-dashboards.md): keeping dashboards fresh with polling, signals, and SSE.
- [Pane host](pane-host.md): master/detail workspaces and side-pane navigation.
- [Presence](presence.md): live user rosters over the SSE lane.
- [Runtime contract](runtime-contract.md): full SSR/hydration/island model and `data-cui-*` reference.
- [Headless components](ui-headless.md): unstyled structure and behavior hooks for custom design systems.
- [Browser-local storage](localdb.md) and [Local entities](local-entities.md): IndexedDB stores declared in Go.
- [SEO](seo.md), [PWA](pwa.md), and [Accessibility](accessibility.md): metadata, sitemaps, offline shells, and a11y checks.
- [Static-site export](static-export.md) and [Site search](site-search.md): render routes to static HTML with offline search.
- [Porting an existing frontend](porting.md): serving an external design system or `html/template` pages.

## Data, APIs & entities

- [Entity declarations](entity-declarations.md): declare tables, REST CRUD, validation, OpenAPI, and MCP tools.
- [Migrations](migrations.md): versioned SQL migrations, schema diffing, and dialect support.
- [Hooks & transactions](hooks-and-transactions.md): lifecycle hooks, `App.InTx`, and shared transactions.
- [Custom endpoints](endpoints.md): adding hand-written routes alongside entities.
- [Query DSL](query-dsl.md), [Includes](includes.md), and [Cursor pagination](cursor-pagination.md): filtering, eager loading, and keyset paging.
- [States](states.md): state machines enforced across REST, MCP, and Go writes.
- [Batch endpoints](batch-endpoints.md): atomic multi-record `_batch` writes.
- [Entity events & SSE](events.md): push-only entity change streams.
- [Entity screens](entityui.md): generated list, detail, and create screens for entities.
- [File uploads](uploads.md) and [Image pipeline](image.md): multipart uploads, variants, and pure-Go resizing.
- [The SQLite driver](sqlite-driver.md): pure-Go SQLite (`CGO_ENABLED=0`) defaults and `:memory:` rules.
- [API versioning (experimental)](api-versioning.md): versioned prefixes, projections, and deprecation headers.

## Auth, security & operations

- [Authentication](auth.md): sessions, login, OAuth, magic links, 2FA, passkeys, and password resets.
- [Access control](access-control.md): roles, wildcard scopes, permissions, and custom policies.
- [Security defaults](security.md): CSP, CSRF, CORS, headers, and safe defaults.
- [Multi-tenant scoping](multi-tenant.md): automatic `tenant_id` isolation.
- [Rate limiting](rate-limit.md) and [Idempotency](idempotency.md): abuse protection and safe mutation retries.
- [Audit log](audit-log.md), [Server logs](log.md), and [Observability](observability.md): audit trails, JSON logs, and metrics.
- [Health checks](health-checks.md) and [First-run setup](first-run.md): probes and initial-boot onboarding.
- [Deployment](deploy.md), [Scaling](scaling.md), and [Backups](backups.md): single-binary deploys, multi-replica fanout, and backups.
- [Data export & erasure](data-export.md): user data portability and deletion helpers.
- [Project settings](settings.md), [Env / .env](dotenv.md), and [Worktree isolation](isolation.md): local config and parallel agent worktrees.

## Batteries & infrastructure

- [Queue](queue.md) and [Cron](cron.md): background jobs with retries/dead-letter and scheduled tasks.
- [Email](email.md) and [Notifications](notifications.md): SMTP/log senders and multi-channel templates.
- [File storage](storage.md) and [Cache](cache.md): pluggable local/S3 storage and caching interfaces.
- [Search](search.md) and [Semantic search](semantic-search.md): FTS5/Postgres full-text and local vector search.
- [Webhooks](webhooks.md): signed outbound delivery with retries and dead-lettering.
- [Admin UI](admin.md): built-in back-office screens for data, queues, and audit logs.
- [Printable documents](print.md): print-friendly HTML and optional PDF generation.
- [Feature flags](feature-flags.md) and [Internationalization](i18n.md): rollouts, translations, CLDR formatting, and RTL.
- [First-party relay](relay.md) and [Analytics recipes](analytics-recipes.md): same-origin proxying for analytics under strict CSP.
- [Embed](embed.md) and [WebRTC rooms](rtc.md): embeddable third-party frames and peer-to-peer signaling.
- [Plugins](plugins.md), [Modules](modules.md), [Process modules](process-modules.md), and [Plugin platform](plugin-platform.md): extending an app in-process or isolated.

## Agents & the agentic web

- [Agent-ready apps](agent-ready.md): `/mcp`, `llm.md`, `llms.txt`, and agent discovery endpoints.
- [MCP server](mcp.md): registering custom tools, resources, prompts, and gates with `core/mcp`.
- [Agent host](agent-host.md): chat-host surfaces and MCP App widgets.
- [A2A task exchange](a2a.md): Agent2Agent v1.0 skills and task lifecycle.
- [Web Bot Auth](web-bot-auth.md): cryptographic bot identity verification.
- [WebMCP bridge (experimental)](webmcp.md): exposing server tools to in-browser agents.
- [Generative UI](generative-ui.md): composing the design system instead of emitting raw markup.
- [Kiln](kiln.md) and [ACP](acp.md): experimental agent-driven build mode and Agent Client Protocol server.

## Tooling, hosts & testing

- [Contracts](contracts.md) and [Strict mode](strict-mode.md): `gofastr verify` static checks and semantic coverage.
- [Testkit](testkit.md) and [Factories](factories.md): real HTTP/DB integration tests and fixture helpers.
- [AppSpec tutorial](tutorial-app-spec.md), [App specs](app-spec.md), and [Codegen](codegen.md): optional one-file app scaffolding.
- [Client SDKs](sdk.md), [Customer CLI](app-cli.md), and [Releases](release.md): shipping Go/TS SDKs, branded CLIs, and release installers.
- [Application hosts](application-hosts.md), [Local apps](local-apps.md), and [Local runs](local-runs.md): running headless or managing local state.
- [Desktop host (experimental)](desktop.md), [OS service host (experimental)](service.md), and [On-disk telemetry (experimental)](telemetry.md).
- [Core packages](core-packages.md): standalone `core/` utilities without dedicated guides.
- [Compute workers](compute.md), [Dev livereload](dev-livereload.md), [Runtime minification](runtime-minification.md), [Unmounting packages](unmount.md), and [Auditing deps](audit-deps.md).
- [Benchmarks](benchmarks.md), [Performance results](perf-results.md), and the [Roadmap](../../../ROADMAP.md).
