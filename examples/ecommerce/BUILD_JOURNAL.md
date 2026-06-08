# ShopFront — build journal

This is the proof-of-thesis flagship: a complete storefront described **once**
in [`gofastr.yml`](gofastr.yml) and emitted as runnable Go by a single command.
Nothing under `gen/` is hand-written.

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

## The one command

```sh
cd examples/ecommerce
gofastr generate --from=gofastr.yml
```

→ `✓ Generated 10 blueprint file(s) in gen`:

```
gen/main.go                      # app entry point — wires DB, entities, UI, MCP
gen/entities/register.go         # app.Entity(...) for all 5 entities
gen/entities/models.go           # typed Go structs
gen/entities/columns.go          # typed column accessors
gen/entities/repo.go             # typed repositories
gen/entities/events.go           # per-entity event hooks
gen/entities/client/client.go    # typed REST client
gen/blueprint/app.go             # theme + sidebar + RegisterGenerated wiring
gen/blueprint/screens.go         # the 8 server-rendered screens
gen/blueprint/stubs.go           # endpoint / seed / plugin stubs
```

`gen/` is gitignored (generated code is regenerated, not committed — `make
clean` wipes it). Run the one command above to materialise it; the output is
normal Go you can read, debug, and step through. The end-to-end test
(`flagship_test.go`) regenerates it on every run, so the proof never depends on
a stale checkout.

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
go run ./examples/ecommerce/gen        # serves on localhost:8080
```

## Regenerating

The blueprint is the source of truth. To re-emit `gen/` after editing
`gofastr.yml`:

```sh
cd examples/ecommerce && gofastr generate --from=gofastr.yml
```

## Honest gaps (tracked, not hidden)

The blueprint codegen produces the schema/REST/OpenAPI/MCP/UI surfaces in full,
but three declared things are emitted as **stubs you wire yourself**, not
runnable behaviour:

- **Seed data** — emitted as `blueprint.BlueprintSeedData()` but not auto-run,
  so lists start empty (the E2E creates its own rows). Auto-wiring seeds is a
  follow-up.
- **Custom endpoints** (`confirm_order`, `ship_order`) — emitted as handler
  stubs in `gen/blueprint/stubs.go` for you to implement.
- **Public OpenAPI** — the raw `/openapi.json` is auth-gated by
  secure-by-default and the blueprint has no `public_openapi` key yet, so the
  spec returns 401 unauthenticated. The Swagger surface is mounted either way.

These are listed so the proof is honest about where "declaration → behaviour"
currently stops.
