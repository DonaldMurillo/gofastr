# ShopFront — build journal

This is the proof-of-thesis flagship: a complete storefront described **once**
in [`gofastr.yml`](gofastr.yml) and scaffolded as runnable, owned Go by a single
command. Because this repo is a monorepo and `examples/ecommerce/` also hosts a
Go test package, the blueprint sets `output_dir: app`, so the scaffold lands in
an owned `app/` subpackage rather than at the module root.

## The declaration

`gofastr.yml` is the entire input — a blueprint declaring:

- **5 related entities** — `categories`, `products`, `orders`, `order_items`,
  `reviews` — with field types (string, text, decimal, int, bool, enum, json,
  image, relation, uuid), validation (`required`, `unique`, `min`/`max`,
  `pattern`), `soft_delete`, cursor pagination, and indices.
- **Relations** — has_many / belongs_to across all five entities.
- **A themed UI** — 8 screens (home, list/detail/create per entity), a nav bar,
  and a colour theme.
- **2 custom endpoints** — `POST /orders/{id}/confirm`, `POST /orders/{id}/ship`.
- **Seed data**, middleware (`request_logger`), and a plugin (`analytics`).
- **Auth + per-user scoping** — `app.auth.enabled: true` mounts the auth
  battery's routes, and `owner_field: user_id` on `orders` / `order_items`
  makes the customer-PII entities fail-closed per-user (see "Securing the
  orders entity" below).

## The one command

```sh
cd examples/ecommerce
gofastr generate --from=gofastr.yml
```

→ `✓ Generated 10 blueprint file(s) in app`:

```
app/main.go                      # app entry point — wires DB, entities, UI, MCP
app/entities/register.go         # app.Entity(...) for all 5 entities
app/entities/models.go           # typed Go structs
app/entities/columns.go          # typed column accessors
app/entities/repo.go             # typed repositories
app/entities/events.go           # per-entity event hooks
app/entities/client/client.go    # typed REST client
app/blueprint/app.go             # theme + sidebar + RegisterGenerated wiring
app/blueprint/screens.go         # the 8 server-rendered screens
app/blueprint/stubs.go           # endpoint / seed / plugin stubs
```

`app/` is owned Go you read, edit, and commit — no `DO NOT EDIT` header, and
re-running `gofastr generate` is add-only (it never clobbers a hand-edited
file; pass `--force` to overwrite). The end-to-end test (`flagship_test.go`)
regenerates a fresh scaffold on every run, so the proof never depends on a
stale checkout.

## The surfaces it produces (all from the one declaration)

Verified by [`flagship_test.go`](flagship_test.go), which builds and runs the
generated binary and asserts every surface is live:

| Surface | Evidence |
|---|---|
| **SQL schema** | tables auto-migrated on boot from the declared fields/indices |
| **REST CRUD** | `POST /products` creates, `GET /products` lists — round-trips |
| **OpenAPI** | `/openapi.json` mounted (auth-gated by secure-by-default → 401) |
| **MCP tools** | `tools/list` advertises all 25 tools (`products_list`, `categories_create`, …) — the AI-author-facing surface |
| **UI** | `GET /` renders the themed `ShopFront` storefront |

Run it yourself:

```sh
go run ./examples/ecommerce/app        # serves on localhost:8080
```

## Scaffolding more

The blueprint is a one-way on-ramp, not a source of truth — the owned `app/` Go
is canonical, and you can edit it directly (or delete `gofastr.yml` once the
code is yours). To scaffold *new* entities or screens you've added to the
blueprint, re-run generate; it's add-only and won't touch files you've edited:

```sh
cd examples/ecommerce && gofastr generate --from=gofastr.yml
```

## Securing the orders entity (a fix, not a feature)

The first cut of this flagship **shipped insecure**: `app.auth.enabled` was
`false`, and `orders` — which carries `customer_name`, `customer_email`,
`customer_phone`, `shipping_address`, and `billing_address` — was exposed via
`crud: true` + `mcp: true` with no `owner_field` and no `access`. Anonymous
HTTP callers and any MCP-connected agent could read every customer's PII and
mutate any order. That is a direct violation of the repo's hard rule: *never
expose an entity holding per-user data via auto-CRUD without
`EntityConfig.OwnerField`*. And the insecurity shipped silently: the
proof-of-thesis test only asserted that the `orders_list` MCP tool was
advertised — it never pinned down that an anonymous caller could actually
read the PII, so nothing in the suite flagged the exposure.

The fix is two declarations in the blueprint, nothing else:

```yaml
app:
  auth:
    enabled: true      # mounts /auth/register, /auth/login, /auth/me, /auth/logout

entities:
  - name: orders
    owner_field: user_id   # + a user_id field; order_items gets the same
```

`owner_field` was chosen over the blueprint's per-operation `access` RBAC map
deliberately. RBAC answers "does your *role* let you touch orders at all" —
any authenticated customer with the permission could still read **every**
customer's orders. `owner_field` is the right teaching pattern for
per-customer PII: the framework stamps `user_id` on create and scopes every
list/get/update/delete (REST *and* the generated MCP tools) to the requesting
user, and **fails closed** — requests that can't produce an owner id get 401
before touching the table. (`access` also currently has no way to grant
permissions in a generated app: the generator doesn't wire a
`framework.AccessMiddleware` role policy yet, so an `access` map would lock
out everyone including legitimate customers.)

`order_items` is scoped with the same `owner_field` — line items are a
customer's purchase history, and leaving the sibling entity open would have
been the same leak through a side door.

`TestOrdersOwnerScoped` in [`flagship_test.go`](flagship_test.go) now pins
this: anonymous REST and MCP calls against orders are rejected (401/403) and
register → login → `/auth/me` proves the session stack works end-to-end.

**Generator gap found by this fix — found, pinned, then fixed.** The first
generated `main.go` enabled the auth battery but did not mount
`auth.SessionMiddleware`, so the session cookie never reached owner-scoped
CRUD — an *authorized* `POST /orders` got 401 just like an anonymous one.
Owner scoping failed closed for everyone (the safe direction), but a
blueprint-only app had no working customer order flow. Rather than hide
that, the test pinned the 401 with an explicit "upgrade me when the
generator is fixed" note. The generator now emits
`fwApp.Use(auth.SessionMiddleware(authMgr))` after the auth manager init,
the pinned assertion tripped exactly as designed, and the test was upgraded
to the full customer flow: register → login → `POST /orders` succeeds with
the session cookie → `GET /orders` lists the customer's own order → a
second customer can neither see it in their list nor fetch it by id. This
is the dogfooding loop working: the flagship found a real framework gap,
the framework was fixed (not the example), and the example's test got
stricter.

## Honest gaps (tracked, not hidden)

The blueprint codegen produces the schema/REST/OpenAPI/MCP/UI surfaces in full,
but three declared things are emitted as **stubs you wire yourself**, not
runnable behaviour:

- **Seed data** — emitted as `blueprint.BlueprintSeedData()` but not auto-run,
  so lists start empty (the E2E creates its own rows). Auto-wiring seeds is a
  follow-up.
- **Custom endpoints** (`confirm_order`, `ship_order`) — emitted as handler
  stubs in `app/blueprint/stubs.go` for you to implement.
- **Public OpenAPI** — the raw `/openapi.json` is auth-gated by
  secure-by-default and the blueprint has no `public_openapi` key yet, so the
  spec returns 401 unauthenticated. The Swagger surface is mounted either way.

These are listed so the proof is honest about where "declaration → behaviour"
currently stops.
