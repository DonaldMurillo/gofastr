# GoFastr — Roadmap

Forward-looking work that isn't built yet (or isn't finished yet). Shipped
features live in `framework/docs/content/<feature>.md` (also embedded into
the binary — run `gofastr docs` to browse) and the two architecture
documents (`framework/ARCHITECTURE.md`, `core-ui/ARCHITECTURE.md`).

Each section ends with a status note. When something ships, delete it from
here and add the `docs/<feature>.md` it now belongs in. Full design
sketches for deleted sections live in git history (this file was trimmed
of ~10 shipped sections on 2026-07-15).

---

## 1. API versioning

**Status:** EXPERIMENTAL (2026-05-22) — lives in
`framework/experimental/apiversions/` (`Version` route-group wrapper,
`Projection`/`ProjectionSet` per-version field shapes, deprecation
headers, version-namespaced MCP tools). Speculative without a real v1↔v2
in-tree case study to shape the projection machinery. Revisit — and
consider promoting out of experimental — when a real consumer surfaces
the shape.

---

## 2. Deferred UI components

Both shipped once as SSR shells with no runtime contract and were
deleted (2026-05-22). Re-add only with the full loop: runtime module +
RPC handler + an e2e test proving the interaction round-trips.

- **Calendar / date picker** — needs `core-ui/runtime/src/datepicker.js`
  + RPC + e2e asserting day selection works end-to-end.
- **Inline edit field** — needs `core-ui/runtime/src/inlineedit.js` +
  RPC + e2e proving click → input swap → Enter saves.

---

## 3. Framework DX — remaining gaps

From the first real third-party app build-out; the rest of the batch
(post-migrate `WithSeed`, `WithAPIPrefix`, entity-first collision
diagnostics) shipped.

### 3a. Entity↔page collision — screen-registered-second diagnostic

`App.Entity`/`App.GroupEntity` detect a route that already owns the
entity URL space and name `WithAPIPrefix` in the error. The reverse
order — registering a screen after the entity — still falls back to the
router's generic conflict diagnostic. Make both orders produce the same
actionable message (colliding entity name, claimed path, recommended
fix). Covered by `framework/collision_test.go` for the shipped
direction.

### 3b. Typed form-field wrappers — `ui.TextField`, `ui.NumberField`, `ui.DateField`

`html.InputConfig` is the low-level primitive; common attrs flow through
`ExtraAttrs` literals at every form call site. Add opinionated wrappers
in `framework/ui/` that compose `FormField + html.Input`, lift
`Required`/`Placeholder`/`Min`/`Max`/`Value`/`Error` into typed config,
and do the ARIA wiring (`aria-describedby`, `aria-invalid`).
`PasswordInput`/`SearchInput`/`NumberInput`/`InputGroup` exist; the
FormField-composing text/number/date trio does not. Acceptance: a form
built with the wrappers has zero `html.Attrs` literals at the call site.

---

## 4. `EntityConfig` sub-config refactor

**Status:** not started (captured 2026-05-24). `EntityConfig` is 17+
flat fields — the semantic relationships between toggles aren't visible
in the type. Group them into `Scope` (MultiTenant/OwnerField/SoftDelete),
`Pagination`, and `Exposure` sub-structs. Deferred because it breaks
every `EntityConfig{...}` literal in user repos: needs its own PR with a
one-release compatibility window (flat fields still compile with
deprecation comments, declaration loader accepts both shapes with a WARN
on flat). Full sketch in git history (pre-2026-07-15 ROADMAP §10).

**No further work** until the deprecation window is scheduled.

---

## 5. BFF posture preset (`framework.WithBFFPosture()`)

