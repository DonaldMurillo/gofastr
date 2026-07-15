# ShopFront — the declaration-driven flagship

This is the largest example in the repo and the proof of the framework's
thesis: a complete storefront described **once** in
[`gofastr.yml`](gofastr.yml) — 5 related entities (categories, products,
orders, order_items, reviews), a themed 8-screen UI, custom endpoints,
seed data, middleware, and a plugin — emitted as runnable Go by a single
command. Nothing under `app/` is hand-written.

## What it proves

One blueprint fans out into every surface: auto-migrated SQL schema,
REST CRUD with validation and cursor pagination, OpenAPI, 25 MCP tools
for agents, and a server-rendered themed storefront. All of it is
asserted live by [`flagship_test.go`](flagship_test.go), which
regenerates `app/` from the blueprint, builds the binary, boots it, and
hits each surface — so the proof never depends on a stale checkout.

## Secure by default

The blueprint ships with auth enabled (`app.auth.enabled: true`) and the
customer-PII entities — `orders` and `order_items` — owner-scoped via
`owner_field: user_id`. The framework stamps the owner on create and
scopes every list/get/update/delete (REST *and* MCP) to the requesting
user, failing closed with 401 when no owner can be derived.
`TestOrdersOwnerScoped` pins this. The full story of how the first cut
shipped insecure and how it was fixed is in
[`BUILD_JOURNAL.md`](BUILD_JOURNAL.md).

## Regenerate and run

```sh
cd examples/ecommerce
gofastr generate --from=gofastr.yml --force   # re-emits app/ from the blueprint (output_dir: app)
gofastr dev --dir app                         # hot-reload dev server on localhost:8080
```

`app/` is committed, owned Go — `output_dir: app` in the blueprint puts the
app in a subpackage so the example also hosts its own test files.

## Read next

- [`BUILD_JOURNAL.md`](BUILD_JOURNAL.md) — the build narrative: what the
  declaration contains, every surface it produces, the security fix, and
  the honest gaps (stubs the generator emits but does not wire).
- [`flagship_test.go`](flagship_test.go) — the end-to-end proof.
- [`gofastr.yml`](gofastr.yml) — the entire input.