**Status:** not started (captured 2026-05-24). The framework has all the
BFF pieces (HttpOnly+Secure session cookies, SessionMiddleware, CSRF,
SkipBearerAuth) but secure wiring is opt-in per piece. A preset flips
them on in one line: cookie-only auth (no JWT in the login body), strict
Origin allowlist on `/api/*`, auto-mounted CSRF + SessionMiddleware.
Must stay an explicit opt-in — each piece would break a class of
existing app (SPA token readers, native clients with `Origin: null`,
un-CSRF'd mutating routes) if defaulted silently. Full option/file
sketch in git history (pre-2026-07-15 ROADMAP §11).

**No further work** until prioritised.

---

## 6. Validation & adoption — proving the thesis

**Status:** in progress — the declaration→surfaces proof shipped;
external adoption is open.

GoFastr makes a falsifiable bet: *an AI agent (or a human) can describe
a real CRUD-heavy app once, in a `gofastr.yml` blueprint, and get a
correct, inspectable, runnable app — SQL + REST + OpenAPI + MCP + UI —
without hand-writing the glue.*

1. **Framework stability → the `v1.0.0` gate.** The API may change until
   v1.0. Drop `v0.x` only when the gate below is green.
2. **Declaration-first proof.** ✓ Shipped: `examples/ecommerce` — a
   five-entity blueprint generated, built, and surface-tested end-to-end
   (`flagship_test.go`, zero hand-written app code).
3. **Dogfooding.** ✓ Kiln and `examples/site` are built on the framework.
   Deepen by porting more internal tooling onto blueprints.
4. **External adoption — the genuinely open item.** No outside
   production users yet. This is the part the code cannot prove for
   itself; named here so the project doesn't pretend otherwise.

**`v1.0.0` gate** (what must be true to drop `v0.x`)

- The public `framework.X` + battery interfaces are frozen, with a
  documented deprecation policy replacing ad-hoc breaking changes.
- The declaration→surfaces proof (#2) stays green in CI, and the
  follow-ups below are closed or consciously scoped out.
- At least one non-author app runs on GoFastr in a real setting (#4).

**Declaration-first follow-ups** (still open; the seed auto-wire and
the `gofastr.yml` discovery question from the original list are
resolved — blueprints emit `app.WithSeed(...)` now, and auto-discovery
of `gofastr.yml` was deliberately rejected in favor of explicit
`--from`, see `cmd/gofastr/generate.go`)

- **`public_openapi` blueprint key.** The raw `/openapi.json` is
  auth-gated by secure-by-default; `AppConfig.PublicOpenAPI` exists but
  the blueprint can't opt into it. Add the key.

---

## 7. v0.20 assessment findings — verified, not yet built

**Status:** backlog (2026-07-13). Three independent model reviews of
v0.20.0 (consolidated; every item below re-verified against HEAD before
landing here). The convergent findings — durable 2FA store + production
boot refusal, shared SQL rate-limit store, coverage floor, doc drift —
shipped the same day; these are the remaining serious items, ranked.

### 7.1 OAuth provider-ID linking must be mandatory (security)

`resolveOAuthUser` deliberately falls back to email-only matching when
the configured `UserStore` doesn't implement `OAuthLinker`
(`battery/auth/oauth2.go:306-356`), and `EntityUserStore` — the store
the auth docs recommend — does not implement it. An IdP that emits an
unverified email claim (the OIDC checks cover issuer/audience/expiry/
subject, not `email_verified` — `battery/auth/oidc.go:503-560`) can
therefore mint a session as an existing local account. Fix shape: a
DB-backed `(provider, provider_id) → user_id` table integrated with
`EntityUserStore`, `OAuth2Plugin.Init` fails without a linker (mirroring
the 2FA store refusal), email-only fallback removed. BREAKING note +
migration path for already-linked users.

### 7.2 Presence topics need an authorization seam (security)

`handleSSE` joins any client-named `?presence=` topic for any minted
island session (`framework/uihost/uihost.go:1449-1456`) — no per-topic
authz hook exists (`core-ui/island/stream.go:35-58` has no reject path).
Under the documented `OnPresenceChange` push pattern the roster —
`DisplayName` is the user's email (`core-ui/island/presence.go:86`) —
reaches every joiner, including anonymous topic-guessers. That defeats
the exact threat cited for refusing an HTTP roster endpoint
(`uihost.go:1477-1483`). Fix shape: `AuthorizeTopic(ctx, topic) bool` on
the manager/uihost; land it before anyone builds presence on non-public
topics.

### 7.3 Runtime RBAC edits don't propagate across replicas (security)

`GrantStore.Grant/Revoke` writes the DB but mutates only the local
replica's live `RolePolicy` (`framework/access/store.go:121-141`) — a
permission revoked through the v0.20 admin screens keeps working on
every other replica until restart. The module registry's fanout
invalidation (`framework/module.go:486-540`) is the in-tree pattern to
copy. Also document the limitation in `access-control.md` until fixed.

### 7.4 Unknown top-level filter params silently return unfiltered rows

`ParseFilters` skips unknown fields (`framework/filter/filter.go:113-125`,
pinned as "lenient" by `framework/crud/cov_lastmile_test.go:33-40`), so
`?titel=x` returns 200 with the full result set, while the identical
typo inside `?where=` correctly 400s (`framework/filter/predicate.go:141-143`).
Silent-wrong is the worst failure class for a data API. Fix shape:
reject unknown keys (BREAKING — changelog note) or an opt-in strict
mode, aligned with the `?where=` posture either way.

### 7.5 Startup seeds race across replicas

`RunSeeds` admits two fresh processes can both see "not seeded" and both
run the callback (`framework/migrate/seed.go:102-117`); `App.Start` runs
seeds in every role (`framework/app.go:1741-1763`). Contradicts the
scaling guide's "two web replicas + one worker, no shared-state
caveats" shape. Fix shape: put ledger-check → callback → ledger-write
(and app-level `WithSeed` hooks) behind the existing Postgres advisory
lock primitive (`core/migrate/lock.go`), or pin seeding to one role.

### 7.6 Stateful islands don't scale past one replica (design)

Even with `WithFanout`, island widget state (objects + signals) is
per-replica — an RPC landing on the wrong replica can't re-render the
widget, so widget-heavy apps need sticky sessions
(`framework/docs/content/scaling.md`, "SSE across replicas"). This is
the ceiling on the flagship interaction model, and cross-replica
presence aggregation (#47) is blocked behind the same decision. Choose
one contract: stateless islands that reload from the DB, a pluggable
shared island-state backend, or documented sticky-only.
